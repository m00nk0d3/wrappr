// Package tasks contains Asynq task handler implementations for the wrappr
// background worker pipeline.
package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/m00nk0d3/wrappr/api/internal/db"
	"github.com/m00nk0d3/wrappr/api/internal/pipeline"
	"github.com/m00nk0d3/wrappr/api/internal/r2"
)

const (
	groqTranscriptionURL = "https://api.groq.com/openai/v1/audio/transcriptions"
	groqModel            = "whisper-large-v3"

	// unclearTranscript is stored when Groq returns an empty transcript.
	unclearTranscript = "Voice memo was unclear — technician notes unavailable."

	// defaultGroqTimeout covers worst-case transcription latency for large audio
	// files without letting a stalled request hang the worker goroutine indefinitely.
	defaultGroqTimeout = 90 * time.Second

	// defaultLLMTimeout is the per-call deadline for LLM chat completions.
	// LLM calls should respond within seconds; a tighter bound lets Asynq retry
	// sooner if the provider is degraded rather than waiting a full 90 s.
	defaultLLMTimeout = 30 * time.Second
)

// ProcessJobHandler implements asynq.Handler for the "pipeline:process_job" task.
// It downloads the job's audio from R2, sends it to Groq Whisper for transcription,
// then calls the LLM to extract structured data and stores everything in the database.
type ProcessJobHandler struct {
	pool       *pgxpool.Pool
	r2         *r2.Client
	groqKey    string
	geminiKey  string
	httpClient *http.Client
	// groqURL is the Groq Whisper transcription endpoint. Overridable in tests.
	groqURL string
	// llmChatURL is the Groq chat completions endpoint. Overridable in tests.
	llmChatURL string
	// geminiChatURL is the Gemini generateContent base URL. Overridable in tests.
	geminiChatURL string
}

// NewProcessJobHandler constructs a ProcessJobHandler with a default HTTP client.
// geminiKey is optional; when set, it enables Gemini 1.5 Flash as a fallback
// if Groq returns HTTP 429 during LLM extraction.
func NewProcessJobHandler(pool *pgxpool.Pool, r2Client *r2.Client, groqKey, geminiKey string) *ProcessJobHandler {
	return &ProcessJobHandler{
		pool:          pool,
		r2:            r2Client,
		groqKey:       groqKey,
		geminiKey:     geminiKey,
		httpClient:    &http.Client{Timeout: defaultGroqTimeout},
		groqURL:       groqTranscriptionURL,
		llmChatURL:    groqChatURL,
		geminiChatURL: geminiBaseURL,
	}
}

