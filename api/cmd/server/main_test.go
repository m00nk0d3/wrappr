package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthHandler_OK(t *testing.T) {
	router := buildRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if got, want := w.Body.String(), `{"status":"ok"}`; got != want {
		t.Errorf("expected body %q, got %q", want, got)
	}
}

func TestBuildRouter_UnknownRoute(t *testing.T) {
	router := buildRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/not-found", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}
