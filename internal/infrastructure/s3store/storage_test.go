package s3store

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/akarso/shopanda/internal/domain/media"
)

// Compile-time interface check.
var _ media.Storage = (*Storage)(nil)
var _ media.SignedURLStorage = (*Storage)(nil)

// --- mock S3 client ---

type mockS3 struct {
	putErr    error
	deleteErr error
	headErr   error
	putKeys   []string
	putCT     []string // Content-Type per call
	putCC     []string // Cache-Control per call
	putACL    []string // ACL per call (empty string when omitted)
	deleted   []string
	headed    []string
}

func (m *mockS3) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if m.putErr != nil {
		return nil, m.putErr
	}
	key := ""
	if in.Key != nil {
		key = *in.Key
	}
	m.putKeys = append(m.putKeys, key)
	ct := ""
	if in.ContentType != nil {
		ct = *in.ContentType
	}
	m.putCT = append(m.putCT, ct)
	cc := ""
	if in.CacheControl != nil {
		cc = *in.CacheControl
	}
	m.putCC = append(m.putCC, cc)
	m.putACL = append(m.putACL, string(in.ACL))
	// Drain body.
	if in.Body != nil {
		io.Copy(io.Discard, in.Body)
	}
	return &s3.PutObjectOutput{}, nil
}

func (m *mockS3) DeleteObject(_ context.Context, in *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if m.deleteErr != nil {
		return nil, m.deleteErr
	}
	key := ""
	if in.Key != nil {
		key = *in.Key
	}
	m.deleted = append(m.deleted, key)
	return &s3.DeleteObjectOutput{}, nil
}

func (m *mockS3) HeadObject(_ context.Context, in *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	key := ""
	if in.Key != nil {
		key = *in.Key
	}
	m.headed = append(m.headed, key)
	if m.headErr != nil {
		return nil, m.headErr
	}
	return &s3.HeadObjectOutput{}, nil
}

type mockPresigner struct {
	url       string
	err       error
	lastTTL   time.Duration
	lastCC    string
	lastDisp  string
	lastKey   string
	callCount int
}

func (m *mockPresigner) PresignGetObject(_ context.Context, params *s3.GetObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
	m.callCount++
	if params.Key != nil {
		m.lastKey = *params.Key
	}
	if params.ResponseCacheControl != nil {
		m.lastCC = *params.ResponseCacheControl
	}
	if params.ResponseContentDisposition != nil {
		m.lastDisp = *params.ResponseContentDisposition
	}
	opts := &s3.PresignOptions{}
	for _, fn := range optFns {
		fn(opts)
	}
	m.lastTTL = opts.Expires
	if m.err != nil {
		return nil, m.err
	}
	return &v4.PresignedHTTPRequest{URL: m.url}, nil
}

// --- tests ---

func TestName(t *testing.T) {
	st := newWithClient(&mockS3{}, "bucket", "https://cdn.example.com")
	if st.Name() != "s3" {
		t.Errorf("Name() = %q, want %q", st.Name(), "s3")
	}
}

func TestSignedURL_UnavailableWithoutPresigner(t *testing.T) {
	st := newWithClient(&mockS3{}, "bucket", "https://cdn.example.com")
	_, err := st.SignedURL(context.Background(), "files/a.pdf", media.SignURLOptions{TTL: time.Minute})
	if err == nil || !strings.Contains(err.Error(), "signed URLs not available") {
		t.Fatalf("SignedURL = %v, want unavailable error", err)
	}
}

func TestSignedURL_SetsPrivateCacheControlAndDisposition(t *testing.T) {
	mock := &mockS3{}
	presign := &mockPresigner{url: "https://my-bucket.s3.eu-west-1.amazonaws.com/a.pdf?X-Amz-Signature=1"}
	st := newWithClient(mock, "my-bucket", "https://s3.eu-west-1.amazonaws.com/my-bucket")
	st.region = "eu-west-1"
	st.presigner = presign

	got, err := st.SignedURL(context.Background(), "private/downloads/id/guide.pdf", media.SignURLOptions{
		TTL:      45 * time.Second,
		Filename: "evil\"\r\nname.pdf",
	})
	if err != nil {
		t.Fatalf("SignedURL: %v", err)
	}
	if got != presign.url {
		t.Fatalf("URL = %q", got)
	}
	if presign.lastTTL != 45*time.Second {
		t.Fatalf("TTL = %v", presign.lastTTL)
	}
	if presign.lastCC != "private, no-store" {
		t.Fatalf("ResponseCacheControl = %q", presign.lastCC)
	}
	if presign.lastDisp != `attachment; filename="evilname.pdf"` {
		t.Fatalf("ResponseContentDisposition = %q", presign.lastDisp)
	}
	if presign.lastKey != "private/downloads/id/guide.pdf" {
		t.Fatalf("key = %q", presign.lastKey)
	}
}

