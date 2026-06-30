package ingestion

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/valpere/ragivka/pkg/storage"
)

// Connector fetches raw document bytes from a source (FR-8).
type Connector interface {
	Connect(ctx context.Context, source string) (io.ReadCloser, error)
}

// S3Connector downloads a document from S3-compatible storage by key.
type S3Connector struct {
	storage storage.StorageClient
	http    *http.Client
}

// NewS3Connector returns a Connector that downloads via pre-signed S3 URL.
func NewS3Connector(sc storage.StorageClient) *S3Connector {
	return &S3Connector{
		storage: sc,
		http:    &http.Client{Timeout: 120 * time.Second},
	}
}

// Connect generates a pre-signed URL for key and streams the response body.
// The caller must close the returned ReadCloser.
func (c *S3Connector) Connect(ctx context.Context, key string) (io.ReadCloser, error) {
	url, err := c.storage.PresignURL(ctx, key, 5*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("s3 connector presign %q: %w", key, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("s3 connector build request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("s3 connector download %q: %w", key, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("s3 connector: status %d for %q", resp.StatusCode, key)
	}
	return resp.Body, nil
}
