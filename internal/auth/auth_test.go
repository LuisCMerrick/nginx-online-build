package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"nginx-builder/internal/auth"
	"nginx-builder/internal/config"
	"strings"
	"testing"
)

func TestAuthMiddleware(t *testing.T) {
	testKey := "test_secret_key_123456"
	cfg := &config.Config{
		AuthEnabled: true,
		AuthKey:     testKey,
		BasePath:    "",
	}

	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ACCESS_GRANTED"))
	})

	mw := auth.Middleware(cfg, dummyHandler)

	t.Run("API request without key returns 401 JSON", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/nginx/versions", nil)
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected status 401, got %d", w.Code)
		}
		var res map[string]any
		if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
			t.Fatalf("failed to decode JSON response: %v", err)
		}
		if res["success"] != false {
			t.Fatalf("expected success: false, got %v", res["success"])
		}
	})

	t.Run("API request with query param ?key= succeeds and sets cookie", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/nginx/versions?key="+testKey, nil)
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", w.Code)
		}
		if w.Body.String() != "ACCESS_GRANTED" {
			t.Fatalf("expected ACCESS_GRANTED, got %s", w.Body.String())
		}
		cookieHeader := w.Header().Get("Set-Cookie")
		if !strings.Contains(cookieHeader, auth.CookieName+"="+testKey) {
			t.Fatalf("expected Set-Cookie with auth key, got %s", cookieHeader)
		}
	})

	t.Run("API request with X-Auth-Key header succeeds", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/nginx/versions", nil)
		req.Header.Set("X-Auth-Key", testKey)
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", w.Code)
		}
		if w.Body.String() != "ACCESS_GRANTED" {
			t.Fatalf("expected ACCESS_GRANTED, got %s", w.Body.String())
		}
	})

	t.Run("API request with Cookie succeeds", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/nginx/versions", nil)
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: testKey})
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", w.Code)
		}
		if w.Body.String() != "ACCESS_GRANTED" {
			t.Fatalf("expected ACCESS_GRANTED, got %s", w.Body.String())
		}
	})

	t.Run("HTML request without key returns 401 HTML challenge form", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected status 401, got %d", w.Code)
		}
		body := w.Body.String()
		if !strings.Contains(body, "访问鉴权验证") {
			t.Fatalf("expected HTML challenge containing 访问鉴权验证, got %s", body)
		}
		if !strings.Contains(body, "key-input") {
			t.Fatalf("expected HTML challenge containing key-input form")
		}
	})

	t.Run("HTML request with ?key= succeeds", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?key="+testKey, nil)
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", w.Code)
		}
		if w.Body.String() != "ACCESS_GRANTED" {
			t.Fatalf("expected ACCESS_GRANTED, got %s", w.Body.String())
		}
	})

	t.Run("When auth is disabled (--no-auth), any request passes without key", func(t *testing.T) {
		disabledCfg := &config.Config{
			AuthEnabled: false,
		}
		mwDisabled := auth.Middleware(disabledCfg, dummyHandler)

		req := httptest.NewRequest("GET", "/api/nginx/versions", nil)
		w := httptest.NewRecorder()
		mwDisabled.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", w.Code)
		}
		if w.Body.String() != "ACCESS_GRANTED" {
			t.Fatalf("expected ACCESS_GRANTED, got %s", w.Body.String())
		}
	})
}
