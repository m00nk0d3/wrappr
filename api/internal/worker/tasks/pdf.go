// Package tasks contains Asynq task handler implementations for the wrappr
// background worker pipeline.
package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/m00nk0d3/wrappr/api/internal/db"
	"github.com/m00nk0d3/wrappr/api/internal/pipeline"
	"github.com/m00nk0d3/wrappr/api/internal/r2"
	"github.com/yuin/goldmark"
)

const (
	// maxPhotos is the maximum number of photos included in the PDF photo grid.
	maxPhotos = 10

	// photoPresignExpiry is how long pre-signed photo URLs stay valid.
	// Must be long enough for Gotenberg to fetch all photos after the URLs are generated.
	photoPresignExpiry = 10 * time.Minute

	// defaultGotenbergTimeout is the HTTP deadline for the Gotenberg PDF render call.
	defaultGotenbergTimeout = 60 * time.Second

	// pdfKeyFormat is the R2 object key template for generated PDF reports.
	pdfKeyFormat = "jobs/%s/%s/report.pdf"
)

// reportData holds all values rendered into the HTML report template.
type reportData struct {
	JobShortID       string
	SubmittedAt      string
	GPS              string
	TechnicianName   string
	CompanyName      string
	ClientName       string
	Address          string
	JobType          string
	JobCategory      string
	LaborHours       string
	ClientSentiment  string
	FollowUpRequired bool
	Summary          template.HTML
	WorkPerformed    template.HTML
	FollowUpNotes    template.HTML
	WarrantyNotes    template.HTML
	SafetyConcerns   []string
	Tags             []string
	Materials        []materialRow
	Recommendations  []recommendationRow
	Photos           []string
}

type materialRow struct {
	Name     string
	Quantity string
	Unit     string
}

type recommendationRow struct {
	Description        string
	Urgency            string
	EstimatedCostRange string
}

// reportTemplate is parsed once at package init to fail fast on template errors.
var reportTemplate = template.Must(template.New("report").Parse(reportHTMLTmpl))

// GeneratePDFHandler implements asynq.Handler for the "pipeline:generate_pdf" task.
// It renders a structured HTML report for the job, POSTs it to Gotenberg for
// PDF compilation, uploads the result to R2, and marks the job as "completed".
type GeneratePDFHandler struct {
	pool         *pgxpool.Pool
	r2           *r2.Client
	gotenbergURL string
	r2PublicURL  string
	httpClient   *http.Client
}

// NewGeneratePDFHandler constructs a GeneratePDFHandler.
// gotenbergURL is the base URL of the Gotenberg service (e.g. "http://localhost:3000").
// r2PublicURL is the optional public CDN prefix for building the pdf_url stored in the DB.
func NewGeneratePDFHandler(pool *pgxpool.Pool, r2Client *r2.Client, gotenbergURL, r2PublicURL string) *GeneratePDFHandler {
	return &GeneratePDFHandler{
		pool:         pool,
		r2:           r2Client,
		gotenbergURL: gotenbergURL,
		r2PublicURL:  r2PublicURL,
		httpClient:   &http.Client{Timeout: defaultGotenbergTimeout},
	}
}

