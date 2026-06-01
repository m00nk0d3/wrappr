// Package jobs provides HTTP handlers for the /v1/jobs resource.
package jobs

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"mime"
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/m00nk0d3/wrappr/api/internal/db"
	"github.com/m00nk0d3/wrappr/api/internal/middleware"
	"github.com/m00nk0d3/wrappr/api/internal/pipeline"
	"github.com/m00nk0d3/wrappr/api/internal/r2"
)

// maxUploadSize is the hard cap on the total request body size (32 MB).
// Enforced by http.MaxBytesReader before parsing; also used as the in-memory
// buffer threshold for ParseMultipartForm so larger payloads spill to temp files.
const maxUploadSize = 32 << 20

// defaultReportLanguage is used when the caller omits report_language.
const defaultReportLanguage = "en"

// CreateHandler returns a Gin handler for POST /v1/jobs.
//
// Accepts multipart/form-data:
//   - audio       — audio file (WebM or M4A/MP4); required
//   - photos[]    — zero or more photo files; optional
//   - client_name — string; required
//   - (all other fields optional — see issue #10)
//
// On success:
//
//	202 {"job_id": "<uuid>"}
func CreateHandler(pool *pgxpool.Pool, r2Client *r2.Client, asynqClient *asynq.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		userIDStr := middleware.GetUserID(c)
		companyIDStr := middleware.GetCompanyID(c)

		// Enforce the hard body-size cap before parsing so oversized requests
		// are rejected immediately rather than after buffering.
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadSize)
		if err := c.Request.ParseMultipartForm(maxUploadSize); err != nil {
			log.Printf("jobs: parse multipart form: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid multipart form"})
			return
		}

		// ---- Required: audio file ----
		audioFile, audioHeader, err := c.Request.FormFile("audio")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "audio file is required"})
			return
		}
		defer audioFile.Close()

		audioContentType := detectContentType(audioHeader.Filename, audioHeader.Header.Get("Content-Type"))
		if !isAllowedAudioType(audioContentType) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "audio must be WebM or M4A/MP4"})
			return
		}

		// ---- Required: client_name ----
		clientName := c.Request.FormValue("client_name")
		if clientName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "client_name is required"})
			return
		}

		// ---- Optional form fields ----
		clientEmail := c.Request.FormValue("client_email")
		clientPhone := c.Request.FormValue("client_phone")
		jobAddress := c.Request.FormValue("job_address")
		jobLatStr := c.Request.FormValue("job_lat")
		jobLngStr := c.Request.FormValue("job_lng")
		jobType := c.Request.FormValue("job_type")
		technicianNotes := c.Request.FormValue("technician_notes")
		reportLanguage := c.Request.FormValue("report_language")
		if reportLanguage == "" {
			reportLanguage = defaultReportLanguage
		}

		// Parse decimal lat/lng strings into pgtype.Numeric (null when absent).
		var jobLat, jobLng pgtype.Numeric
		if jobLatStr != "" {
			if err := jobLat.Scan(jobLatStr); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid job_lat: must be a decimal number"})
				return
			}
		}
		if jobLngStr != "" {
			if err := jobLng.Scan(jobLngStr); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid job_lng: must be a decimal number"})
				return
			}
		}

		// Parse JWT-provided UUIDs into pgtype.UUID.
		var userUUID, companyUUID pgtype.UUID
		if err := userUUID.Scan(userIDStr); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid user ID in token"})
			return
		}
		if err := companyUUID.Scan(companyIDStr); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid company ID in token"})
			return
		}

		// Generate a random path prefix so R2 keys can be built before the job
		// record is inserted (the DB auto-generates the job UUID).
		keyPrefix, err := randomHex(8)
		if err != nil {
			log.Printf("jobs: generate key prefix: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			return
		}

		// uploadedKeys tracks every R2 object written in this request so they can
		// be cleaned up if a later step (DB insert, task enqueue) fails.
		var uploadedKeys []string
		cleanupUploads := func() {
			for _, k := range uploadedKeys {
				if delErr := r2Client.Delete(ctx, k); delErr != nil {
					log.Printf("jobs: cleanup R2 key %q: %v", k, delErr)
				}
			}
		}

		// ---- Upload audio to R2 ----
		// Use a deterministic key with only the file extension from the user-supplied
		// filename to prevent path injection via browser-controlled filenames.
		audioKey := fmt.Sprintf("jobs/%s/%s/audio%s", companyIDStr, keyPrefix, filepath.Ext(audioHeader.Filename))
		if err := r2Client.Upload(ctx, audioKey, audioFile, audioContentType, audioHeader.Size); err != nil {
			log.Printf("jobs: upload audio: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to upload audio"})
			return
		}
		uploadedKeys = append(uploadedKeys, audioKey)

		// ---- Upload photos to R2 (optional) ----
		var photoKeys []string
		if mf := c.Request.MultipartForm; mf != nil {
			for i, fh := range mf.File["photos[]"] {
				f, err := fh.Open()
				if err != nil {
					log.Printf("jobs: open photo %q: %v", fh.Filename, err)
					cleanupUploads()
					c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read photo file"})
					return
				}
				ct := detectContentType(fh.Filename, fh.Header.Get("Content-Type"))
				// Use a positional key (photo_<i><ext>) so filenames with path separators
				// or duplicate names cannot collide or traverse the key namespace.
				photoKey := fmt.Sprintf("jobs/%s/%s/photos/photo_%d%s", companyIDStr, keyPrefix, i, filepath.Ext(fh.Filename))
				if uploadErr := r2Client.Upload(ctx, photoKey, f, ct, fh.Size); uploadErr != nil {
					f.Close()
					log.Printf("jobs: upload photo %q: %v", fh.Filename, uploadErr)
					cleanupUploads()
					c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to upload photo"})
					return
				}
				f.Close()
				uploadedKeys = append(uploadedKeys, photoKey)
				photoKeys = append(photoKeys, photoKey)
			}
		}
		if photoKeys == nil {
			photoKeys = []string{} // keep the slice non-nil for DB
		}

		// ---- Create job record ----
		q := db.New(pool)
		job, err := q.CreateJob(ctx, db.CreateJobParams{
			CompanyID:        companyUUID,
			TechnicianID:     userUUID,
			ClientName:       clientName,
			ClientEmail:      optText(clientEmail),
			ClientPhone:      optText(clientPhone),
			JobAddress:       optText(jobAddress),
			JobLat:           jobLat,
			JobLng:           jobLng,
			JobType:          optText(jobType),
			TechnicianNotes:  optText(technicianNotes),
			AudioUrl:         pgtype.Text{String: audioKey, Valid: true},
			PhotoUrls:        photoKeys,
			DetectedLanguage: pgtype.Text{},
			ReportLanguage:   pgtype.Text{String: reportLanguage, Valid: true},
		})
		if err != nil {
			log.Printf("jobs: create job: %v", err)
			cleanupUploads()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			return
		}

		jobIDStr, err := uuidString(job.ID)
		if err != nil {
			log.Printf("jobs: encode job ID: %v", err)
			cleanupUploads()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			return
		}

		// ---- Enqueue Asynq task ----
		payload, err := json.Marshal(pipeline.ProcessJobPayload{JobID: jobIDStr})
		if err != nil {
			log.Printf("jobs: marshal task payload: %v", err)
			cleanupUploads()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			return
		}

		task := asynq.NewTask(pipeline.TaskTypeProcessJob, payload, asynq.MaxRetry(3))
		if _, err := asynqClient.Enqueue(task); err != nil {
			log.Printf("jobs: enqueue task for job %s: %v", jobIDStr, err)
			// Mark the job as failed so it does not sit in "queued" with no worker
			// picking it up. The client will receive a 500 and can retry cleanly.
			// Trade-off: R2 files are deleted (see cleanupUploads below) but the DB
			// job record is retained with status=failed. The stale audio_url/photo_urls
			// won't be accessed again because no worker picks up failed jobs; the
			// record is kept for audit purposes.
			if _, statusErr := q.UpdateJobStatus(ctx, db.UpdateJobStatusParams{
				ID:             job.ID,
				PipelineStatus: "failed",
			}); statusErr != nil {
				log.Printf("jobs: mark job %s failed after enqueue error: %v", jobIDStr, statusErr)
			}
			cleanupUploads()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to enqueue job"})
			return
		}

		c.JSON(http.StatusAccepted, gin.H{"job_id": jobIDStr})
	}
}

// optText wraps a possibly-empty string as a pgtype.Text (null when empty).
func optText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

// uuidString converts a pgtype.UUID to its canonical hyphenated hex form.
func uuidString(id pgtype.UUID) (string, error) {
	if !id.Valid {
		return "", fmt.Errorf("UUID is null")
	}
	b := id.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// randomHex returns n random bytes encoded as a lowercase hex string (2n chars).
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// isAllowedAudioType returns true for WebM and M4A/MP4 content types accepted
// by the Groq Whisper API.
func isAllowedAudioType(ct string) bool {
	switch ct {
	case "audio/webm", "video/webm",
		"audio/mp4", "video/mp4",
		"audio/m4a", "audio/x-m4a":
		return true
	}
	return false
}

// detectContentType returns the MIME type for a file, preferring the extension
// over the browser-supplied value when the extension is recognised.
func detectContentType(filename, supplied string) string {
	if ext := filepath.Ext(filename); ext != "" {
		if ct := mime.TypeByExtension(ext); ct != "" {
			return ct
		}
	}
	if supplied != "" {
		return supplied
	}
	return "application/octet-stream"
}
