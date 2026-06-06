// Package r2 provides a Cloudflare R2 storage client backed by the AWS S3-compatible API.
// R2 endpoints follow the pattern https://<account_id>.r2.cloudflarestorage.com.
package r2

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// s3API is the subset of the S3 client API used by Client.
// Extracted as an interface so tests can inject a mock without a live S3 endpoint.
type s3API interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

// Client wraps the S3 client configured for Cloudflare R2.
type Client struct {
	s3      s3API
	presign *s3.PresignClient
	bucket  string
}

// New creates a new R2 Client using static credentials.
// accountID is the Cloudflare account ID used to build the endpoint URL.
//
// Unlike awsconfig.LoadDefaultConfig, this constructs the aws.Config directly
// so it never probes EC2 IMDS or ~/.aws files, avoiding a ~2 s startup delay
// in containerised environments that have no instance metadata service.
func New(accountID, accessKeyID, secretAccessKey, bucket string) (*Client, error) {
	endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", accountID)

	// R2 accepts "auto" as the region for the S3-compatible API.
	cfg := aws.Config{
		Region:      "auto",
		Credentials: credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, ""),
	}

	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		// Point all requests at the R2 account endpoint.
		o.BaseEndpoint = aws.String(endpoint)
		// Use path-style so the bucket name stays in the URL path rather than the hostname.
		o.UsePathStyle = true
	})

	return &Client{s3: s3Client, presign: s3.NewPresignClient(s3Client), bucket: bucket}, nil
}

// Upload streams body to R2 at key with contentType. size is the byte count of body
// and is required so the SDK can set Content-Length without buffering.
func (c *Client) Upload(ctx context.Context, key string, body io.Reader, contentType string, size int64) error {
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(size),
	})
	if err != nil {
		return fmt.Errorf("r2: upload %q: %w", key, err)
	}
	return nil
}

// Download retrieves an object from R2 by key and returns its body as a ReadCloser.
// The caller is responsible for closing the returned body.
func (c *Client) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("r2: download %q: %w", key, err)
	}
	return out.Body, nil
}

// Delete removes an object from R2 by key. It is a no-op if the key does not exist.
func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("r2: delete %q: %w", key, err)
	}
	return nil
}

// PresignGetURL returns a pre-signed GET URL for the object at key that is valid
// for the given expiry duration. This allows external systems (e.g. Gotenberg)
// to fetch private R2 objects without requiring long-lived credentials.
func (c *Client) PresignGetURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	req, err := c.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("r2: presign %q: %w", key, err)
	}
	return req.URL, nil
}
