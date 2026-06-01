package r2

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// mockS3 is a test double for s3API that records calls and returns preset results.
type mockS3 struct {
	putErr    error
	getBody   io.ReadCloser
	getErr    error
	deleteErr error

	lastPutKey    string
	lastPutCT     string
	lastGetKey    string
	lastDeleteKey string
}

func (m *mockS3) PutObject(_ context.Context, params *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if params.Key != nil {
		m.lastPutKey = *params.Key
	}
	if params.ContentType != nil {
		m.lastPutCT = *params.ContentType
	}
	return &s3.PutObjectOutput{}, m.putErr
}

func (m *mockS3) GetObject(_ context.Context, params *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if params.Key != nil {
		m.lastGetKey = *params.Key
	}
	if m.getErr != nil {
		return nil, m.getErr
	}
	return &s3.GetObjectOutput{Body: m.getBody}, nil
}

func (m *mockS3) DeleteObject(_ context.Context, params *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if params.Key != nil {
		m.lastDeleteKey = *params.Key
	}
	return &s3.DeleteObjectOutput{}, m.deleteErr
}

func newTestClient(mock *mockS3) *Client {
	return &Client{s3: mock, bucket: "test-bucket"}
}

// ---- Upload ----

func TestUpload_Success(t *testing.T) {
	mock := &mockS3{}
	c := newTestClient(mock)

	err := c.Upload(context.Background(), "jobs/abc/audio.webm", strings.NewReader("data"), "audio/webm", 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.lastPutKey != "jobs/abc/audio.webm" {
		t.Errorf("PutObject key: want %q, got %q", "jobs/abc/audio.webm", mock.lastPutKey)
	}
	if mock.lastPutCT != "audio/webm" {
		t.Errorf("PutObject content-type: want %q, got %q", "audio/webm", mock.lastPutCT)
	}
}

func TestUpload_Error(t *testing.T) {
	mock := &mockS3{putErr: errors.New("S3 unavailable")}
	c := newTestClient(mock)

	err := c.Upload(context.Background(), "jobs/abc/audio.webm", strings.NewReader("data"), "audio/webm", 4)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---- Download ----

func TestDownload_Success(t *testing.T) {
	want := "audio bytes"
	mock := &mockS3{getBody: io.NopCloser(strings.NewReader(want))}
	c := newTestClient(mock)

	rc, err := c.Download(context.Background(), "jobs/abc/audio.webm")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer rc.Close()

	got, _ := io.ReadAll(rc)
	if string(got) != want {
		t.Errorf("body: want %q, got %q", want, string(got))
	}
	if mock.lastGetKey != "jobs/abc/audio.webm" {
		t.Errorf("GetObject key: want %q, got %q", "jobs/abc/audio.webm", mock.lastGetKey)
	}
}

func TestDownload_Error(t *testing.T) {
	mock := &mockS3{getErr: errors.New("key not found")}
	c := newTestClient(mock)

	_, err := c.Download(context.Background(), "missing/key")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---- Delete ----

func TestDelete_Success(t *testing.T) {
	mock := &mockS3{}
	c := newTestClient(mock)

	err := c.Delete(context.Background(), "jobs/abc/audio.webm")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.lastDeleteKey != "jobs/abc/audio.webm" {
		t.Errorf("DeleteObject key: want %q, got %q", "jobs/abc/audio.webm", mock.lastDeleteKey)
	}
}

func TestDelete_Error(t *testing.T) {
	mock := &mockS3{deleteErr: errors.New("permission denied")}
	c := newTestClient(mock)

	err := c.Delete(context.Background(), "jobs/abc/audio.webm")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
