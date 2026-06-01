package tasks

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAudioFilenameFromKey(t *testing.T) {
	cases := []struct {
		key  string
		want string
	}{
		{"jobs/company/prefix/audio.webm", "audio.webm"},
		{"jobs/company/prefix/recording.m4a", "recording.m4a"},
		{"audio.webm", "audio.webm"},           // no slashes — returns full key
		{"a/b/c/d/e.mp4", "e.mp4"},              // deep nesting
		{"jobs/company/prefix/", ""},            // trailing slash — empty filename
		{"", ""},                                // empty key
	}

	for _, tc := range cases {
		got := audioFilenameFromKey(tc.key)
		if got != tc.want {
			t.Errorf("audioFilenameFromKey(%q): want %q, got %q", tc.key, tc.want, got)
		}
	}
}

func TestNewProcessJobHandler_DefaultHTTPClient(t *testing.T) {
	h := NewProcessJobHandler(nil, nil, "test-key")
	if h.httpClient == nil {
		t.Fatal("httpClient must not be nil")
	}
	if h.httpClient.Timeout != defaultGroqTimeout {
		t.Errorf("httpClient.Timeout: want %v, got %v", defaultGroqTimeout, h.httpClient.Timeout)
	}
	if h.httpClient == http.DefaultClient {
		t.Error("httpClient must not be http.DefaultClient")
	}
	if h.groqURL != groqTranscriptionURL {
		t.Errorf("groqURL: want %q, got %q", groqTranscriptionURL, h.groqURL)
	}
}

// newTestHandler returns a ProcessJobHandler wired to srv for use in transcribeAudio tests.
func newTestHandler(t *testing.T, srv *httptest.Server) *ProcessJobHandler {
	t.Helper()
	return &ProcessJobHandler{
		groqKey:    "test-key",
		httpClient: srv.Client(),
		groqURL:    srv.URL,
	}
}

func TestTranscribeAudio_Success(t *testing.T) {
	want := "Technician replaced the water heater."
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("want POST, got %s", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("Authorization: want %q, got %q", "Bearer test-key", auth)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
		}
		fmt.Fprint(w, want)
	}))
	defer srv.Close()

	h := newTestHandler(t, srv)
	got, err := h.transcribeAudio(context.Background(), strings.NewReader("fake audio bytes"), "audio.webm")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("transcript: want %q, got %q", want, got)
	}
}

func TestTranscribeAudio_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	h := newTestHandler(t, srv)
	_, err := h.transcribeAudio(context.Background(), strings.NewReader("fake audio"), "audio.webm")
	if err == nil {
		t.Fatal("expected error for non-200 response, got nil")
	}
}

func TestTranscribeAudio_EmptyTranscript(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Intentionally empty body — simulates Groq returning no transcript.
	}))
	defer srv.Close()

	h := newTestHandler(t, srv)
	got, err := h.transcribeAudio(context.Background(), strings.NewReader("fake audio"), "audio.webm")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("want empty transcript, got %q", got)
	}
}

func TestTranscribeAudio_ServerUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.Close() // shut down immediately so requests fail with a connection error

	h := newTestHandler(t, srv)
	_, err := h.transcribeAudio(context.Background(), strings.NewReader("fake audio"), "audio.webm")
	if err == nil {
		t.Fatal("expected error for closed server, got nil")
	}
}
