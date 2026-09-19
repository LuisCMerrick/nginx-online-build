package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
