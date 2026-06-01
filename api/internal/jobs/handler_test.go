package jobs

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgtype"
)

func newJobRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// nil pool, r2 and asynqClient — safe for paths that fail before touching them.
	r.POST("/v1/jobs", CreateHandler(nil, nil, nil))
	return r
}

func buildMultipartBody(t *testing.T, fields map[string]string, audioFilename, audioContent string) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	if audioFilename != "" {
		part, err := w.CreateFormFile("audio", audioFilename)
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := io.WriteString(part, audioContent); err != nil {
			t.Fatalf("write audio: %v", err)
		}
	}

	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("write field %q: %v", k, err)
		}
	}

	w.Close()
	return &body, w.FormDataContentType()
}

func doJobRequest(t *testing.T, router *gin.Engine, body *bytes.Buffer, ct string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/jobs", body)
	req.Header.Set("Content-Type", ct)
	router.ServeHTTP(w, req)
	return w
}

func TestCreateHandler_MissingAudio(t *testing.T) {
	router := newJobRouter()
	body, ct := buildMultipartBody(t, map[string]string{"client_name": "Acme"}, "", "")
	w := doJobRequest(t, router, body, ct)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateHandler_InvalidAudioType(t *testing.T) {
	router := newJobRouter()
	// Upload a .pdf file — not an allowed audio type.
	body, ct := buildMultipartBody(t,
		map[string]string{"client_name": "Acme"},
		"report.pdf", "fake-pdf-content",
	)
	w := doJobRequest(t, router, body, ct)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateHandler_MissingClientName(t *testing.T) {
	router := newJobRouter()
	// Valid audio type, but no client_name.
	body, ct := buildMultipartBody(t, nil, "voice.webm", "fake-audio")
	w := doJobRequest(t, router, body, ct)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateHandler_InvalidJobLat(t *testing.T) {
	router := newJobRouter()
	body, ct := buildMultipartBody(t, map[string]string{
		"client_name": "Acme",
		"job_lat":     "not-a-number",
	}, "voice.webm", "fake-audio")
	w := doJobRequest(t, router, body, ct)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateHandler_InvalidJobLng(t *testing.T) {
	router := newJobRouter()
	body, ct := buildMultipartBody(t, map[string]string{
		"client_name": "Acme",
		"job_lng":     "bad",
	}, "voice.webm", "fake-audio")
	w := doJobRequest(t, router, body, ct)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateHandler_InvalidPhotoType(t *testing.T) {
	router := newJobRouter()
	// Valid audio + an invalid photo type (.pdf). Photo validation runs before
	// UUID parsing so this must return 400 without needing a real JWT context.
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	audioPart, _ := w.CreateFormFile("audio", "voice.webm")
	io.WriteString(audioPart, "fake-audio")
	photoPart, _ := w.CreateFormFile("photos[]", "doc.pdf")
	io.WriteString(photoPart, "fake-pdf")
	w.WriteField("client_name", "Acme")
	w.Close()

	rec := doJobRequest(t, router, &body, w.FormDataContentType())
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---- Helper function unit tests ----

func TestIsAllowedAudioType(t *testing.T) {
	allowed := []string{
		"audio/webm", "video/webm",
		"audio/mp4", "video/mp4",
		"audio/m4a", "audio/x-m4a",
	}
	for _, ct := range allowed {
		if !isAllowedAudioType(ct) {
			t.Errorf("isAllowedAudioType(%q): want true, got false", ct)
		}
	}

	disallowed := []string{
		"application/octet-stream",
		"image/jpeg",
		"application/pdf",
		"text/plain",
		"",
	}
	for _, ct := range disallowed {
		if isAllowedAudioType(ct) {
			t.Errorf("isAllowedAudioType(%q): want false, got true", ct)
		}
	}
}

func TestDetectContentType(t *testing.T) {
	// Use mime.TypeByExtension to get platform-accurate expected values.
	webmByExt := mime.TypeByExtension(".webm")
	m4aByExt := mime.TypeByExtension(".m4a")

	cases := []struct {
		filename string
		supplied string
		want     string
	}{
		{"audio.webm", "audio/webm", webmByExt},   // extension wins over supplied
		{"recording.m4a", "audio/webm", m4aByExt}, // extension wins
		{"unknown.xyz", "audio/webm", "audio/webm"},     // unknown extension → use supplied
		{"unknown.xyz", "", "application/octet-stream"}, // unknown ext + no supplied → fallback
		{"noext", "audio/mp4", "audio/mp4"},             // no extension → use supplied
	}

	for _, tc := range cases {
		got := detectContentType(tc.filename, tc.supplied)
		if got != tc.want {
			t.Errorf("detectContentType(%q, %q): want %q, got %q", tc.filename, tc.supplied, tc.want, got)
		}
	}
}

func TestOptText(t *testing.T) {
	if r := optText(""); r.Valid {
		t.Error("optText(\"\") should return invalid (null) text")
	}
	if r := optText("hello"); !r.Valid || r.String != "hello" {
		t.Errorf("optText(\"hello\"): got %+v", r)
	}
}

func TestRandomHex(t *testing.T) {
	s, err := randomHex(8)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s) != 16 { // 8 bytes → 16 hex chars
		t.Errorf("randomHex(8): want 16 chars, got %d", len(s))
	}

	s2, _ := randomHex(8)
	if s == s2 {
		// statistically near-impossible for 8 random bytes to collide
		t.Error("randomHex returned identical values on two calls")
	}
}

func TestUUIDString(t *testing.T) {
	cases := []struct {
		name  string
		id    pgtype.UUID
		want  string
		isErr bool
	}{
		{
			name: "null UUID returns error",
			id:   pgtype.UUID{},
			isErr: true,
		},
		{
			name: "zero UUID",
			id:   pgtype.UUID{Bytes: [16]byte{}, Valid: true},
			want: "00000000-0000-0000-0000-000000000000",
		},
		{
			name: "known bytes",
			id: pgtype.UUID{
				Bytes: [16]byte{
					0x01, 0x02, 0x03, 0x04,
					0x05, 0x06,
					0x07, 0x08,
					0x09, 0x0a,
					0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
				},
				Valid: true,
			},
			want: "01020304-0506-0708-090a-0b0c0d0e0f10",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := uuidString(tc.id)
			if tc.isErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestIsAllowedPhotoType(t *testing.T) {
	allowed := []string{"image/jpeg", "image/png", "image/webp", "image/heic", "image/heif"}
	for _, ct := range allowed {
		if !isAllowedPhotoType(ct) {
			t.Errorf("isAllowedPhotoType(%q): want true, got false", ct)
		}
	}

	disallowed := []string{
		"application/pdf",
		"text/plain",
		"audio/webm",
		"image/gif",
		"",
	}
	for _, ct := range disallowed {
		if isAllowedPhotoType(ct) {
			t.Errorf("isAllowedPhotoType(%q): want false, got true", ct)
		}
	}
}
