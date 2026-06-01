// Package worker provides the Asynq worker server setup for the wrappr pipeline.
package worker

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/m00nk0d3/wrappr/api/internal/config"
	"github.com/m00nk0d3/wrappr/api/internal/db"
	"github.com/m00nk0d3/wrappr/api/internal/pipeline"
	"github.com/m00nk0d3/wrappr/api/internal/r2"
	"github.com/m00nk0d3/wrappr/api/internal/worker/tasks"
)

const workerConcurrency = 5

// Start builds the Asynq server from cfg, registers all task handlers, and
// blocks until ctx is cancelled. It returns only after the server has shut down.
func Start(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool) error {
	if cfg.GroqAPIKey == "" {
		return fmt.Errorf("worker: GROQ_API_KEY is required but not set")
	}

	redisOpt, err := ParseRedisURL(cfg.RedisURL)
	if err != nil {
		return fmt.Errorf("worker: %w", err)
	}

	r2Client, err := r2.New(cfg.R2AccountID, cfg.R2AccessKeyID, cfg.R2SecretAccessKey, cfg.R2Bucket)
	if err != nil {
		return fmt.Errorf("worker: create R2 client: %w", err)
	}

	srv := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency:    workerConcurrency,
		RetryDelayFunc: retryDelay,
		ErrorHandler:   asynq.ErrorHandlerFunc(makeErrorHandler(pool)),
	})

	mux := asynq.NewServeMux()
	mux.Handle(pipeline.TaskTypeProcessJob, tasks.NewProcessJobHandler(pool, r2Client, cfg.GroqAPIKey))

	// Run the server in a goroutine and wait for context cancellation.
	errCh := make(chan error, 1)
	go func() {
		if err := srv.Run(mux); err != nil {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Println("worker: shutdown signal received")
		srv.Shutdown()
		return nil
	case err := <-errCh:
		return fmt.Errorf("worker: server error: %w", err)
	}
}

// retryDelay returns the backoff duration for a given retry attempt.
// n is 1-indexed (n=1 → first retry, n=2 → second, ...).
func retryDelay(n int, _ error, _ *asynq.Task) time.Duration {
	switch n {
	case 1:
		return 30 * time.Second
	case 2:
		return 2 * time.Minute
	default:
		return 10 * time.Minute
	}
}

// makeErrorHandler returns an asynq.ErrorHandlerFunc that marks a job as
// "failed" in the database when its task has exhausted all retries.
func makeErrorHandler(pool *pgxpool.Pool) func(ctx context.Context, task *asynq.Task, err error) {
	return func(ctx context.Context, task *asynq.Task, err error) {
		retried, retriedOK := asynq.GetRetryCount(ctx)
		maxRetry, maxRetryOK := asynq.GetMaxRetry(ctx)
		if !retriedOK || !maxRetryOK || retried < maxRetry {
			// Still has retries remaining — nothing to do here.
			return
		}

		log.Printf("worker: task %s final failure (retried=%d): %v", task.Type(), retried, err)

		// Best-effort status update — log on failure, don't block.
		if markErr := markJobFailed(pool, task.Payload()); markErr != nil {
			log.Printf("worker: mark job failed: %v", markErr)
		}
	}
}

// markJobFailed parses the job ID from rawPayload and sets pipeline_status="failed".
// context.Background() is used deliberately: the worker's ctx may already be
// cancelled during shutdown, and we still want this best-effort DB update to land.
func markJobFailed(pool *pgxpool.Pool, rawPayload []byte) error {
	var payload pipeline.ProcessJobPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}

	var jobUUID pgtype.UUID
	if err := jobUUID.Scan(payload.JobID); err != nil {
		return fmt.Errorf("parse job UUID %q: %w", payload.JobID, err)
	}

	q := db.New(pool)
	if _, err := q.UpdateJobStatus(context.Background(), db.UpdateJobStatusParams{
		ID:             jobUUID,
		PipelineStatus: "failed",
	}); err != nil {
		return fmt.Errorf("update job status: %w", err)
	}

	return nil
}

// ParseRedisURL converts a redis:// or rediss:// URL string into an asynq.RedisClientOpt.
// rediss:// enables TLS — required for managed Redis providers (Upstash, Render, etc.).
// It is exported so the HTTP server can reuse the same parsing logic when
// constructing an asynq.Client for enqueueing tasks.
func ParseRedisURL(rawURL string) (asynq.RedisClientOpt, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return asynq.RedisClientOpt{}, fmt.Errorf("parse redis URL %q: %w", rawURL, err)
	}

	host := u.Host
	if host == "" {
		return asynq.RedisClientOpt{}, fmt.Errorf("redis URL %q missing host", rawURL)
	}

	opt := asynq.RedisClientOpt{Addr: host}
	if u.User != nil {
		if pw, ok := u.User.Password(); ok {
			opt.Password = pw
		}
	}

	// Parse optional Redis database number from the URL path (e.g. /1).
	if db := strings.TrimPrefix(u.Path, "/"); db != "" {
		n, err := strconv.Atoi(db)
		if err != nil {
			return asynq.RedisClientOpt{}, fmt.Errorf("redis URL %q invalid database number: %w", rawURL, err)
		}
		opt.DB = n
	}

	if u.Scheme == "rediss" {
		opt.TLSConfig = &tls.Config{}
	}

	return opt, nil
}