// ProcessTask handles a single "pipeline:process_job" task.
// Returning a non-nil error causes Asynq to retry the task automatically.
func (h *ProcessJobHandler) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var payload pipeline.ProcessJobPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		// Malformed payload — no point retrying.
		return fmt.Errorf("process_job: unmarshal payload (%v): %w", err, asynq.SkipRetry)
	}
	if payload.JobID == "" {
		return fmt.Errorf("process_job: empty job_id: %w", asynq.SkipRetry)
	}

	var jobUUID pgtype.UUID
	if err := jobUUID.Scan(payload.JobID); err != nil {
		return fmt.Errorf("process_job: parse job UUID %q (%v): %w", payload.JobID, err, asynq.SkipRetry)
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

	// 3. Download audio from R2 and stream directly to Groq — avoids buffering
	// the entire file in memory (up to 32 MB per job).
	audioReader, err := h.r2.Download(ctx, job.AudioUrl.String)
	if err != nil {
		return fmt.Errorf("process_job: download audio for job %s: %w", payload.JobID, err)
	}
	defer audioReader.Close()

	// 4. Transcribe via Groq Whisper.
	audioFilename := audioFilenameFromKey(job.AudioUrl.String)
	transcript, err := h.transcribeAudio(ctx, audioReader, audioFilename)
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

	log.Printf("process_job: job %s transcribed (%d chars) — starting LLM extraction", payload.JobID, len(transcript))

	// 6. Mark as "extracting".
	if _, err := q.UpdateJobStatus(ctx, db.UpdateJobStatusParams{
		ID:             jobUUID,
		PipelineStatus: "extracting",
	}); err != nil {
		return fmt.Errorf("process_job: update status to extracting for job %s: %w", payload.JobID, err)
	}

	// 7. Extract structured data from the transcript using the LLM.
	// report_language is set at job-creation time from company.default_language.
	// A tighter timeout is used here so a stalled LLM call fails fast and lets
	// Asynq retry sooner, independent of the transcription HTTP client timeout.
	reportLanguage := job.ReportLanguage.String
	llmCtx, llmCancel := context.WithTimeout(ctx, defaultLLMTimeout)
	defer llmCancel()
	extraction, modelUsed, err := h.extractJobData(llmCtx, transcript, reportLanguage)
	if err != nil {
		return fmt.Errorf("process_job: LLM extraction for job %s: %w", payload.JobID, err)
	}

	// 8. Compute derived values.
	tags := normalizeTags(extraction.JobTags, maxTags, allowedTagsSet)
	rawJSON, err := json.Marshal(extraction)
	if err != nil {
		log.Printf("process_job: marshal extraction JSON for job %s: %v — storing empty object", payload.JobID, err)
		rawJSON = []byte("{}")
	}
	now := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}

	var laborHours pgtype.Numeric
	if extraction.LaborHoursEstimated != nil {
		s := strconv.FormatFloat(*extraction.LaborHoursEstimated, 'f', 1, 64)
		if err := laborHours.Scan(s); err != nil {
			log.Printf("process_job: invalid labor_hours_estimated %v for job %s: %v", *extraction.LaborHoursEstimated, payload.JobID, err)
		}
	}

	// Nil slices become SQL NULL; normalise to empty so the array columns are
	// consistently non-null even when the LLM omits them.
	if extraction.SafetyConcerns == nil {
		extraction.SafetyConcerns = []string{}
	}
	if tags == nil {
		tags = []string{}
	}

	// 9. Persist all AI-extracted fields and advance pipeline to "completed".
	if _, err := q.UpdateJobPipeline(ctx, db.UpdateJobPipelineParams{
		ID:                 jobUUID,
		PipelineStatus:     "completed",
		Transcript:         pgtype.Text{String: transcript, Valid: true},
		AiSummary:          pgtype.Text{String: extraction.Summary, Valid: extraction.Summary != ""},
		AiWorkPerformed:    pgtype.Text{String: extraction.WorkPerformed, Valid: extraction.WorkPerformed != ""},
		AiFollowUpNotes:    pgtype.Text{String: extraction.FollowUpNotes, Valid: extraction.FollowUpNotes != ""},
		AiWarrantyNotes:    pgtype.Text{String: extraction.WarrantyNotes, Valid: extraction.WarrantyNotes != ""},
		AiJobCategory:      pgtype.Text{String: sanitizeCategory(extraction.JobCategory), Valid: extraction.JobCategory != ""},
		AiClientSentiment:  pgtype.Text{String: sanitizeSentiment(extraction.ClientSentiment), Valid: extraction.ClientSentiment != ""},
		AiLaborHours:       laborHours,
		AiFollowUpRequired: extraction.FollowUpRequired,
		AiSafetyConcerns:   extraction.SafetyConcerns,
		AiTags:             tags,
		AiRawJson:          rawJSON,
		AiModelUsed:        pgtype.Text{String: modelUsed, Valid: true},
		AiProcessedAt:      now,
		PdfUrl:             pgtype.Text{},      // populated by PDF generation step (future issue)
		EmailSentAt:        pgtype.Timestamptz{}, // populated by email step (future issue)
		CompletedAt:        now,
	}); err != nil {
		return fmt.Errorf("process_job: store AI data for job %s: %w", payload.JobID, err)
	}

	// 10. Insert materials into job_materials (best-effort — log errors, don't fail).
	for _, mat := range extraction.MaterialsUsed {
		if mat.Name == "" {
			continue
		}
		if _, err := q.CreateJobMaterial(ctx, db.CreateJobMaterialParams{
			JobID:    jobUUID,
			Name:     mat.Name,
			Quantity: pgtype.Text{String: mat.Quantity, Valid: mat.Quantity != ""},
			Unit:     pgtype.Text{String: mat.Unit, Valid: mat.Unit != ""},
		}); err != nil {
			log.Printf("process_job: insert material %q for job %s: %v", mat.Name, payload.JobID, err)
		}
	}

	// 11. Insert recommendations into job_recommendations (best-effort).
	for _, rec := range extraction.Recommendations {
		if rec.Description == "" {
			continue
		}
		if _, err := q.CreateJobRecommendation(ctx, db.CreateJobRecommendationParams{
			JobID:              jobUUID,
			Description:        rec.Description,
			Urgency:            sanitizeUrgency(rec.Urgency),
			EstimatedCostRange: pgtype.Text{String: rec.EstimatedCostRange, Valid: rec.EstimatedCostRange != ""},
		}); err != nil {
			log.Printf("process_job: insert recommendation for job %s: %v", payload.JobID, err)
		}
	}

	// 12. Insert tags into job_tags (best-effort — unique constraint guards against duplicates).
	for _, tag := range tags {
		if _, err := q.CreateJobTag(ctx, db.CreateJobTagParams{
			JobID: jobUUID,
			Tag:   tag,
		}); err != nil {
			log.Printf("process_job: insert tag %q for job %s: %v", tag, payload.JobID, err)
		}
	}

	log.Printf("process_job: job %s completed successfully (model: %s)", payload.JobID, modelUsed)
	return nil
}

// transcribeAudio streams audioReader to the Groq Whisper API and returns the
// transcript text. It uses io.Pipe so the audio is forwarded to Groq without
// being fully buffered in memory.
func (h *ProcessJobHandler) transcribeAudio(ctx context.Context, audioReader io.Reader, filename string) (string, error) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)

	// Stream the multipart body to Groq in a goroutine so the HTTP request
	// begins reading before the entire audio has been written.
	go func() {
		err := func() error {
			part, err := mw.CreateFormFile("file", filename)
			if err != nil {
				return fmt.Errorf("create form file: %w", err)
			}
			if _, err := io.Copy(part, audioReader); err != nil {
				return fmt.Errorf("copy audio: %w", err)
			}
			if err := mw.WriteField("model", groqModel); err != nil {
				return fmt.Errorf("write model field: %w", err)
			}
			if err := mw.WriteField("response_format", "text"); err != nil {
				return fmt.Errorf("write response_format field: %w", err)
			}
			return mw.Close()
		}()
		pw.CloseWithError(err)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.groqURL, pr)
	if err != nil {
		pr.CloseWithError(err)
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+h.groqKey)

	resp, err := h.httpClient.Do(req)
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
