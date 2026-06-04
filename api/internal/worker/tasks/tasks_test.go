package tasks

import (
	"context"
	"encoding/json"
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
	h := NewProcessJobHandler(nil, nil, "test-key", "")
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
	if h.llmChatURL != groqChatURL {
		t.Errorf("llmChatURL: want %q, got %q", groqChatURL, h.llmChatURL)
	}
	if h.geminiChatURL != geminiBaseURL {
		t.Errorf("geminiChatURL: want %q, got %q", geminiBaseURL, h.geminiChatURL)
	}
	if h.geminiKey != "" {
		t.Errorf("geminiKey: want empty, got %q", h.geminiKey)
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

// ── LLM extraction helpers ────────────────────────────────────────────────────

// newLLMTestHandler returns a ProcessJobHandler whose llmChatURL points at srv.
func newLLMTestHandler(t *testing.T, srv *httptest.Server) *ProcessJobHandler {
	t.Helper()
	return &ProcessJobHandler{
		groqKey:       "test-groq-key",
		httpClient:    srv.Client(),
		llmChatURL:    srv.URL,
		geminiChatURL: srv.URL, // default; override per test when needed
	}
}

// validGroqChatResponse returns a JSON-encoded Groq chat completion response
// whose content field is the given JSON string.
func validGroqChatResponse(content string) string {
	resp := groqChatResponse{
		Choices: []struct {
			Message groqMessage `json:"message"`
		}{
			{Message: groqMessage{Role: "assistant", Content: content}},
		},
	}
	b, _ := json.Marshal(resp)
	return string(b)
}

// validGeminiResponse returns a JSON-encoded Gemini generateContent response.
func validGeminiResponse(content string) string {
	resp := geminiResponse{
		Candidates: []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		}{
			{Content: struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			}{Parts: []struct {
				Text string `json:"text"`
			}{{Text: content}}}},
		},
	}
	b, _ := json.Marshal(resp)
	return string(b)
}

// minimalExtractionJSON is the smallest valid llmExtractionResult payload.
const minimalExtractionJSON = `{"summary":"Water heater replaced.","work_performed":"Replaced 50-gallon water heater.","materials_used":[],"issues_found":[],"recommendations":[],"follow_up_required":false,"follow_up_notes":null,"labor_hours_estimated":2.0,"job_tags":["water_heater"],"client_sentiment":"positive","warranty_notes":null,"safety_concerns":[],"job_category":"plumbing"}`

// ── callGroqLLM ───────────────────────────────────────────────────────────────

func TestCallGroqLLM_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("want POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type: want application/json, got %s", ct)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-groq-key" {
			t.Errorf("Authorization: want Bearer test-groq-key, got %s", auth)
		}
		fmt.Fprint(w, validGroqChatResponse(minimalExtractionJSON))
	}))
	defer srv.Close()

	h := newLLMTestHandler(t, srv)
	got, err := h.callGroqLLM(context.Background(), "system", "user msg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != minimalExtractionJSON {
		t.Errorf("content: want %q, got %q", minimalExtractionJSON, got)
	}
}

func TestCallGroqLLM_RateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	h := newLLMTestHandler(t, srv)
	_, err := h.callGroqLLM(context.Background(), "system", "user msg")
	if err == nil {
		t.Fatal("expected errGroqRateLimited, got nil")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error %q should mention rate limited", err)
	}
}

