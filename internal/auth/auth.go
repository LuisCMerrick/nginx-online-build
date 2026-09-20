package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"nginx-builder/internal/config"
	"strings"
	"sync"
	"time"
)

const (
	CookieName      = "nginx_builder_session"
	QueryParamKey   = "key"
	QueryParamToken = "token"
	QueryParamAuth  = "auth"
	HeaderAuthKey   = "X-Auth-Key"
	sessionLifetime = 12 * time.Hour
	maxSessions     = 1024
)

type sessionStore struct {
	sync.Mutex
	sessions map[string]time.Time
}

func (s *sessionStore) create() (string, error) {
	key, err := randomKey()
	if err != nil {
		return "", err
	}
	s.Lock()
	defer s.Unlock()
	now := time.Now()
	for token, expiry := range s.sessions {
		if !expiry.After(now) {
			delete(s.sessions, token)
		}
	}
	if len(s.sessions) >= maxSessions {
		var oldest string
		var expiry time.Time
		for token, t := range s.sessions {
			if oldest == "" || t.Before(expiry) {
				oldest = token
				expiry = t
			}
		}
		delete(s.sessions, oldest)
	}
	s.sessions[key] = now.Add(sessionLifetime)
	return key, nil
}
func (s *sessionStore) valid(key string) bool {
	s.Lock()
	defer s.Unlock()
	expiry, ok := s.sessions[key]
	if !ok {
		return false
	}
	if !expiry.After(time.Now()) {
		delete(s.sessions, key)
		return false
	}
	return true
}
func (s *sessionStore) remove(key string) { s.Lock(); delete(s.sessions, key); s.Unlock() }
func randomKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func Middleware(cfg *config.Config, next http.Handler) http.Handler {
	sessions := &sessionStore{sessions: make(map[string]time.Time)}
	path := cfg.BasePath
	if path == "" {
		path = "/"
	}
	setCookie := func(w http.ResponseWriter, r *http.Request, key string, maxAge int) {
		http.SetCookie(w, &http.Cookie{Name: CookieName, Value: key, Path: path, HttpOnly: true, Secure: cfg.CookieSecure || r.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.ContentLength > config.MaxRequestBytes {
			authJSON(w, http.StatusRequestEntityTooLarge, false, "request body too large")
			return
		}
		route := r.URL.Path
		if cfg.BasePath != "" {
			route = strings.TrimPrefix(route, cfg.BasePath)
		}
		if route == "/api/auth/login" || route == "/api/auth/logout" {
			w.Header().Set("Cache-Control", "no-store")
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", "POST")
				authJSON(w, http.StatusMethodNotAllowed, false, "POST required")
				return
			}
			if !sameOrigin(r, cfg.CookieSecure) {
				authJSON(w, http.StatusForbidden, false, "cross-origin request rejected")
				return
			}
			if route == "/api/auth/logout" {
				if cookie, err := r.Cookie(CookieName); err == nil {
					sessions.remove(cookie.Value)
				}
				setCookie(w, r, "", -1)
				authJSON(w, http.StatusOK, true, "")
				return
			}
			if !cfg.AuthEnabled {
				authJSON(w, http.StatusOK, true, "")
				return
			}
			var payload struct {
				Key string `json:"key"`
			}
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&payload); err != nil {
				authJSON(w, http.StatusBadRequest, false, "invalid login payload")
				return
			}
			var extra any
			if err := decoder.Decode(&extra); err != io.EOF {
				authJSON(w, http.StatusBadRequest, false, "invalid login payload")
				return
			}
			if !ValidateKey(strings.TrimSpace(payload.Key), cfg.AuthKey) {
				authJSON(w, http.StatusUnauthorized, false, "invalid access key")
				return
			}
			session, err := sessions.create()
			if err != nil {
				authJSON(w, http.StatusServiceUnavailable, false, "cannot create session")
				return
			}
			setCookie(w, r, session, int(sessionLifetime.Seconds()))
			// Expire the old raw-key cookie on migration. It is never accepted for authentication.
			http.SetCookie(w, &http.Cookie{Name: "nginx_builder_key", Path: path, MaxAge: -1, HttpOnly: true, Secure: cfg.CookieSecure || r.TLS != nil})
			authJSON(w, http.StatusOK, true, "")
			return
		}
		if !cfg.AuthEnabled {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if ValidateKey(headerKey(r), cfg.AuthKey) {
			next.ServeHTTP(w, r)
			return
		}
		query := r.URL.Query()
		queryKey := query.Get(QueryParamKey)
		if queryKey == "" {
			queryKey = query.Get(QueryParamToken)
		}
		if queryKey == "" {
			queryKey = query.Get(QueryParamAuth)
		}
		if queryKey != "" && ValidateKey(strings.TrimSpace(queryKey), cfg.AuthKey) {
			// URL keys are an entry-point login mechanism; APIs use headers or a session.
			if r.Method == http.MethodGet && !isAPIRequest(r.URL.Path, cfg.BasePath) {
				session, err := sessions.create()
				if err != nil {
					authJSON(w, http.StatusServiceUnavailable, false, "cannot create session")
					return
				}
				setCookie(w, r, session, int(sessionLifetime.Seconds()))
				query.Del(QueryParamKey)
				query.Del(QueryParamToken)
				query.Del(QueryParamAuth)
				target := "/" + strings.TrimLeft(r.URL.EscapedPath(), "/")
				if query.Encode() != "" {
					target += "?" + query.Encode()
				}
				http.Redirect(w, r, target, http.StatusSeeOther)
				return
			}
		}
		if cookie, err := r.Cookie(CookieName); err == nil && sessions.valid(cookie.Value) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead && !sameOrigin(r, cfg.CookieSecure) {
				authJSON(w, http.StatusForbidden, false, "cross-origin request rejected")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if isAPIRequest(r.URL.Path, cfg.BasePath) || strings.Contains(r.Header.Get("Accept"), "text/event-stream") || query.Get("stream") == "true" {
			authJSON(w, http.StatusUnauthorized, false, "authentication required: use X-Auth-Key or a browser session")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(renderAuthChallengeHTML(cfg.BasePath)))
	})
}

func headerKey(r *http.Request) string {
	if key := r.Header.Get(HeaderAuthKey); key != "" {
		return strings.TrimSpace(key)
	}
	if key := r.Header.Get("Authorization"); strings.HasPrefix(key, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(key, "Bearer "))
	}
	return ""
}
func ValidateKey(provided, expected string) bool {
	return provided != "" && expected != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}
