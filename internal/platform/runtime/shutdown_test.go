package runtime_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/runtime"
)

func TestShutdownBackground_TimeoutLogs(t *testing.T) {
	var buf bytes.Buffer
	log := logger.NewWithWriter(&buf, "info")
	never := make(chan struct{})

	start := time.Now()
	runtime.ShutdownBackground(log, 50*time.Millisecond, nil, nil, []<-chan struct{}{never})
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("ShutdownBackground took %s, want return within timeout", elapsed)
	}
	if !bytes.Contains(buf.Bytes(), []byte("background.shutdown.timeout")) {
		t.Errorf("expected background.shutdown.timeout log, got %s", buf.String())
	}
}

func TestShutdownBackground_WaitsAllDones(t *testing.T) {
	a := make(chan struct{})
	b := make(chan struct{})
	go func() {
		time.Sleep(30 * time.Millisecond)
		close(a)
	}()
	go func() {
		time.Sleep(40 * time.Millisecond)
		close(b)
	}()

	start := time.Now()
	runtime.ShutdownBackground(logger.NewWithWriter(&bytes.Buffer{}, "error"), 200*time.Millisecond, nil, nil, []<-chan struct{}{a, b})
	elapsed := time.Since(start)
	if elapsed < 30*time.Millisecond {
		t.Fatalf("ShutdownBackground returned in %s, want to wait for both dones", elapsed)
	}
	if elapsed > 150*time.Millisecond {
		t.Fatalf("ShutdownBackground took %s, want ~40ms (parallel wait, not hung)", elapsed)
	}
}