// ProcessTask handles a single "pipeline:generate_pdf" task.
// Returning a non-nil error causes Asynq to retry the task automatically.
func (h *GeneratePDFHandler) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var payload pipeline.GeneratePDFPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("generate_pdf: unmarshal payload %w: %w", err, asynq.SkipRetry)
	}
	if payload.JobID == "" {
		return fmt.Errorf("generate_pdf: empty job_id: %w", asynq.SkipRetry)
	}

	var jobUUID pgtype.UUID
	if err := jobUUID.Scan(payload.JobID); err != nil {
		return fmt.Errorf("generate_pdf: parse job UUID %q: %w", payload.JobID, asynq.SkipRetry)
	}

	q := db.New(h.pool)

	// 1. Advance status so the dashboard shows progress.
	if _, err := q.UpdateJobStatus(ctx, db.UpdateJobStatusParams{
		ID:             jobUUID,
		PipelineStatus: "generating_pdf",
	}); err != nil {
		return fmt.Errorf("generate_pdf: update status for job %s: %w", payload.JobID, err)
	}

	// 2. Load job.
	job, err := q.GetJobByID(ctx, jobUUID)
	if err != nil {
		return fmt.Errorf("generate_pdf: get job %s: %w", payload.JobID, err)
	}

	// 3. Load company (for footer).
	company, err := q.GetCompanyByID(ctx, job.CompanyID)
	if err != nil {
		return fmt.Errorf("generate_pdf: get company for job %s: %w", payload.JobID, err)
	}

	// 4. Load technician (for header card).
	technician, err := q.GetUserByID(ctx, job.TechnicianID)
	if err != nil {
		return fmt.Errorf("generate_pdf: get technician for job %s: %w", payload.JobID, err)
	}

	// 5. Load materials and recommendations (best-effort).
	materials, err := q.ListJobMaterialsByJob(ctx, jobUUID)
	if err != nil {
		log.Printf("generate_pdf: load materials for job %s: %v", payload.JobID, err)
		materials = nil
	}
	recommendations, err := q.ListJobRecommendationsByJob(ctx, jobUUID)
	if err != nil {
		log.Printf("generate_pdf: load recommendations for job %s: %v", payload.JobID, err)
		recommendations = nil
	}

	// 6. Generate pre-signed photo URLs (up to maxPhotos, valid for photoPresignExpiry).
	photoKeys := job.PhotoUrls
	if len(photoKeys) > maxPhotos {
		photoKeys = photoKeys[:maxPhotos]
	}
	photoURLs := make([]string, 0, len(photoKeys))
	for _, key := range photoKeys {
		presigned, presignErr := h.r2.PresignGetURL(ctx, key, photoPresignExpiry)
		if presignErr != nil {
			log.Printf("generate_pdf: presign photo %q for job %s: %v", key, payload.JobID, presignErr)
			continue
		}
		photoURLs = append(photoURLs, presigned)
	}

	// 7. Build template data — convert pgtype values to plain Go types.
	matRows := make([]materialRow, 0, len(materials))
	for _, m := range materials {
		matRows = append(matRows, materialRow{
			Name:     m.Name,
			Quantity: textString(m.Quantity),
			Unit:     textString(m.Unit),
		})
	}
	recRows := make([]recommendationRow, 0, len(recommendations))
	for _, rec := range recommendations {
		recRows = append(recRows, recommendationRow{
			Description:        rec.Description,
			Urgency:            rec.Urgency,
			EstimatedCostRange: textString(rec.EstimatedCostRange),
		})
	}

	data := reportData{
		JobShortID:       shortJobID(payload.JobID),
		SubmittedAt:      formatTimestamp(job.SubmittedAt),
		GPS:              formatGPS(job.JobLat, job.JobLng),
		TechnicianName:   technician.Name,
		CompanyName:      company.Name,
		ClientName:       job.ClientName,
		Address:          textString(job.JobAddress),
		JobType:          textString(job.JobType),
		JobCategory:      textString(job.AiJobCategory),
		LaborHours:       numericString(job.AiLaborHours),
		ClientSentiment:  textString(job.AiClientSentiment),
		FollowUpRequired: job.AiFollowUpRequired,
		Summary:          renderMarkdown(textString(job.AiSummary)),
		WorkPerformed:    renderMarkdown(textString(job.AiWorkPerformed)),
		FollowUpNotes:    renderMarkdown(textString(job.AiFollowUpNotes)),
		WarrantyNotes:    renderMarkdown(textString(job.AiWarrantyNotes)),
		SafetyConcerns:   job.AiSafetyConcerns,
		Tags:             job.AiTags,
		Materials:        matRows,
		Recommendations:  recRows,
		Photos:           photoURLs,
	}

	// 8. Render HTML.
	htmlStr, err := buildReportHTML(data)
	if err != nil {
		return fmt.Errorf("generate_pdf: build HTML for job %s: %w", payload.JobID, err)
	}

	// 9. POST HTML to Gotenberg and receive PDF bytes.
	pdfBytes, err := postToGotenberg(ctx, h.httpClient, h.gotenbergURL, htmlStr)
	if err != nil {
		return fmt.Errorf("generate_pdf: gotenberg for job %s: %w", payload.JobID, err)
	}

	// 10. Upload PDF to R2.
	companyIDStr := pgUUIDString(job.CompanyID)
	jobIDStr := pgUUIDString(jobUUID)
	pdfKey := fmt.Sprintf(pdfKeyFormat, companyIDStr, jobIDStr)

	if err := h.r2.Upload(ctx, pdfKey, bytes.NewReader(pdfBytes), "application/pdf", int64(len(pdfBytes))); err != nil {
		return fmt.Errorf("generate_pdf: upload PDF for job %s: %w", payload.JobID, err)
	}

	// 11. Build public URL (falls back to R2 key when R2PublicURL is unset).
	pdfURL := pdfKey
	if h.r2PublicURL != "" {
		pdfURL = strings.TrimRight(h.r2PublicURL, "/") + "/" + pdfKey
	}

	// 12. Persist pdf_url and advance to "completed".
	if _, err := q.UpdateJobPdf(ctx, db.UpdateJobPdfParams{
		ID:             jobUUID,
		PdfUrl:         pgtype.Text{String: pdfURL, Valid: true},
		PipelineStatus: "completed",
	}); err != nil {
		return fmt.Errorf("generate_pdf: update job PDF for %s: %w", payload.JobID, err)
	}

	log.Printf("generate_pdf: job %s completed — PDF uploaded to %s", payload.JobID, pdfKey)
	return nil
}

