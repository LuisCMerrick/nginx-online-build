package auth

import (
	"net/http"
	"net/http/httptest"
	"nginx-builder/internal/config"
	"strings"
	"testing"
	"time"
)

func TestAuthenticationAndSessionLifecycle(t *testing.T) {
	for _, base := range []string{"", "/nginx"} {
		t.Run(base, func(t *testing.T) {
			cfg := &config.Config{AuthEnabled: true, AuthKey: "fixed-test-secret", BasePath: base, CookieSecure: true}
			mw := Middleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("granted")) }))
			request := func(method, path, body string, cookie *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(method, "https://builder.example"+base+path, strings.NewReader(body))
				if cookie != nil {
					r.AddCookie(cookie)
				}
				for k, v := range headers {
					r.Header.Set(k, v)
				}
				w := httptest.NewRecorder()
				mw.ServeHTTP(w, r)
				return w
			}
			for _, path := range []string{"/api/builds", "/api/builds?key=fixed-test-secret"} {
				if w := request("GET", path, "", nil, nil); w.Code != 401 {
					t.Fatalf("unauthenticated API: %d", w.Code)
				}
			}
			for _, name := range []string{CookieName, "nginx_builder_key"} {
				if w := request("GET", "/api/builds", "", &http.Cookie{Name: name, Value: cfg.AuthKey}, nil); w.Code != 401 {
					t.Fatal("raw-key cookie accepted")
				}
			}
			for k, v := range map[string]string{"X-Auth-Key": cfg.AuthKey, "Authorization": "Bearer " + cfg.AuthKey} {
				if w := request("GET", "/api/builds", "", nil, map[string]string{k: v}); w.Code != 200 || len(w.Result().Cookies()) != 0 {
					t.Fatal("header auth failed or issued cookie")
				}
			}
			if w := request("GET", "/", "", nil, nil); w.Code != 401 || !strings.Contains(w.Body.String(), "key-input") {
				t.Fatal("missing login form")
			}
			if w := request("POST", "/api/auth/login", `{"key":"wrong"}`, nil, nil); w.Code != 401 {
				t.Fatal("invalid key accepted")
			}
			if w := request("POST", "/api/auth/login", `{"key":"fixed-test-secret"}`, nil, map[string]string{"Origin": "https://attacker.example"}); w.Code != 403 {
				t.Fatal("cross-origin login accepted")
			}
			w := request("POST", "/api/auth/login", `{"key":"fixed-test-secret"}`, nil, nil)
			if w.Code != 200 {
				t.Fatalf("login: %d %s", w.Code, w.Body)
			}
			var session *http.Cookie
			for _, c := range w.Result().Cookies() {
				if c.Name == CookieName {
					session = c
				}
			}
			if session == nil || !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteLaxMode || len(session.Value) != 64 || session.Value == cfg.AuthKey {
				t.Fatalf("unsafe session: %#v", session)
			}
			wantPath := base
			if wantPath == "" {
				wantPath = "/"
			}
			if session.Path != wantPath {
				t.Fatal("wrong cookie path")
			}
			if w := request("GET", "/api/builds", "", session, nil); w.Code != 200 {
				t.Fatal("session rejected")
			}
			if w := request("POST", "/api/builds", "{}", session, map[string]string{"Origin": "https://attacker.example"}); w.Code != 403 {
				t.Fatal("cross-origin mutation accepted")
			}
			if w := request("POST", "/api/builds", "{}", session, map[string]string{"Origin": "https://builder.example"}); w.Code != 200 {
				t.Fatal("same-origin mutation rejected")
			}
			w = request("GET", "/?key=fixed-test-secret&token=x&auth=y&lang=zh", "", nil, nil)
			if w.Code != 303 || w.Header().Get("Location") != base+"/?lang=zh" || w.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Fatalf("unsafe redirect: %d %v", w.Code, w.Header())
			}
			if w := request("POST", "/api/auth/logout", "", session, nil); w.Code != 200 {
				t.Fatal("logout failed")
			}
			if w := request("GET", "/api/builds", "", session, nil); w.Code != 401 {
				t.Fatal("revoked session accepted")
			}
		})
	}
}

func TestSessionExpiryAndCapacity(t *testing.T) {
	s := &sessionStore{sessions: map[string]time.Time{"expired": time.Now().Add(-time.Second)}}
	if s.valid("expired") {
		t.Fatal("expired session accepted")
	}
	for i := 0; i < maxSessions+5; i++ {
		if _, err := s.create(); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.sessions) != maxSessions {
		t.Fatalf("unbounded session store: %d", len(s.sessions))
	}
}

func TestAuthDisabledAndBodyLimit(t *testing.T) {
	mw := Middleware(&config.Config{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	for _, size := range []int{0, int(config.MaxRequestBytes) + 1} {
		r := httptest.NewRequest("POST", "/api/builds", strings.NewReader(strings.Repeat("x", size)))
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, r)
		want := 200
		if size > 0 {
			want = 413
		}
		if w.Code != want {
			t.Fatalf("status=%d want=%d", w.Code, want)
		}
	}
}