func TestSignedURL_NonASCIIFilenameUsesRFC5987(t *testing.T) {
	mock := &mockS3{}
	presign := &mockPresigner{url: "https://my-bucket.s3.eu-west-1.amazonaws.com/a.pdf?X-Amz-Signature=1"}
	st := newWithClient(mock, "my-bucket", "https://s3.eu-west-1.amazonaws.com/my-bucket")
	st.region = "eu-west-1"
	st.presigner = presign

	_, err := st.SignedURL(context.Background(), "private/downloads/id/guide.pdf", media.SignURLOptions{
		TTL:      time.Minute,
		Filename: "café.pdf",
	})
	if err != nil {
		t.Fatalf("SignedURL: %v", err)
	}
	want := media.ContentDispositionAttachment("café.pdf")
	if !strings.Contains(want, "filename*=UTF-8''") {
		t.Fatalf("helper missing filename*: %q", want)
	}
	if presign.lastDisp != want {
		t.Fatalf("ResponseContentDisposition = %q, want %q", presign.lastDisp, want)
	}
}

func TestSignedURL_RejectsForeignHost(t *testing.T) {
	st := newWithClient(&mockS3{}, "my-bucket", "https://s3.eu-west-1.amazonaws.com/my-bucket")
	st.region = "eu-west-1"
	st.presigner = &mockPresigner{url: "https://evil.example/steal.pdf"}
	_, err := st.SignedURL(context.Background(), "private/downloads/id/guide.pdf", media.SignURLOptions{TTL: time.Minute})
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("SignedURL = %v, want host rejection", err)
	}
}

