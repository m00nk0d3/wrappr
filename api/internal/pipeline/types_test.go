package pipeline

import (
	"encoding/json"
	"testing"
)

func TestTaskTypeProcessJob(t *testing.T) {
	if TaskTypeProcessJob != "pipeline:process_job" {
		t.Errorf("unexpected task type: %q", TaskTypeProcessJob)
	}
}

func TestProcessJobPayload_JSON(t *testing.T) {
	p := ProcessJobPayload{JobID: "abc-123"}

	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got ProcessJobPayload
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.JobID != p.JobID {
		t.Errorf("round-trip: want JobID=%q, got %q", p.JobID, got.JobID)
	}
}
