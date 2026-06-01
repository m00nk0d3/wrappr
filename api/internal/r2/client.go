// Package r2 provides a Cloudflare R2 storage client backed by the AWS S3-compatible API.
// R2 endpoints follow the pattern https://<account_id>.r2.cloudflarestorage.com.
package r2

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Client wraps the S3 client configured for Cloudflare R2.
type Client struct {
	s3     *s3.Client
	bucket string
}

// New creates a new R2 Client using static credentials.
// accountID is the Cloudflare account ID used to build the endpoint URL.
func New(accountID, accessKeyID, secretAccessKey, bucket string) (*Client, error) {
	endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", accountID)

	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		// R2 accepts "auto" as the region for the S3 API.
		awsconfig.WithRegion("auto"),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, ""),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("r2: load aws config: %w", err)
	}

	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		// Point all requests at the R2 account endpoint.
		o.BaseEndpoint = aws.String(endpoint)
		// Use path-style so the bucket name stays in the URL path rather than the hostname.
		o.UsePathStyle = true
	})

	return &Client{s3: s3Client, bucket: bucket}, nil
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
