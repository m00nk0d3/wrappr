package tasks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestBuildReportHTML_ContainsJobID verifies that the rendered HTML contains
// the short job ID derived from the payload UUID.
func TestBuildReportHTML_ContainsJobID(t *testing.T) {
	data := reportData{
		JobShortID:     "ABC12345",
		TechnicianName: "John Smith",
		CompanyName:    "Acme Corp",
	}
	html, err := buildReportHTML(data)
	if err != nil {
		t.Fatalf("buildReportHTML() error = %v", err)
	}
	if !strings.Contains(html, "ABC12345") {
		t.Error("rendered HTML does not contain the job short ID")
	}
}

// TestBuildReportHTML_ContainsTechnicianName verifies that the technician name
// appears in the header card of the rendered report.
func TestBuildReportHTML_ContainsTechnicianName(t *testing.T) {
	data := reportData{
		JobShortID:     "TESTJOB1",
		TechnicianName: "Jane Doe",
		CompanyName:    "Beta LLC",
	}
	html, err := buildReportHTML(data)
	if err != nil {
		t.Fatalf("buildReportHTML() error = %v", err)
	}
	if !strings.Contains(html, "Jane Doe") {
		t.Error("rendered HTML does not contain the technician name")
	}
}

// TestBuildReportHTML_PhotoGrid verifies that photo URLs appear in the rendered
// HTML when photos are provided.
func TestBuildReportHTML_PhotoGrid(t *testing.T) {
	photoURL := "https://example.com/photo1.jpg"
	data := reportData{
		JobShortID:     "PHOTOJOB",
		TechnicianName: "Tech",
		CompanyName:    "Test Co",
		Photos:         []string{photoURL},
	}
	html, err := buildReportHTML(data)
	if err != nil {
		t.Fatalf("buildReportHTML() error = %v", err)
	}
	if !strings.Contains(html, photoURL) {
		t.Error("rendered HTML does not contain the photo URL")
	}
	if !strings.Contains(html, `<div class="photo-grid">`) {
		t.Error("rendered HTML does not contain the photo grid div")
	}
}

// TestBuildReportHTML_EmptyPhotos verifies that a valid HTML document is
// produced even when no photos are provided (photo section omitted).
func TestBuildReportHTML_EmptyPhotos(t *testing.T) {
	data := reportData{
		JobShortID:     "NOPHOTO",
		TechnicianName: "Tech",
		CompanyName:    "Test Co",
		Photos:         nil,
	}
	html, err := buildReportHTML(data)
	if err != nil {
		t.Fatalf("buildReportHTML() error = %v", err)
	}
	// Footer must always be present.
	if !strings.Contains(html, "Powered by Wrappr") {
		t.Error("rendered HTML does not contain the footer")
	}
	// Photo grid section must be absent (the CSS class still appears in <style>).
	if strings.Contains(html, `<div class="photo-grid">`) {
		t.Error("rendered HTML unexpectedly contains photo grid div when Photos is nil")
	}
}

// TestBuildReportHTML_MarkdownRendered verifies that markdown content in the
// Summary field is rendered to HTML (e.g. ** becomes <strong>).
func TestBuildReportHTML_MarkdownRendered(t *testing.T) {
	data := reportData{
		JobShortID:     "MDJOB",
		TechnicianName: "Tech",
		CompanyName:    "Test Co",
		Summary:        renderMarkdown("**bold text** and _italic_"),
	}
	html, err := buildReportHTML(data)
	if err != nil {
		t.Fatalf("buildReportHTML() error = %v", err)
	}
	if !strings.Contains(html, "<strong>bold text</strong>") {
		t.Error("rendered HTML does not contain expected <strong> from markdown")
	}
}

// TestPostToGotenberg_Success verifies that PDF bytes returned by a mock
// Gotenberg server are passed through unchanged.
func TestPostToGotenberg_Success(t *testing.T) {
	wantPDF := []byte("%PDF-1.4 test document")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(wantPDF)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	got, err := postToGotenberg(context.Background(), client, srv.URL, "<html><body>test</body></html>")
	if err != nil {
		t.Fatalf("postToGotenberg() error = %v", err)
	}
	if string(got) != string(wantPDF) {
		t.Errorf("postToGotenberg() = %q, want %q", got, wantPDF)
	}
}

// TestPostToGotenberg_NonOKStatus verifies that a non-200 response from
// Gotenberg is surfaced as an error.
func TestPostToGotenberg_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	_, err := postToGotenberg(context.Background(), client, srv.URL, "<html><body>test</body></html>")
	if err == nil {
		t.Fatal("expected error for non-200 Gotenberg response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to mention HTTP status 500, got: %v", err)
	}
}

// TestNewGeneratePDFHandler_Defaults verifies that the constructor sets
// expected default values on the handler struct.
func TestNewGeneratePDFHandler_Defaults(t *testing.T) {
	h := NewGeneratePDFHandler(nil, nil, "http://localhost:3000", "https://pub.r2.dev")
	if h.gotenbergURL != "http://localhost:3000" {
		t.Errorf("gotenbergURL = %q, want %q", h.gotenbergURL, "http://localhost:3000")
	}
	if h.r2PublicURL != "https://pub.r2.dev" {
		t.Errorf("r2PublicURL = %q, want %q", h.r2PublicURL, "https://pub.r2.dev")
	}
	if h.httpClient == nil {
		t.Fatal("httpClient is nil, want non-nil")
	}
	if h.httpClient.Timeout != defaultGotenbergTimeout {
		t.Errorf("httpClient.Timeout = %v, want %v", h.httpClient.Timeout, defaultGotenbergTimeout)
	}
}

// TestShortJobID verifies the first-segment truncation logic.
func TestShortJobID(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"550e8400-e29b-41d4-a716-446655440000", "550E8400"},
		{"abcdefgh", "ABCDEFGH"},
		{"ab", "AB"},
	}
	for _, tc := range cases {
		got := shortJobID(tc.input)
		if got != tc.want {
			t.Errorf("shortJobID(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
