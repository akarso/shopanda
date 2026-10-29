package s3store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	domainMedia "github.com/akarso/shopanda/internal/domain/media"
)

// Compile-time checks.
var (
	_ domainMedia.Storage          = (*Storage)(nil)
	_ domainMedia.SignedURLStorage = (*Storage)(nil)
)

// maxTimeout is the per-operation context deadline.
const maxTimeout = 30 * time.Second

// DefaultSignedURLTTL is used when SignedURL is called with a non-positive ttl.
const DefaultSignedURLTTL = 60 * time.Second

const (
	publicCacheControl  = "public, max-age=31536000"
	privateCacheControl = "private, no-store"
)

// Config holds the settings needed to build an S3 storage backend.
type Config struct {
	Endpoint  string // S3-compatible endpoint URL (e.g. "https://s3.amazonaws.com")
	Bucket    string
	Region    string
	AccessKey string
	SecretKey string
	BaseURL   string // Optional CDN / public URL prefix (e.g. "https://cdn.example.com")
	PublicACL bool   // Set ACL=public-read on PutObject (only for legacy ACL buckets)
}

// s3API is the subset of the S3 client we actually use—makes unit-testing easy.
type s3API interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
}

// PresignClient is the subset of *s3.PresignClient used for grant delivery.
// Tests inject a fake to assert TTL / cache-control / disposition without AWS.
type PresignClient interface {
	PresignGetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

// Storage implements media.Storage for S3-compatible backends.
type Storage struct {
	client       s3API
	presigner    PresignClient // nil for test doubles that only stub Put/Delete/Head
	bucket       string
	region       string
	baseURL      string
	endpointHost string // custom endpoint hostname (MinIO); empty for production AWS
	publicACL    bool
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
		},
	}
	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		opts = append(opts, func(o *s3.Options) {
			o.Credentials = credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")
		})
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
			ep = fmt.Sprintf("https://s3.%s.%s", cfg.Region, awsDNSSuffix(cfg.Region))
		}
		baseURL = ep + "/" + cfg.Bucket
	}

	endpointHost := ""
	if ep := strings.TrimRight(cfg.Endpoint, "/"); ep != "" {
		if u, err := parseURLHost(ep); err == nil {
			endpointHost = u
		}
	}
	// Do not derive endpointHost from baseURL / CDN — SignedURLHosts only
	// lists hosts the SDK actually signs against (custom endpoint or AWS).

	return &Storage{
		client:       client,
		presigner:    s3.NewPresignClient(client),
		bucket:       cfg.Bucket,
		region:       cfg.Region,
		baseURL:      baseURL,
		endpointHost: endpointHost,
		publicACL:    cfg.PublicACL,
	}, nil
}

// newWithClient is a test-only constructor that accepts a custom s3API.
// SignedURL is unavailable without a PresignClient (presigner stays nil).
// region is empty; set st.region (and optionally st.endpointHost) when
// testing SignedURLHosts / host allowlist checks.
func newWithClient(client s3API, bucket, baseURL string) *Storage {
	baseURL = strings.TrimRight(baseURL, "/")
	return &Storage{client: client, bucket: bucket, baseURL: baseURL}
}

func parseURLHost(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty url")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	return u.Hostname(), nil
}

// SignedURLHosts returns hosts that SignedURL / PresignGetObject may produce.
// Production AWS (empty Endpoint): path-style s3.{region}.{dnsSuffix} and
// virtual-hosted {bucket}.s3.{region}.{dnsSuffix} (SDK v2 default). dnsSuffix
// is amazonaws.com, or amazonaws.com.cn for China regions (cn-*).
// Custom Endpoint (MinIO): that endpoint hostname only (path-style).
// CDN / BaseURL is not included unless it is also the custom endpoint — we
// do not presign against CDN hosts.
func (s *Storage) SignedURLHosts() []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(h string) {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" {
			return
		}
		if _, ok := seen[h]; ok {
			return
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	if s.endpointHost != "" {
		add(s.endpointHost)
		return out
	}
	region := strings.TrimSpace(s.region)
	if region != "" {
		suffix := awsDNSSuffix(region)
		add(fmt.Sprintf("s3.%s.%s", region, suffix))
		if bucket := strings.TrimSpace(s.bucket); bucket != "" {
			add(fmt.Sprintf("%s.s3.%s.%s", bucket, region, suffix))
		}
	}
	return out
}

// awsDNSSuffix returns the AWS DNS suffix for regional S3 hosts.
// China partition regions (cn-*) use amazonaws.com.cn; commercial AWS uses
// amazonaws.com. Keeps SignedURLHosts aligned with the SDK's resolved endpoint.
func awsDNSSuffix(region string) string {
	r := strings.ToLower(strings.TrimSpace(region))
	if strings.HasPrefix(r, "cn-") {
		return "amazonaws.com.cn"
	}
	return "amazonaws.com"
}

// Name returns "s3".
func (s *Storage) Name() string { return "s3" }