func isAPIRequest(path, basePath string) bool {
	return strings.HasPrefix(path, "/api/") || basePath != "" && strings.HasPrefix(path, basePath+"/api/")
}
func sameOrigin(r *http.Request, secure bool) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return r.Header.Get("Sec-Fetch-Site") != "cross-site"
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host) && (u.Scheme == "http" || u.Scheme == "https") && (!(secure || r.TLS != nil) || u.Scheme == "https")
}
func authJSON(w http.ResponseWriter, status int, success bool, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	result := map[string]any{"success": success}
	if message != "" {
		result["error"] = message
	}
	_ = json.NewEncoder(w).Encode(result)
}
func jsonString(s string) string { value, _ := json.Marshal(s); return string(value) }

// renderAuthChallengeHTML returns a modern dark-themed authentication challenge page.
func renderAuthChallengeHTML(basePath string) string {
	redirectPath := "/"
	if basePath != "" {
		redirectPath = basePath + "/"
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>身份鉴权认证 · Nginx Online Web Builder</title>
  <style>
    :root {
      --bg-dark: #090d16;
      --card-bg: #0f172a;
      --border-color: #1e293b;
      --accent: #009639;
      --accent-hover: #00b344;
      --text-primary: #f8fafc;
      --text-secondary: #94a3b8;
    }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
      background: var(--bg-dark);
      color: var(--text-primary);
      min-height: 100vh;
      display: flex;
      align-items: center;
      justify-content: center;
      padding: 20px;
    }
    .auth-card {
      background: var(--card-bg);
      border: 1px solid var(--border-color);
      border-radius: 12px;
      padding: 32px 28px;
      max-width: 440px;
      width: 100%%;
      box-shadow: 0 10px 30px rgba(0, 0, 0, 0.5);
      text-align: center;
    }
    .logo-wrap {
      display: flex;
      justify-content: center;
      align-items: center;
      margin-bottom: 16px;
    }
    .nginx-logo {
      width: 48px;
      height: 48px;
      fill: var(--accent);
    }
    h1 {
      font-size: 20px;
      font-weight: 700;
      margin-bottom: 8px;
      letter-spacing: -0.3px;
    }
    p.desc {
      color: var(--text-secondary);
      font-size: 13px;
      line-height: 1.5;
      margin-bottom: 24px;
    }
    .form-group {
      text-align: left;
      margin-bottom: 18px;
    }
    label {
      display: block;
      font-size: 12px;
      font-weight: 600;
      color: var(--text-secondary);
      margin-bottom: 6px;
    }
    input[type="text"], input[type="password"] {
      width: 100%%;
      padding: 10px 14px;
      background: #060911;
      border: 1px solid var(--border-color);
      border-radius: 6px;
      color: #fff;
      font-family: monospace;
      font-size: 14px;
      outline: none;
      transition: border-color 0.2s;
    }
    input[type="text"]:focus, input[type="password"]:focus {
      border-color: var(--accent);
    }
    .btn-submit {
      width: 100%%;
      background: var(--accent);
      color: #fff;
      border: none;
      border-radius: 6px;
      padding: 11px;
      font-size: 14px;
      font-weight: 600;
      cursor: pointer;
      transition: background 0.2s;
    }
    .btn-submit:hover {
      background: var(--accent-hover);
    }
    .hint-box {
      margin-top: 20px;
      background: rgba(255, 255, 255, 0.03);
      border: 1px dashed var(--border-color);
      border-radius: 6px;
      padding: 10px 12px;
      font-size: 11px;
      color: var(--text-secondary);
      line-height: 1.5;
      text-align: left;
    }
    .hint-box code {
      color: #38bdf8;
      font-family: monospace;
    }
    .error-msg {
      color: #ef4444;
      font-size: 12px;
      margin-top: 8px;
      display: none;
    }
  </style>
</head>
<body>
  <div class="auth-card">
    <div class="logo-wrap">
      <svg class="nginx-logo" viewBox="0 0 24 24">
        <path d="M12 2L2 7v10l10 5 10-5V7L12 2zm0 2.8l7 3.5v7.4l-7 3.5-7-3.5V8.3l7-3.5z"/>
      </svg>
    </div>
    <h1>访问鉴权验证</h1>
    <p class="desc">当前 Nginx 在线编译平台已启用安全鉴权，请输入访问凭证密钥以进入系统。</p>

    <form id="auth-form">
      <div class="form-group">
        <label for="key-input">访问鉴权密钥 (Access Key):</label>
        <input type="text" id="key-input" placeholder="输入控制台打印的 32 位鉴权 Key" autocomplete="off" autofocus required>
        <div id="error-msg" class="error-msg">密钥不能为空</div>
      </div>
      <button type="submit" class="btn-submit">验证并进入系统</button>
    </form>

    <div class="hint-box">
      💡 <strong>快捷访问提示：</strong><br>
      请输入管理员配置的访问密钥。<br>
      未配置固定密钥时，可在本次服务启动日志中查看自动生成的密钥。
    </div>
  </div>

  <script>
    const basePath = %s;
    const redirectTarget = %s;
    try { localStorage.removeItem("nginx_builder_key"); } catch (_) {}
    document.getElementById("auth-form").addEventListener("submit", async function(e) {
      e.preventDefault();
      const input = document.getElementById("key-input");
      const error = document.getElementById("error-msg");
      try {
        const response = await fetch(basePath + "/api/auth/login", {
          method: "POST", credentials: "same-origin",
          headers: {"Content-Type": "application/json"},
          body: JSON.stringify({key: input.value.trim()})
        });
        input.value = "";
        if (!response.ok) throw new Error("Invalid key or session unavailable");
        window.location.replace(redirectTarget);
      } catch (_) { error.style.display = "block"; }
    });
  </script>
</body>
</html>
`, jsonString(basePath), jsonString(redirectPath))
}
