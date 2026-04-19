package s3store

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/akarso/shopanda/internal/domain/media"
)

// Compile-time interface check.
var _ media.Storage = (*Storage)(nil)

// --- mock S3 client ---

type mockS3 struct {
	putErr    error
	deleteErr error
	putKeys   []string
	putCT     []string // Content-Type per call
	putCC     []string // Cache-Control per call
	putACL    []string // ACL per call (empty string when omitted)
	deleted   []string
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

// --- tests ---

func TestName(t *testing.T) {
	st := newWithClient(&mockS3{}, "bucket", "https://cdn.example.com")
	if st.Name() != "s3" {
		t.Errorf("Name() = %q, want %q", st.Name(), "s3")
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