// Save uploads file to the S3 bucket at the given path.
// Paths under media.PrivateDownloadsPrefix get private, no-store Cache-Control
// and never receive a public ACL (even when PublicACL is configured).
func (s *Storage) Save(path string, file io.Reader) error {
	key, err := validateKey(path)
	if err != nil {
		return fmt.Errorf("s3store: invalid key %q: %w", path, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), maxTimeout)
	defer cancel()

	contentType := mimeFromPath(key)
	private := domainMedia.IsPrivateDownloadsPath(key)

	input := &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        file,
		ContentType: aws.String(contentType),
	}
	if private {
		input.CacheControl = aws.String(privateCacheControl)
	} else {
		input.CacheControl = aws.String(publicCacheControl)
		if s.publicACL {
			input.ACL = s3types.ObjectCannedACLPublicRead
		}
	}

	if _, err := s.client.PutObject(ctx, input); err != nil {
		return fmt.Errorf("s3store: put %q: %w", key, err)
	}
	return nil
}

// Delete removes the object at the given path from the bucket.
func (s *Storage) Delete(path string) error {
	key, err := validateKey(path)
	if err != nil {
		return fmt.Errorf("s3store: invalid key %q: %w", path, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), maxTimeout)
	defer cancel()

	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}); err != nil {
		return fmt.Errorf("s3store: delete %q: %w", key, err)
	}
	return nil
}

// URL returns the public URL for the given storage-relative path.
// The path is expected to be a validated key (as returned by Save).
// Do not use for downloadable grant delivery — prefer SignedURL.
func (s *Storage) URL(path string) string {
	return s.baseURL + "/" + strings.TrimLeft(path, "/")
}

// Head verifies the object exists (PR-1077: prove blob before claiming a download).
func (s *Storage) Head(ctx context.Context, path string) error {
	key, err := validateKey(path)
	if err != nil {
		return fmt.Errorf("s3store: invalid key %q: %w", path, err)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, maxTimeout)
	defer cancel()

	_, err = s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isS3NotFound(err) {
			return fmt.Errorf("s3store: head %q: %w", key, os.ErrNotExist)
		}
		return fmt.Errorf("s3store: head %q: %w", key, err)
	}
	return nil
}

// SignedURL returns a short-lived pre-signed GET URL for path (PR-1077).
// Overrides response Cache-Control to private, no-store and sets
// Content-Disposition attachment so intermediaries do not cache grant bytes.
func (s *Storage) SignedURL(ctx context.Context, path string, opts domainMedia.SignURLOptions) (string, error) {
	if s.presigner == nil {
		return "", fmt.Errorf("s3store: signed URLs not available")
	}
	key, err := validateKey(path)
	if err != nil {
		return "", fmt.Errorf("s3store: invalid key %q: %w", path, err)
	}
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = DefaultSignedURLTTL
	}
	input := &s3.GetObjectInput{
		Bucket:                     aws.String(s.bucket),
		Key:                        aws.String(key),
		ResponseCacheControl:       aws.String(privateCacheControl),
		ResponseContentDisposition: aws.String(domainMedia.ContentDispositionAttachment(opts.Filename)),
	}
	if ctx == nil {
		ctx = context.Background()
	}
	out, err := s.presigner.PresignGetObject(ctx, input, func(o *s3.PresignOptions) {
		o.Expires = ttl
	})
	if err != nil {
		return "", fmt.Errorf("s3store: presign get %q: %w", key, err)
	}
	if out == nil || strings.TrimSpace(out.URL) == "" {
		return "", fmt.Errorf("s3store: presign get %q: empty URL", key)
	}
	if err := ensureSignedURLHostAllowed(out.URL, s.SignedURLHosts()); err != nil {
		return "", fmt.Errorf("s3store: presign get %q: %w", key, err)
	}
	return out.URL, nil
}

func ensureSignedURLHostAllowed(raw string, allowed []string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("parse signed URL: %w", err)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return fmt.Errorf("signed URL missing host")
	}
	for _, a := range allowed {
		if host == strings.ToLower(a) {
			return nil
		}
	}
	return fmt.Errorf("signed URL host %q not allowed", host)
}

func isS3NotFound(err error) bool {
	if err == nil {
		return false
	}
	var nsk *s3types.NotFound
	if errors.As(err, &nsk) {
		return true
	}
	var nfe *s3types.NoSuchKey
	if errors.As(err, &nfe) {
		return true
	}
	// Some S3-compatible servers return HTTP 404 without typed NotFound/NoSuchKey.
	// Match only typed status accessors — never substring "not found".
	var withStatus interface{ HTTPStatusCode() int }
	if errors.As(err, &withStatus) && withStatus.HTTPStatusCode() == 404 {
		return true
	}
	return false
}

// validateKey checks that a storage key is safe and returns the normalised key.
// It rejects paths containing "..", ".", empty segments, or that resolve to an
// empty string.
func validateKey(p string) (string, error) {
	p = strings.TrimLeft(p, "/")
	if p == "" {
		return "", fmt.Errorf("empty key")
	}
	parts := strings.Split(p, "/")
	for _, seg := range parts {
		if seg == "" {
			return "", fmt.Errorf("empty segment")
		}
		if seg == "." || seg == ".." {
			return "", fmt.Errorf("traversal segment %q", seg)
		}
	}
	return p, nil
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
