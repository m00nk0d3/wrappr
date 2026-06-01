// Package pipeline holds types shared between the HTTP handler that enqueues
// Asynq tasks and the worker that processes them. Keeping them here avoids an
// import cycle between internal/jobs (HTTP layer) and internal/worker/tasks
// (background worker layer).
package pipeline

// TaskTypeProcessJob is the Asynq task type for the transcription pipeline.
const TaskTypeProcessJob = "pipeline:process_job"

// ProcessJobPayload is the JSON payload carried by a TaskTypeProcessJob task.
type ProcessJobPayload struct {
	JobID string `json:"job_id"`
}