// buildReportHTML renders the HTML report template with the provided data.
func buildReportHTML(data reportData) (string, error) {
	var buf bytes.Buffer
	if err := reportTemplate.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute report template: %w", err)
	}
	return buf.String(), nil
}

// postToGotenberg sends an HTML document to the Gotenberg Chromium HTML→PDF
// endpoint and returns the resulting PDF bytes.
// gotenbergURL is the base URL of the Gotenberg service (e.g. "http://localhost:3000").
func postToGotenberg(ctx context.Context, client *http.Client, gotenbergURL, htmlContent string) ([]byte, error) {
	endpoint := strings.TrimRight(gotenbergURL, "/") + "/forms/chromium/convert/html"

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)

	// Write the multipart body in a goroutine so the HTTP client can begin
	// reading the stream before all bytes are written.
	go func() {
		err := func() error {
			// Gotenberg requires the HTML in a part named "index.html".
			part, err := mw.CreateFormFile("index.html", "index.html")
			if err != nil {
				return fmt.Errorf("create form file: %w", err)
			}
			if _, err := io.WriteString(part, htmlContent); err != nil {
				return fmt.Errorf("write HTML: %w", err)
			}
			return mw.Close()
		}()
		pw.CloseWithError(err)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, pr)
	if err != nil {
		pr.CloseWithError(err)
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gotenberg returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// renderMarkdown converts a markdown string to sanitised HTML using goldmark.
// The returned template.HTML is safe to embed directly in html/template output.
//
// goldmark strips raw HTML by default (unsafe mode is opt-in via
// html.WithUnsafe()); do NOT enable unsafe mode here — AI-extracted content
// must never bypass the built-in sanitisation.
func renderMarkdown(md string) template.HTML {
	if md == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := goldmark.Convert([]byte(md), &buf); err != nil {
		// Fall back to escaped plain text so the report is never blank.
		return template.HTML("<p>" + template.HTMLEscapeString(md) + "</p>")
	}
	return template.HTML(buf.String())
}

// textString returns the string value of a pgtype.Text, or "" when invalid.
func textString(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}

// pgUUIDString converts a pgtype.UUID to its canonical hyphenated string form.
func pgUUIDString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuid.UUID(u.Bytes).String()
}

// numericString formats a pgtype.Numeric as a decimal string for display.
func numericString(n pgtype.Numeric) string {
	if !n.Valid {
		return ""
	}
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return ""
	}
	return strconv.FormatFloat(f.Float64, 'f', -1, 64)
}

// formatTimestamp formats a pgtype.Timestamptz for human-readable display in the report.
func formatTimestamp(ts pgtype.Timestamptz) string {
	if !ts.Valid {
		return ""
	}
	return ts.Time.UTC().Format("January 2, 2006 at 15:04 UTC")
}