func TestCallGroqLLM_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	h := newLLMTestHandler(t, srv)
	_, err := h.callGroqLLM(context.Background(), "system", "user")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestCallGroqLLM_NoChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"choices":[]}`)
	}))
	defer srv.Close()

	h := newLLMTestHandler(t, srv)
	_, err := h.callGroqLLM(context.Background(), "system", "user")
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

// ── callGeminiLLM ─────────────────────────────────────────────────────────────

func TestCallGeminiLLM_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("want POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.RawQuery, "key=") {
			t.Error("request URL should contain ?key= param")
		}
		fmt.Fprint(w, validGeminiResponse(minimalExtractionJSON))
	}))
	defer srv.Close()

	h := &ProcessJobHandler{
		geminiKey:     "test-gemini-key",
		httpClient:    srv.Client(),
		geminiChatURL: srv.URL,
	}
	got, err := h.callGeminiLLM(context.Background(), "system", "user msg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != minimalExtractionJSON {
		t.Errorf("content: want %q, got %q", minimalExtractionJSON, got)
	}
}

func TestCallGeminiLLM_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	h := &ProcessJobHandler{
		geminiKey:     "key",
		httpClient:    srv.Client(),
		geminiChatURL: srv.URL,
	}
	_, err := h.callGeminiLLM(context.Background(), "system", "user")
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
}

// ── extractJobData ────────────────────────────────────────────────────────────

func TestExtractJobData_GroqSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, validGroqChatResponse(minimalExtractionJSON))
	}))
	defer srv.Close()

	h := newLLMTestHandler(t, srv)
	result, model, err := h.extractJobData(context.Background(), "some transcript", "en")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model != groqLLMModel {
		t.Errorf("model: want %q, got %q", groqLLMModel, model)
	}
	if result.Summary != "Water heater replaced." {
		t.Errorf("summary: want %q, got %q", "Water heater replaced.", result.Summary)
	}
	if result.JobCategory != "plumbing" {
		t.Errorf("job_category: want plumbing, got %q", result.JobCategory)
	}
}

func TestExtractJobData_GroqRateLimitFallsBackToGemini(t *testing.T) {
	groqSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer groqSrv.Close()

	geminiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, validGeminiResponse(minimalExtractionJSON))
	}))
	defer geminiSrv.Close()

	h := &ProcessJobHandler{
		groqKey:       "groq-key",
		geminiKey:     "gemini-key",
		httpClient:    geminiSrv.Client(), // plain-HTTP client reaches both test servers
		llmChatURL:    groqSrv.URL,
		geminiChatURL: geminiSrv.URL,
	}

	result, model, err := h.extractJobData(context.Background(), "transcript", "en")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model != geminiModel {
		t.Errorf("model: want %q, got %q", geminiModel, model)
	}
	if result.Summary != "Water heater replaced." {
		t.Errorf("summary mismatch: got %q", result.Summary)
	}
}

func TestExtractJobData_GroqRateLimitNoGeminiKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	h := &ProcessJobHandler{
		groqKey:    "groq-key",
		geminiKey:  "", // no fallback configured
		httpClient: srv.Client(),
		llmChatURL: srv.URL,
	}
	_, _, err := h.extractJobData(context.Background(), "transcript", "en")
	if err == nil {
		t.Fatal("expected error when Groq rate-limited and no Gemini key")
	}
}

func TestExtractJobData_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, validGroqChatResponse("not valid json {{{"))
	}))
	defer srv.Close()

	h := newLLMTestHandler(t, srv)
	_, _, err := h.extractJobData(context.Background(), "transcript", "en")
	if err == nil {
		t.Fatal("expected JSON parse error")
	}
}

// ── sanitize helpers ──────────────────────────────────────────────────────────

func TestSanitizeSentiment(t *testing.T) {
	cases := map[string]string{
		"positive":    "positive",
		"POSITIVE":    "positive",
		"negative":    "negative",
		"neutral":     "neutral",
		"unavailable": "unavailable",
		"happy":       "unavailable",
		"":            "unavailable",
	}
	for input, want := range cases {
		if got := sanitizeSentiment(input); got != want {
			t.Errorf("sanitizeSentiment(%q): want %q, got %q", input, want, got)
		}
	}
}

func TestSanitizeCategory(t *testing.T) {
	cases := map[string]string{
		"electrical":  "electrical",
		"PLUMBING":    "plumbing",
		"hvac":        "hvac",
		"cleaning":    "cleaning",
		"landscaping": "landscaping",
		"general":     "general",
		"other":       "other",
		"roofing":     "other",
		"":            "other",
	}
	for input, want := range cases {
		if got := sanitizeCategory(input); got != want {
			t.Errorf("sanitizeCategory(%q): want %q, got %q", input, want, got)
		}
	}
}

func TestSanitizeUrgency(t *testing.T) {
	cases := map[string]string{
		"immediate":        "immediate",
		"IMMEDIATE":        "immediate",
		"within_30_days":   "within_30_days",
		"when_convenient":  "when_convenient",
		"soon":             "when_convenient",
		"":                 "when_convenient",
	}
	for input, want := range cases {
		if got := sanitizeUrgency(input); got != want {
			t.Errorf("sanitizeUrgency(%q): want %q, got %q", input, want, got)
		}
	}
}

// ── normalizeTags ─────────────────────────────────────────────────────────────

func TestNormalizeTags(t *testing.T) {
	t.Run("lowercases and trims", func(t *testing.T) {
		got := normalizeTags([]string{"  LEAK_REPAIR ", "Plumbing"}, 5, nil)
		if got[0] != "leak_repair" {
			t.Errorf("want leak_repair, got %q", got[0])
		}
		if got[1] != "plumbing" {
			t.Errorf("want plumbing, got %q", got[1])
		}
	})

	t.Run("deduplicates", func(t *testing.T) {
		got := normalizeTags([]string{"repair", "repair", "REPAIR"}, 5, nil)
		if len(got) != 1 {
			t.Errorf("want 1 unique tag, got %d: %v", len(got), got)
		}
	})

	t.Run("truncates to max", func(t *testing.T) {
		got := normalizeTags([]string{"a", "b", "c", "d", "e", "f", "g"}, 5, nil)
		if len(got) != 5 {
			t.Errorf("want 5 tags, got %d", len(got))
		}
	})

	t.Run("skips empty strings", func(t *testing.T) {
		got := normalizeTags([]string{"", "   ", "repair"}, 5, nil)
		if len(got) != 1 || got[0] != "repair" {
			t.Errorf("want [repair], got %v", got)
		}
	})

	t.Run("nil input returns empty", func(t *testing.T) {
		got := normalizeTags(nil, 5, nil)
		if len(got) != 0 {
			t.Errorf("want empty, got %v", got)
		}
	})

	t.Run("filters out-of-vocabulary tags", func(t *testing.T) {
		allowed := map[string]struct{}{"repair": {}, "inspection": {}}
		got := normalizeTags([]string{"repair", "roofing", "INSPECTION", "unknown_tag"}, 5, allowed)
		if len(got) != 2 {
			t.Errorf("want 2 in-vocab tags, got %d: %v", len(got), got)
		}
		if got[0] != "repair" || got[1] != "inspection" {
			t.Errorf("want [repair inspection], got %v", got)
		}
	})
}

// ── buildExtractionPrompt ─────────────────────────────────────────────────────

func TestBuildExtractionPrompt(t *testing.T) {
	t.Run("contains report language", func(t *testing.T) {
		system, user := buildExtractionPrompt("the transcript", "pt-BR")
		if !strings.Contains(system, "pt-BR") {
			t.Error("system prompt should contain the report language")
		}
		if !strings.Contains(user, "the transcript") {
			t.Error("user message should contain the transcript")
		}
	})

	t.Run("defaults to en when empty", func(t *testing.T) {
		system, _ := buildExtractionPrompt("t", "")
		if !strings.Contains(system, "language: en") {
			t.Errorf("expected default language en in prompt, got: %s", system)
		}
	})

	t.Run("contains controlled tag vocabulary", func(t *testing.T) {
		system, _ := buildExtractionPrompt("t", "en")
		if !strings.Contains(system, "leak_repair") {
			t.Error("system prompt should list controlled tag vocabulary")
		}
	})
}
