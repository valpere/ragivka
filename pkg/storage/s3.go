package storage

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type s3Client struct {
	client *s3.Client
	presig *s3.PresignClient
	bucket string
}

// NewS3Client constructs a StorageClient backed by AWS S3 or a MinIO-compatible endpoint (FR-20).
// Set cfg.Endpoint and cfg.UsePathStyle=true for local MinIO dev.
func NewS3Client(cfg S3Config) StorageClient {
	awsCfg := aws.Config{
		Region: cfg.Region,
		Credentials: credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID,
			cfg.SecretAccessKey,
			"",
		),
	}

	opts := []func(*s3.Options){
		func(o *s3.Options) {
			o.UsePathStyle = cfg.UsePathStyle
		},
	}
	if cfg.Endpoint != "" {
		opts = append(opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		})
	}

	cl := s3.NewFromConfig(awsCfg, opts...)
	return &s3Client{
		client: cl,
		presig: s3.NewPresignClient(cl),
		bucket: cfg.Bucket,
	}
}

func (c *s3Client) PutObject(ctx context.Context, key string, body io.Reader, size int64) error {
	_, err := c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentLength: aws.Int64(size),
	})
	if err != nil {
		return fmt.Errorf("s3 put %q: %w", key, err)
	}
	return nil
}

func (c *s3Client) PresignURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	req, err := c.presig.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("s3 presign %q: %w", key, err)
	}
	return req.URL, nil
}

func (c *s3Client) DeleteObject(ctx context.Context, key string) error {
	_, err := c.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("s3 delete %q: %w", key, err)
	}
	return nil
}