// formatGPS returns a "lat, lng" string, or "" when either value is absent.
func formatGPS(lat, lng pgtype.Numeric) string {
	latStr := numericString(lat)
	lngStr := numericString(lng)
	if latStr == "" || lngStr == "" {
		return ""
	}
	return latStr + ", " + lngStr
}

// shortJobID returns the first segment of a UUID string (before the first '-'),
// upper-cased, for compact display in report headings.
func shortJobID(jobID string) string {
	if idx := strings.IndexByte(jobID, '-'); idx > 0 {
		return strings.ToUpper(jobID[:idx])
	}
	if len(jobID) > 8 {
		return strings.ToUpper(jobID[:8])
	}
	return strings.ToUpper(jobID)
}

// reportHTMLTmpl is the Go html/template source for the job PDF report.
// All CSS is inline; no external resources are referenced so Gotenberg can
// render it in a sandboxed Chromium session without network access.
const reportHTMLTmpl = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Job Report — {{.JobShortID}}</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Helvetica, Arial, sans-serif;
    color: #1a1a1a; background: #fff; padding: 40px; font-size: 14px; line-height: 1.6;
  }
  h2 {
    font-size: 15px; font-weight: 600; color: #374151;
    margin-bottom: 12px; padding-bottom: 6px; border-bottom: 1px solid #e5e7eb;
  }
  .header {
    background: #f8fafc; border: 1px solid #e2e8f0;
    border-radius: 8px; padding: 24px; margin-bottom: 28px;
  }
  .header h1 { font-size: 22px; font-weight: 700; color: #111827; margin-bottom: 16px; }
  .header-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
  .meta-item { display: flex; flex-direction: column; }
  .meta-label {
    font-size: 10px; font-weight: 700; color: #6b7280;
    text-transform: uppercase; letter-spacing: 0.06em;
  }
  .meta-value { font-size: 14px; color: #111827; margin-top: 2px; }
  .follow-up-flag { color: #dc2626; font-weight: 700; }
  .section { margin-bottom: 24px; }
  .section-content { color: #374151; }
  .section-content p { margin-bottom: 8px; }
  .section-content ul, .section-content ol { padding-left: 20px; margin-bottom: 8px; }
  .section-content li { margin-bottom: 4px; }
  .tags { display: flex; flex-wrap: wrap; gap: 6px; }
  .tag {
    background: #eff6ff; color: #1d4ed8;
    padding: 3px 10px; border-radius: 9999px; font-size: 12px; font-weight: 500;
  }
  .photo-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; }
  .photo-grid img {
    width: 100%; height: 180px; object-fit: cover;
    border-radius: 6px; border: 1px solid #e5e7eb;
  }
  table { width: 100%; border-collapse: collapse; font-size: 13px; }
  th {
    background: #f9fafb; text-align: left; padding: 8px 12px;
    border-bottom: 2px solid #e5e7eb; font-weight: 600; color: #374151;
  }
  td { padding: 8px 12px; border-bottom: 1px solid #f3f4f6; color: #374151; }
  .badge {
    display: inline-block; padding: 2px 8px;
    border-radius: 4px; font-size: 11px; font-weight: 600; text-transform: capitalize;
  }
  .badge-high    { background: #fee2e2; color: #b91c1c; }
  .badge-medium  { background: #fef3c7; color: #92400e; }
  .badge-low     { background: #d1fae5; color: #065f46; }
  .badge-unknown { background: #f3f4f6; color: #6b7280; }
  .safety {
    background: #fff7ed; border-left: 4px solid #f97316;
    padding: 10px 14px; border-radius: 0 6px 6px 0; margin-bottom: 8px;
    font-size: 13px; color: #9a3412;
  }
  .footer {
    margin-top: 40px; padding-top: 16px; border-top: 1px solid #e5e7eb;
    display: flex; justify-content: space-between; align-items: center;
    font-size: 11px; color: #9ca3af;
  }
  @page { margin: 18mm 15mm; }
</style>
</head>
<body>

<div class="header">
  <h1>Job Report</h1>
  <div class="header-grid">
    <div class="meta-item">
      <span class="meta-label">Job ID</span>
      <span class="meta-value">#{{.JobShortID}}</span>
    </div>
    <div class="meta-item">
      <span class="meta-label">Date</span>
      <span class="meta-value">{{.SubmittedAt}}</span>
    </div>
    <div class="meta-item">
      <span class="meta-label">Technician</span>
      <span class="meta-value">{{.TechnicianName}}</span>
    </div>
    <div class="meta-item">
      <span class="meta-label">Client</span>
      <span class="meta-value">{{.ClientName}}</span>
    </div>
    {{if .Address}}
    <div class="meta-item">
      <span class="meta-label">Address</span>
      <span class="meta-value">{{.Address}}</span>
    </div>
    {{end}}
    {{if .GPS}}
    <div class="meta-item">
      <span class="meta-label">GPS</span>
      <span class="meta-value">{{.GPS}}</span>
    </div>
    {{end}}
    {{if .JobType}}
    <div class="meta-item">
      <span class="meta-label">Job Type</span>
      <span class="meta-value">{{.JobType}}</span>
    </div>
    {{end}}
    {{if .JobCategory}}
    <div class="meta-item">
      <span class="meta-label">Category</span>
      <span class="meta-value">{{.JobCategory}}</span>
    </div>
    {{end}}
    {{if .LaborHours}}
    <div class="meta-item">
      <span class="meta-label">Labour Hours</span>
      <span class="meta-value">{{.LaborHours}}</span>
    </div>
    {{end}}
    {{if .ClientSentiment}}
    <div class="meta-item">
      <span class="meta-label">Client Sentiment</span>
      <span class="meta-value">{{.ClientSentiment}}</span>
    </div>
    {{end}}
    {{if .FollowUpRequired}}
    <div class="meta-item">
      <span class="meta-label">Follow-up Required</span>
      <span class="meta-value follow-up-flag">Yes</span>
    </div>
    {{end}}
  </div>
</div>

{{if .Summary}}
<div class="section">
  <h2>Summary</h2>
  <div class="section-content">{{.Summary}}</div>
</div>
{{end}}

{{if .WorkPerformed}}
<div class="section">
  <h2>Work Performed</h2>
  <div class="section-content">{{.WorkPerformed}}</div>
</div>
{{end}}

{{if .Materials}}
<div class="section">
  <h2>Materials Used</h2>
  <table>
    <thead>
      <tr><th>Material</th><th>Quantity</th><th>Unit</th></tr>
    </thead>
    <tbody>
    {{range .Materials}}
      <tr>
        <td>{{.Name}}</td>
        <td>{{.Quantity}}</td>
        <td>{{.Unit}}</td>
      </tr>
    {{end}}
    </tbody>
  </table>
</div>
{{end}}

{{if .Recommendations}}
<div class="section">
  <h2>Recommendations</h2>
  <table>
    <thead>
      <tr><th>Description</th><th>Urgency</th><th>Est. Cost</th></tr>
    </thead>
    <tbody>
    {{range .Recommendations}}
      <tr>
        <td>{{.Description}}</td>
        <td><span class="badge badge-{{.Urgency}}">{{.Urgency}}</span></td>
        <td>{{.EstimatedCostRange}}</td>
      </tr>
    {{end}}
    </tbody>
  </table>
</div>
{{end}}

{{if .FollowUpNotes}}
<div class="section">
  <h2>Follow-up Notes</h2>
  <div class="section-content">{{.FollowUpNotes}}</div>
</div>
{{end}}

{{if .WarrantyNotes}}
<div class="section">
  <h2>Warranty Notes</h2>
  <div class="section-content">{{.WarrantyNotes}}</div>
</div>
{{end}}

{{if .SafetyConcerns}}
<div class="section">
  <h2>Safety Concerns</h2>
  {{range .SafetyConcerns}}
  <div class="safety">{{.}}</div>
  {{end}}
</div>
{{end}}

{{if .Tags}}
<div class="section">
  <h2>Tags</h2>
  <div class="tags">
    {{range .Tags}}
    <span class="tag">{{.}}</span>
    {{end}}
  </div>
</div>
{{end}}

{{if .Photos}}
<div class="section">
  <h2>Photos</h2>
  <div class="photo-grid">
    {{range .Photos}}
    <img src="{{.}}" alt="Job photo" loading="eager">
    {{end}}
  </div>
</div>
{{end}}

<div class="footer">
  <span>{{.CompanyName}}</span>
  <span>Powered by Wrappr</span>
</div>

</body>
</html>`