func TestSignedURL_RejectsCDNHostWhenNotPresignTarget(t *testing.T) {
	// CDN BaseURL is for public URL(); SignedURLHosts must not allow it alone.
	st, err := New(Config{
		Bucket:  "my-bucket",
		Region:  "eu-west-1",
		BaseURL: "https://cdn.example.com",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	st.presigner = &mockPresigner{url: "https://cdn.example.com/a.pdf?X-Amz-Signature=1"}
	_, err = st.SignedURL(context.Background(), "private/downloads/id/guide.pdf", media.SignURLOptions{TTL: time.Minute})
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("SignedURL = %v, want CDN host rejection", err)
	}
}

func TestNew_SignedURLHosts_AWSVirtualHosted(t *testing.T) {
	st, err := New(Config{
		Bucket: "my-bucket",
		Region: "eu-west-1",
		// Empty Endpoint + empty BaseURL → path-style public baseURL fallback,
		// but allowlist must still include virtual-hosted (SDK v2 presign default).
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	hosts := st.SignedURLHosts()
	want := map[string]bool{
		"s3.eu-west-1.amazonaws.com":            true,
		"my-bucket.s3.eu-west-1.amazonaws.com": true,
	}
	if len(hosts) != 2 {
		t.Fatalf("SignedURLHosts = %v, want 2 hosts", hosts)
	}
	for _, h := range hosts {
		if !want[h] {
			t.Fatalf("unexpected host %q in %v", h, hosts)
		}
		delete(want, h)
	}
	for h := range want {
		t.Fatalf("missing host %q in %v", h, hosts)
	}
	// CDN must not appear; baseURL host is path-style regional (already listed).
	for _, h := range hosts {
		if strings.Contains(h, "cdn") {
			t.Fatalf("CDN must not be in allowlist: %v", hosts)
		}
	}
}

func TestNew_SignedURLHosts_ChinaRegion(t *testing.T) {
	st, err := New(Config{
		Bucket: "my-bucket",
		Region: "cn-north-1",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	hosts := st.SignedURLHosts()
	want := map[string]bool{
		"s3.cn-north-1.amazonaws.com.cn":            true,
		"my-bucket.s3.cn-north-1.amazonaws.com.cn": true,
	}
	if len(hosts) != 2 {
		t.Fatalf("SignedURLHosts = %v, want 2 hosts", hosts)
	}
	for _, h := range hosts {
		if !want[h] {
			t.Fatalf("unexpected host %q in %v", h, hosts)
		}
		delete(want, h)
	}
	for h := range want {
		t.Fatalf("missing host %q in %v", h, hosts)
	}
	if !strings.HasSuffix(st.baseURL, "amazonaws.com.cn/my-bucket") {
		t.Fatalf("baseURL = %q, want china DNS suffix", st.baseURL)
	}
}

func TestNew_SignedURLHosts_CustomEndpoint(t *testing.T) {
	st, err := New(Config{
		Endpoint: "https://minio.local:9000",
		Bucket:   "media",
		Region:   "us-east-1",
		BaseURL:  "https://cdn.example.com", // must not become the only/required host
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	hosts := st.SignedURLHosts()
	if len(hosts) != 1 || hosts[0] != "minio.local" {
		t.Fatalf("SignedURLHosts = %v, want [minio.local]", hosts)
	}
}

type statusCodeErr struct {
	code int
	msg  string
}

func (e statusCodeErr) Error() string         { return e.msg }
func (e statusCodeErr) HTTPStatusCode() int   { return e.code }

func TestIsS3NotFound(t *testing.T) {
	if !isS3NotFound(&s3types.NotFound{Message: aws.String("missing")}) {
		t.Fatal("NotFound typed error")
	}
	if !isS3NotFound(&s3types.NoSuchKey{Message: aws.String("missing")}) {
		t.Fatal("NoSuchKey typed error")
	}
	if !isS3NotFound(statusCodeErr{code: 404, msg: "http response error StatusCode: 404"}) {
		t.Fatal("HTTPStatusCode 404")
	}
	if isS3NotFound(errors.New("object not found in cache")) {
		t.Fatal("must not match substring not found")
	}
	if isS3NotFound(statusCodeErr{code: 403, msg: "forbidden"}) {
		t.Fatal("must not treat 403 as not found")
	}
}

func TestSignedURL_PresignError(t *testing.T) {
	st := newWithClient(&mockS3{}, "bucket", "https://cdn.example.com")
	st.presigner = &mockPresigner{err: errors.New("kms denied")}
	_, err := st.SignedURL(context.Background(), "files/a.pdf", media.SignURLOptions{TTL: time.Minute})
	if err == nil || !strings.Contains(err.Error(), "kms denied") {
		t.Fatalf("SignedURL = %v", err)
	}
}

func TestHead_OK(t *testing.T) {
	mock := &mockS3{}
	st := newWithClient(mock, "bucket", "https://cdn.example.com")
	if err := st.Head(context.Background(), "files/a.pdf"); err != nil {
		t.Fatalf("Head: %v", err)
	}
	if len(mock.headed) != 1 || mock.headed[0] != "files/a.pdf" {
		t.Fatalf("headed = %v", mock.headed)
	}
}

func TestHead_NotFound(t *testing.T) {
	mock := &mockS3{headErr: &s3types.NotFound{Message: aws.String("missing")}}
	st := newWithClient(mock, "bucket", "https://cdn.example.com")
	err := st.Head(context.Background(), "files/missing.pdf")
	if err == nil || !media.IsNotFound(err) {
		t.Fatalf("Head = %v, want os.ErrNotExist wrap", err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Head = %v, want errors.Is ErrNotExist", err)
	}
}

func TestSave(t *testing.T) {
	mock := &mockS3{}
	st := newWithClient(mock, "bucket", "https://cdn.example.com")

	err := st.Save("uploads/abc/photo.jpg", strings.NewReader("data"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.putKeys) != 1 || mock.putKeys[0] != "uploads/abc/photo.jpg" {
		t.Errorf("putKeys = %v, want [uploads/abc/photo.jpg]", mock.putKeys)
	}
	if mock.putCT[0] != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", mock.putCT[0])
	}
	if mock.putCC[0] != "public, max-age=31536000" {
		t.Errorf("Cache-Control = %q, want public, max-age=31536000", mock.putCC[0])
	}
}

func TestSave_PrivateDownloadsPrefix(t *testing.T) {
	mock := &mockS3{}
	st := newWithClient(mock, "bucket", "https://cdn.example.com")
	st.publicACL = true

	err := st.Save("private/downloads/aid/guide.pdf", strings.NewReader("data"))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if mock.putCC[0] != "private, no-store" {
		t.Fatalf("Cache-Control = %q, want private, no-store", mock.putCC[0])
	}
	if mock.putACL[0] != "" {
		t.Fatalf("ACL = %q, want empty even when publicACL configured", mock.putACL[0])
	}
}

func TestSave_Error(t *testing.T) {
	mock := &mockS3{putErr: errors.New("access denied")}
	st := newWithClient(mock, "bucket", "https://cdn.example.com")

	err := st.Save("uploads/test.png", strings.NewReader("data"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "access denied") {
		t.Errorf("error = %q, want mention of 'access denied'", err.Error())
	}
}

func TestDelete(t *testing.T) {
	mock := &mockS3{}
	st := newWithClient(mock, "bucket", "https://cdn.example.com")

	err := st.Delete("uploads/abc/photo.jpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.deleted) != 1 || mock.deleted[0] != "uploads/abc/photo.jpg" {
		t.Errorf("deleted = %v, want [uploads/abc/photo.jpg]", mock.deleted)
	}
}

func TestDelete_Error(t *testing.T) {
	mock := &mockS3{deleteErr: errors.New("not found")}
	st := newWithClient(mock, "bucket", "https://cdn.example.com")

	err := st.Delete("uploads/test.png")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want mention of 'not found'", err.Error())
	}
}

func TestURL(t *testing.T) {
	st := newWithClient(&mockS3{}, "bucket", "https://cdn.example.com")

	url := st.URL("uploads/abc/photo.jpg")
	if url != "https://cdn.example.com/uploads/abc/photo.jpg" {
		t.Errorf("URL = %q, want https://cdn.example.com/uploads/abc/photo.jpg", url)
	}
}

func TestURL_TrailingSlashStripped(t *testing.T) {
	st := newWithClient(&mockS3{}, "bucket", "https://cdn.example.com/")

	url := st.URL("test.png")
	if url != "https://cdn.example.com/test.png" {
		t.Errorf("URL = %q, want https://cdn.example.com/test.png", url)
	}
}

func TestValidateKey(t *testing.T) {
	t.Run("valid keys", func(t *testing.T) {
		cases := []struct {
			input string
			want  string
		}{
			{"normal/path/file.jpg", "normal/path/file.jpg"},
			{"/leading/slash.png", "leading/slash.png"},
			{"file.jpg", "file.jpg"},
		}
		for _, tc := range cases {
			got, err := validateKey(tc.input)
			if err != nil {
				t.Errorf("validateKey(%q) unexpected error: %v", tc.input, err)
				continue
			}
			if got != tc.want {
				t.Errorf("validateKey(%q) = %q, want %q", tc.input, got, tc.want)
			}
		}
	})

	t.Run("rejected keys", func(t *testing.T) {
		cases := []string{
			"../../etc/passwd",
			"/uploads/../secret",
			"./relative",
			"double//slash.gif",
			"",
			"trailing/",
		}
		for _, input := range cases {
			_, err := validateKey(input)
			if err == nil {
				t.Errorf("validateKey(%q) expected error, got nil", input)
			}
		}
	})
}

func TestSave_InvalidKey(t *testing.T) {
	mock := &mockS3{}
	st := newWithClient(mock, "bucket", "https://cdn.example.com")

	err := st.Save("../../etc/passwd", strings.NewReader("data"))
	if err == nil {
		t.Fatal("expected error for traversal key")
	}
	if !strings.Contains(err.Error(), "invalid key") {
		t.Errorf("error = %q, want mention of 'invalid key'", err.Error())
	}
	if len(mock.putKeys) != 0 {
		t.Errorf("S3 client should not have been called, got putKeys=%v", mock.putKeys)
	}
}

func TestDelete_InvalidKey(t *testing.T) {
	mock := &mockS3{}
	st := newWithClient(mock, "bucket", "https://cdn.example.com")

	err := st.Delete("../secret.txt")
	if err == nil {
		t.Fatal("expected error for traversal key")
	}
	if !strings.Contains(err.Error(), "invalid key") {
		t.Errorf("error = %q, want mention of 'invalid key'", err.Error())
	}
	if len(mock.deleted) != 0 {
		t.Errorf("S3 client should not have been called, got deleted=%v", mock.deleted)
	}
}

func TestMimeFromPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"photo.jpg", "image/jpeg"},
		{"photo.jpeg", "image/jpeg"},
		{"image.png", "image/png"},
		{"anim.gif", "image/gif"},
		{"modern.webp", "image/webp"},
		{"icon.svg", "image/svg+xml"},
		{"doc.pdf", "application/pdf"},
		{"unknown.xyz", "application/octet-stream"},
		{"noext", "application/octet-stream"},
	}
	for _, tc := range cases {
		got := mimeFromPath(tc.path)
		if got != tc.want {
			t.Errorf("mimeFromPath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestSave_ContentType(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"photo.png", "image/png"},
		{"photo.webp", "image/webp"},
		{"photo.gif", "image/gif"},
		{"doc.pdf", "application/pdf"},
	}
	for _, tc := range cases {
		mock := &mockS3{}
		st := newWithClient(mock, "bucket", "https://cdn.example.com")
		if err := st.Save(tc.path, bytes.NewReader(nil)); err != nil {
			t.Fatalf("Save(%q): %v", tc.path, err)
		}
		if mock.putCT[0] != tc.want {
			t.Errorf("Save(%q) Content-Type = %q, want %q", tc.path, mock.putCT[0], tc.want)
		}
	}
}

func TestNew_EmptyBucket(t *testing.T) {
	_, err := New(Config{Region: "us-east-1"})
	if err == nil || !strings.Contains(err.Error(), "empty bucket") {
		t.Errorf("expected empty bucket error, got: %v", err)
	}
}

func TestNew_EmptyRegion(t *testing.T) {
	_, err := New(Config{Bucket: "my-bucket"})
	if err == nil || !strings.Contains(err.Error(), "empty region") {
		t.Errorf("expected empty region error, got: %v", err)
	}
}

func TestNew_FallbackBaseURL(t *testing.T) {
	st, err := New(Config{
		Bucket:    "my-bucket",
		Region:    "eu-west-1",
		AccessKey: "key",
		SecretKey: "secret",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	url := st.URL("test.jpg")
	if !strings.HasPrefix(url, "https://s3.eu-west-1.amazonaws.com/my-bucket/") {
		t.Errorf("URL = %q, expected AWS S3 fallback prefix", url)
	}
}

func TestNew_CustomEndpoint(t *testing.T) {
	st, err := New(Config{
		Endpoint:  "https://minio.local:9000",
		Bucket:    "media",
		Region:    "us-east-1",
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	url := st.URL("test.jpg")
	if !strings.HasPrefix(url, "https://minio.local:9000/media/") {
		t.Errorf("URL = %q, expected custom endpoint prefix", url)
	}
}

func TestNew_ExplicitBaseURL(t *testing.T) {
	st, err := New(Config{
		Bucket:    "media",
		Region:    "us-east-1",
		AccessKey: "key",
		SecretKey: "secret",
		BaseURL:   "https://cdn.shop.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	url := st.URL("uploads/photo.jpg")
	if url != "https://cdn.shop.com/uploads/photo.jpg" {
		t.Errorf("URL = %q, want https://cdn.shop.com/uploads/photo.jpg", url)
	}
}

func TestSave_NoACLByDefault(t *testing.T) {
	mock := &mockS3{}
	st := newWithClient(mock, "bucket", "https://cdn.example.com")

	if err := st.Save("photo.jpg", strings.NewReader("data")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.putACL[0] != "" {
		t.Errorf("ACL = %q, want empty (omitted)", mock.putACL[0])
	}
}

func TestSave_PublicACL(t *testing.T) {
	mock := &mockS3{}
	st := newWithClient(mock, "bucket", "https://cdn.example.com")
	st.publicACL = true

	if err := st.Save("photo.jpg", strings.NewReader("data")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.putACL[0] != "public-read" {
		t.Errorf("ACL = %q, want public-read", mock.putACL[0])
	}
}

func TestIsPrivateDownloadsPath(t *testing.T) {
	if !media.IsPrivateDownloadsPath("private/downloads/x/y.pdf") {
		t.Fatal("expected private path")
	}
	if media.IsPrivateDownloadsPath("uploads/x/y.pdf") {
		t.Fatal("uploads must not be private prefix")
	}
}
