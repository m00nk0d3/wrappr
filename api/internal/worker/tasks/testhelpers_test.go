package tasks

import (
	"sync"
	"testing"

	"github.com/hibiken/asynq"
)

// noopEnqueuer is a taskEnqueuer that silently discards all enqueued tasks.
// Use it in tests that exercise code paths which call Enqueue but don't need
// real Redis connectivity.
type noopEnqueuer struct{}

func (noopEnqueuer) Enqueue(_ *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	return nil, nil
}

// recordingEnqueuer is a taskEnqueuer that records every Enqueue call so tests
// can assert which task types were enqueued and how many times.
type recordingEnqueuer struct {
	mu    sync.Mutex
	calls []string // task type for each Enqueue call
}

func (r *recordingEnqueuer) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, task.Type())
	return &asynq.TaskInfo{}, nil
}

// enqueuedTypes returns a snapshot of task types in the order they were enqueued.
func (r *recordingEnqueuer) enqueuedTypes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.calls))
	copy(out, r.calls)
	return out
}

// ── Constructor wiring tests ────────────────────────────────────────────────

// TestNewProcessJobHandler_StoresEnqueuer verifies that the enqueuer passed to
// the constructor is stored and available for downstream task chaining.
func TestNewProcessJobHandler_StoresEnqueuer(t *testing.T) {
	eq := noopEnqueuer{}
	h := NewProcessJobHandler(nil, nil, "key", "", eq)
	if h.enqueuer == nil {
		t.Fatal("enqueuer should be stored on the handler, got nil")
	}
}

// TestNewProcessJobHandler_NilEnqueuerAllowed verifies that nil is a valid value
// for the enqueuer (used when PDF generation is disabled or in minimal test setups).
func TestNewProcessJobHandler_NilEnqueuerAllowed(t *testing.T) {
	h := NewProcessJobHandler(nil, nil, "key", "", nil)
	if h.enqueuer != nil {
		t.Errorf("expected nil enqueuer, got %v", h.enqueuer)
	}
}
