package s3store

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	domainMedia "github.com/akarso/shopanda/internal/domain/media"
)

// Compile-time check.
var _ domainMedia.Storage = (*Storage)(nil)

// maxTimeout is the per-operation context deadline.
const maxTimeout = 30 * time.Second

// Config holds the settings needed to build an S3 storage backend.
type Config struct {
	Endpoint  string // S3-compatible endpoint URL (e.g. "https://s3.amazonaws.com")
	Bucket    string
	Region    string
	AccessKey string
	SecretKey string
	BaseURL   string // Optional CDN / public URL prefix (e.g. "https://cdn.example.com")
}

// s3API is the subset of the S3 client we actually use—makes unit-testing easy.
type s3API interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

// Storage implements media.Storage for S3-compatible backends.
type Storage struct {
	client  s3API
	bucket  string
	baseURL string
}

// New creates an S3 Storage using the given config.
func New(cfg Config) (*Storage, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("s3store: empty bucket")
	}
	if cfg.Region == "" {
		return nil, fmt.Errorf("s3store: empty region")
	}

	opts := []func(*s3.Options){
		func(o *s3.Options) {
			o.Region = cfg.Region
			o.Credentials = credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")
		},
	}
	if cfg.Endpoint != "" {
		opts = append(opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true // Required for MinIO / non-AWS S3.
		})
	}

	client := s3.New(s3.Options{}, opts...)

	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		// Fall back to path-style S3 URL.
		ep := strings.TrimRight(cfg.Endpoint, "/")
		if ep == "" {
			ep = fmt.Sprintf("https://s3.%s.amazonaws.com", cfg.Region)
		}
		baseURL = ep + "/" + cfg.Bucket
	}

	return &Storage{client: client, bucket: cfg.Bucket, baseURL: baseURL}, nil
}

// newWithClient is a test-only constructor that accepts a custom s3API.
func newWithClient(client s3API, bucket, baseURL string) *Storage {
	return &Storage{client: client, bucket: bucket, baseURL: baseURL}
}

// Name returns "s3".
func (s *Storage) Name() string { return "s3" }

// Save uploads file to the S3 bucket at the given path.
func (s *Storage) Save(path string, file io.Reader) error {
	ctx, cancel := context.WithTimeout(context.Background(), maxTimeout)
	defer cancel()

	clean := cleanPath(path)
	contentType := mimeFromPath(clean)

	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:       aws.String(s.bucket),
		Key:          aws.String(clean),
		Body:         file,
		ContentType:  aws.String(contentType),
		CacheControl: aws.String("public, max-age=31536000"),
		ACL:          s3types.ObjectCannedACLPublicRead,
	})
	if err != nil {
		return fmt.Errorf("s3store: put %q: %w", clean, err)
	}
	return nil
}

// Delete removes the object at the given path from the bucket.
func (s *Storage) Delete(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), maxTimeout)
	defer cancel()

	clean := cleanPath(path)
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(clean),
	})
	if err != nil {
		return fmt.Errorf("s3store: delete %q: %w", clean, err)
	}
	return nil
}

// URL returns the public URL for the given storage-relative path.
func (s *Storage) URL(path string) string {
	return s.baseURL + "/" + cleanPath(path)
}

// cleanPath normalises a storage-relative path: strips leading slash, collapses ".." segments.
func cleanPath(p string) string {
	p = strings.TrimLeft(p, "/")
	// Remove any ".." segments to prevent path traversal in keys.
	parts := strings.Split(p, "/")
	clean := make([]string, 0, len(parts))
	for _, seg := range parts {
		if seg == ".." || seg == "." || seg == "" {
			continue
		}
		clean = append(clean, seg)
	}
	return strings.Join(clean, "/")
}

// mimeFromPath returns a Content-Type guess based on file extension.
func mimeFromPath(path string) string {
	idx := strings.LastIndex(path, ".")
	if idx == -1 {
		return "application/octet-stream"
	}
	switch strings.ToLower(path[idx:]) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".pdf":
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}
