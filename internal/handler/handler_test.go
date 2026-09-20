package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"nginx-builder/internal/config"
	"nginx-builder/internal/model"
	"strings"
	"testing"
)

func TestGetOptionsUsesFrontendPathDefaultFieldNames(t *testing.T) {
	h := NewHandler(nil)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/nginx/options", nil)

	h.handleGetOptions(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	var payload struct {
		PathDefaults map[string]map[string]string `json:"path_defaults"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	prefix, ok := payload.PathDefaults["prefix"]
	if !ok {
		t.Fatal("expected prefix path default")
	}
	if prefix["flag"] != "--prefix" {
		t.Fatalf("expected lower-case flag field, got %#v", prefix)
	}
	if prefix["default"] != "/usr/local/nginx" {
		t.Fatalf("expected lower-case default field, got %#v", prefix)
	}
	if prefix["description"] == "" {
		t.Fatalf("expected lower-case description field, got %#v", prefix)
	}
}

func TestParseBuildRequestPath(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		buildID     string
		action      string
		shouldParse bool
	}{
		{name: "root artifact", path: "/api/builds/build-123/artifact", buildID: "build-123", action: "artifact", shouldParse: true},
		{name: "subpath artifact", path: "/nginx-online-build/api/builds/build-123/artifact", buildID: "build-123", action: "artifact", shouldParse: true},
		{name: "missing build id", path: "/nginx-online-build/api/builds/", shouldParse: false},
		{name: "unrelated path", path: "/nginx-online-build/api/nginx/options", shouldParse: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buildID, action, ok := parseBuildRequestPath(tt.path)
			if ok != tt.shouldParse || buildID != tt.buildID || action != tt.action {
				t.Fatalf("parseBuildRequestPath(%q) = (%q, %q, %v), want (%q, %q, %v)", tt.path, buildID, action, ok, tt.buildID, tt.action, tt.shouldParse)
			}
		})
	}
}

func TestStrictJSONAndRequestBodyLimit(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"versoin":"stable"}`, 400},
		{`{"third_party_sources":{"unknown":true}}`, 400},
		{`{} {}`, 400},
		{`{"version":"` + strings.Repeat("x", int(config.MaxRequestBytes)) + `"}`, 413},
		{`{"version":"stable"}`, 0},
	} {
		// Negative content length simulates a chunked body without an advertised size.
		r := httptest.NewRequest("POST", "/api/builds", strings.NewReader(tc.body))
		r.ContentLength = -1
		w := httptest.NewRecorder()
		var req model.CreateBuildRequest
		ok := decodeRequest(w, r, &req)
		if tc.status == 0 {
			if !ok || req.Version != "stable" {
				t.Fatal("valid request rejected")
			}
		} else if ok || w.Code != tc.status {
			t.Fatalf("status=%d want=%d body=%s", w.Code, tc.status, w.Body)
		}
	}
}
