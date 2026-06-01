package tasks

import (
	"net/http"
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
}
