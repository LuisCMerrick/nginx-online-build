package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"nginx-builder/internal/config"
	"strings"
	"time"
)

const (
	// CookieName is the standard cookie storing client auth credential
	CookieName = "nginx_builder_key"
	// QueryParamKey represents URL parameter ?key=
	QueryParamKey = "key"
	// QueryParamToken represents URL parameter ?token=
	QueryParamToken = "token"
	// QueryParamAuth represents URL parameter ?auth=
	QueryParamAuth = "auth"
	// HeaderAuthKey is the HTTP request header for API clients
	HeaderAuthKey = "X-Auth-Key"
)

// GenerateRandomKey creates a cryptographically secure 16-byte (32 hex characters) secret key.
func GenerateRandomKey() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("nb_%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// Middleware wraps an http.Handler to enforce URL / Cookie / Header authentication.
func Middleware(cfg *config.Config, next http.Handler) http.Handler {
	if !cfg.AuthEnabled {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providedKey := ExtractKey(r)
		if ValidateKey(providedKey, cfg.AuthKey) {
			// If authenticated via URL query or header, set cookie so subsequent browser requests don't need ?key=
			if c, err := r.Cookie(CookieName); err != nil || c.Value != cfg.AuthKey {
				cookiePath := "/"
				if cfg.BasePath != "" {
					cookiePath = cfg.BasePath
				}
				http.SetCookie(w, &http.Cookie{
					Name:     CookieName,
					Value:    cfg.AuthKey,
					Path:     cookiePath,
					HttpOnly: false, // allow client-side scripts to read/maintain session
					SameSite: http.SameSiteLaxMode,
					MaxAge:   86400 * 30, // 30 days
				})
			}
			next.ServeHTTP(w, r)
			return
		}

		// Check if it's an API request or SSE stream
		isAPI := isAPIRequest(r.URL.Path, cfg.BasePath)
		isSSE := strings.Contains(r.Header.Get("Accept"), "text/event-stream") || r.URL.Query().Get("stream") == "true"

		if isAPI || isSSE {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": false,
				"error":   "401 Unauthorized: missing or invalid access key. Append ?key=<key> or provide X-Auth-Key header.",
			})
			return
		}

		// Render stylish 401 Auth Challenge UI for browser visitors
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(renderAuthChallengeHTML(cfg.BasePath)))
	})
}

// ExtractKey extracts authentication key from URL query, Cookies, or HTTP Headers.
func ExtractKey(r *http.Request) string {
	// 1. URL Query parameter (?key=..., ?token=..., ?auth=...)
	if k := r.URL.Query().Get(QueryParamKey); k != "" {
		return strings.TrimSpace(k)
	}
	if k := r.URL.Query().Get(QueryParamToken); k != "" {
		return strings.TrimSpace(k)
	}
	if k := r.URL.Query().Get(QueryParamAuth); k != "" {
		return strings.TrimSpace(k)
	}

	// 2. Cookie
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		return strings.TrimSpace(c.Value)
	}

	// 3. HTTP Header (X-Auth-Key or Authorization: Bearer <key>)
	if h := r.Header.Get(HeaderAuthKey); h != "" {
		return strings.TrimSpace(h)
	}
	if auth := r.Header.Get("Authorization"); auth != "" {
		if strings.HasPrefix(auth, "Bearer ") {
			return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		}
	}

	return ""
}

// ValidateKey compares the provided key with expected key in constant time to prevent timing attacks.
func ValidateKey(provided, expected string) bool {
	if provided == "" || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func isAPIRequest(path, basePath string) bool {
	if strings.HasPrefix(path, "/api/") {
		return true
	}
	if basePath != "" && strings.HasPrefix(path, basePath+"/api/") {
		return true
	}
	return false
}

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
      服务启动时已在终端控制台打印专属鉴权链接，形如：<br>
      <code>?key=&lt;auth_key&gt;</code>，直接点击链接即可免密快速登录。
    </div>
  </div>

  <script>
    const basePath = "%s";
    const redirectTarget = "%s";

    document.getElementById("auth-form").addEventListener("submit", function(e) {
      e.preventDefault();
      const key = document.getElementById("key-input").value.trim();
      if (!key) {
        document.getElementById("error-msg").style.display = "block";
        return;
      }
      // Save cookie and redirect with ?key= to establish full session
      const cookiePath = basePath || "/";
      document.cookie = "%s=" + encodeURIComponent(key) + "; path=" + cookiePath + "; max-age=2592000; SameSite=Lax";
      
      let target = redirectTarget;
      target += (target.includes("?") ? "&" : "?") + "key=" + encodeURIComponent(key);
      window.location.href = target;
    });
  </script>
</body>
</html>
`, basePath, redirectPath, CookieName)
}
