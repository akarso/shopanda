package localfs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/media"
)

func TestSave_PrivateModesAndAncestorChmod(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Legacy world-traversable root (simulates upgrade from public/media).
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := NewWithSecret(root, "/media", []byte("unit-test-sign-secret-32-bytes!!"))
	if err != nil {
		t.Fatal(err)
	}
	key := "private/blobs/metaldogs/source/2026/11/a.jpg"
	if err := s.Save(key, strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	full, err := s.resolveSafePath(key)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(full)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o, want 0600", fi.Mode().Perm())
	}
	dir := filepath.Dir(full)
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %o, want 0700", di.Mode().Perm())
	}
	// Public key keeps looser modes.
	pub := "uploads/x.jpg"
	if err := s.Save(pub, strings.NewReader("y")); err != nil {
		t.Fatal(err)
	}
	pfull, err := s.resolveSafePath(pub)
	if err != nil {
		t.Fatal(err)
	}
	pfi, err := os.Stat(pfull)
	if err != nil {
		t.Fatal(err)
	}
	if pfi.Mode().Perm() != 0o644 {
		t.Fatalf("public file mode = %o, want 0644", pfi.Mode().Perm())
	}
}

func TestResolveSafePath_SymlinkEscapeRejected(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("leak"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	s, err := NewWithSecret(root, "/media", []byte("unit-test-sign-secret-32-bytes!!"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save("escape/secret.txt", strings.NewReader("nope")); err == nil {
		t.Fatal("Save via symlink escape want error")
	}
	if _, err := s.Open("escape/secret.txt"); err == nil {
		t.Fatal("Open via symlink escape want error")
	}
	if _, err := s.SignedURL(t.Context(), "escape/secret.txt", media.SignURLOptions{TTL: time.Minute}); err == nil {
		t.Fatal("SignedURL via symlink escape want error")
	}
}

func TestParseToken_V0CompatAndV1Disposition(t *testing.T) {
	t.Parallel()
	s, err := NewWithSecret(t.TempDir(), "/media", []byte("unit-test-sign-secret-32-bytes!!"))
	if err != nil {
		t.Fatal(err)
	}
	key := "private/downloads/a/guide.pdf"
	if err := s.Save(key, strings.NewReader("%PDF")); err != nil {
		t.Fatal(err)
	}
	exp := time.Now().UTC().Add(time.Minute).Unix()

	// Legacy empty filename.
	tok, err := s.signV0ForTest(key, "", exp)
	if err != nil {
		t.Fatal(err)
	}
	path, filename, disp, err := s.ParseToken(tok)
	if err != nil || path != key || filename != "" || disp != media.DispositionAttachment {
		t.Fatalf("v0 empty: path=%q file=%q disp=%q err=%v", path, filename, disp, err)
	}

	// Legacy non-empty filename must not steal last byte as disposition.
	tok, err = s.signV0ForTest(key, "guide.pdf", exp)
	if err != nil {
		t.Fatal(err)
	}
	path, filename, disp, err = s.ParseToken(tok)
	if err != nil || path != key || filename != "guide.pdf" || disp != media.DispositionAttachment {
		t.Fatalf("v0 named: path=%q file=%q disp=%q err=%v", path, filename, disp, err)
	}

	// v1 inline + attachment.
	tok, err = s.sign(key, "guide.pdf", exp, media.DispositionInline)
	if err != nil {
		t.Fatal(err)
	}
	path, filename, disp, err = s.ParseToken(tok)
	if err != nil || filename != "guide.pdf" || disp != media.DispositionInline {
		t.Fatalf("v1 inline: file=%q disp=%q err=%v", filename, disp, err)
	}
	tok, err = s.sign(key, "guide.pdf", exp, media.DispositionAttachment)
	if err != nil {
		t.Fatal(err)
	}
	_, filename, disp, err = s.ParseToken(tok)
	if err != nil || filename != "guide.pdf" || disp != media.DispositionAttachment {
		t.Fatalf("v1 attach: file=%q disp=%q err=%v", filename, disp, err)
	}
}
