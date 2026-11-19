package localfs

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRead_CanceledBetweenChunks(t *testing.T) {
	prevSize := readChunkSize
	readChunkSize = 64
	t.Cleanup(func() {
		readChunkSize = prevSize
		testAfterReadChunk = nil
	})

	s, err := NewWithSecret(t.TempDir(), "/media", []byte("unit-test-sign-secret-32-bytes!!"))
	if err != nil {
		t.Fatal(err)
	}
	content := bytes.Repeat([]byte("x"), 512) // several 64-byte chunks
	if err := s.Save("blobs/chunked.bin", bytes.NewReader(content)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	chunks := 0
	testAfterReadChunk = func(n int) {
		chunks++
		if chunks == 1 {
			cancel()
		}
	}

	got, err := s.Read(ctx, "blobs/chunked.bin")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled (in-loop check)", err)
	}
	if chunks < 1 {
		t.Fatal("expected at least one chunk before cancel")
	}
	if len(got) >= len(content) {
		t.Fatalf("got full %d bytes despite cancel", len(got))
	}
	if got != nil && strings.Count(string(got), "x") == len(content) {
		t.Fatal("full content returned on cancel")
	}
}
