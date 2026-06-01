// Package tasks contains Asynq task handler implementations for the wrappr
// background worker pipeline.
package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/m00nk0d3/wrappr/api/internal/db"
	"github.com/m00nk0d3/wrappr/api/internal/jobs"
	"github.com/m00nk0d3/wrappr/api/internal/r2"
)

const (
	groqTranscriptionURL = "https://api.groq.com/openai/v1/audio/transcriptions"
	groqModel            = "whisper-large-v3"

	// unclearTranscript is stored when Groq returns an empty transcript.
	unclearTranscript = "Voice memo was unclear — technician notes unavailable."
)

// ProcessJobHandler implements asynq.Handler for the "pipeline:process_job" task.
// It downloads the job's audio from R2, sends it to Groq Whisper, and stores the
// resulting transcript in the database.
type ProcessJobHandler struct {
	pool    *pgxpool.Pool
	r2      *r2.Client
	groqKey string
}

// NewProcessJobHandler constructs a ProcessJobHandler.
func NewProcessJobHandler(pool *pgxpool.Pool, r2Client *r2.Client, groqKey string) *ProcessJobHandler {
	return &ProcessJobHandler{pool: pool, r2: r2Client, groqKey: groqKey}
}

// ProcessTask handles a single "pipeline:process_job" task.
// Returning a non-nil error causes Asynq to retry the task automatically.
func (h *ProcessJobHandler) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var payload jobs.ProcessJobPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		// Malformed payload — no point retrying.
		return fmt.Errorf("process_job: unmarshal payload: %w", asynq.SkipRetry)
	}
	if payload.JobID == "" {
		return fmt.Errorf("process_job: empty job_id: %w", asynq.SkipRetry)
	}

	var jobUUID pgtype.UUID
	if err := jobUUID.Scan(payload.JobID); err != nil {
		return fmt.Errorf("process_job: parse job UUID %q: %w", payload.JobID, asynq.SkipRetry)
	}

	q := db.New(h.pool)

	// 1. Load job from DB.
	job, err := q.GetJobByID(ctx, jobUUID)
	if err != nil {
		return fmt.Errorf("process_job: get job %s: %w", payload.JobID, err)
	}

	if !job.AudioUrl.Valid || job.AudioUrl.String == "" {
		return fmt.Errorf("process_job: job %s has no audio_url: %w", payload.JobID, asynq.SkipRetry)
	}

	// 2. Mark as "transcribing".
	if _, err := q.UpdateJobStatus(ctx, db.UpdateJobStatusParams{
		ID:             jobUUID,
		PipelineStatus: "transcribing",
	}); err != nil {
		return fmt.Errorf("process_job: update status to transcribing: %w", err)
	}

	// 3. Download audio bytes from R2.
	audioReader, err := h.r2.Download(ctx, job.AudioUrl.String)
	if err != nil {
		return fmt.Errorf("process_job: download audio for job %s: %w", payload.JobID, err)
	}
	defer audioReader.Close()

	audioBytes, err := io.ReadAll(audioReader)
	if err != nil {
		return fmt.Errorf("process_job: read audio bytes for job %s: %w", payload.JobID, err)
	}

	// 4. Transcribe via Groq Whisper.
	audioFilename := audioFilenameFromKey(job.AudioUrl.String)
	transcript, err := h.transcribeAudio(ctx, audioBytes, audioFilename)
	if err != nil {
		return fmt.Errorf("process_job: transcribe job %s: %w", payload.JobID, err)
	}
	if transcript == "" {
		log.Printf("process_job: empty transcript for job %s — using placeholder", payload.JobID)
		transcript = unclearTranscript
	}

	// 5. Persist transcript + advance status to "transcribed".
	if _, err := q.UpdateJobTranscript(ctx, db.UpdateJobTranscriptParams{
		ID:             jobUUID,
		PipelineStatus: "transcribed",
		Transcript:     pgtype.Text{String: transcript, Valid: true},
	}); err != nil {
		return fmt.Errorf("process_job: store transcript for job %s: %w", payload.JobID, err)
	}

	log.Printf("process_job: job %s transcribed successfully", payload.JobID)
	return nil
}

// transcribeAudio calls the Groq Whisper API and returns the transcript text.
func (h *ProcessJobHandler) transcribeAudio(ctx context.Context, audioBytes []byte, filename string) (string, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(audioBytes); err != nil {
		return "", fmt.Errorf("write audio bytes: %w", err)
	}
	if err := w.WriteField("model", groqModel); err != nil {
		return "", fmt.Errorf("write model field: %w", err)
	}
	if err := w.WriteField("response_format", "text"); err != nil {
		return "", fmt.Errorf("write response_format field: %w", err)
	}
	w.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, groqTranscriptionURL, &body)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+h.groqKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("groq API returned %d: %s", resp.StatusCode, string(respBody))
	}

	return string(respBody), nil
}

// audioFilenameFromKey extracts the filename component from an R2 object key.
// For a key like "jobs/company/prefix/audio.webm" it returns "audio.webm".
func audioFilenameFromKey(key string) string {
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == '/' {
			return key[i+1:]
		}
	}
	return key
}
