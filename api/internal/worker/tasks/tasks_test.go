package tasks

import (
	"net/http"
	"testing"
	"time"
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

func TestGroqHTTPClientTimeout(t *testing.T) {
	if groqHTTPClient.Timeout != 90*time.Second {
		t.Errorf("groqHTTPClient.Timeout: want 90s, got %v", groqHTTPClient.Timeout)
	}
	if groqHTTPClient == http.DefaultClient {
		t.Error("groqHTTPClient must not be http.DefaultClient")
	}
}
