package main

import (
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestBootSignal_StopWithoutSignal(t *testing.T) {
	sigCh := make(chan os.Signal, 1)
	b := newBootSignal(sigCh)
	if err := b.Stop(); err != nil {
		t.Fatalf("Stop with no signal: %v", err)
	}
}

func TestBootSignal_StopDrainsPendingSignal(t *testing.T) {
	// Signal already buffered before Stop — the race the NotifyContext
	// order missed. newBootSignal's goroutine may or may not have taken it;
	// either path must report boot interrupted.
	sigCh := make(chan os.Signal, 1)
	sigCh <- syscall.SIGTERM
	b := newBootSignal(sigCh)
	if err := b.Stop(); err == nil || !strings.Contains(err.Error(), "boot interrupted") {
		t.Fatalf("got %v, want boot interrupted", err)
	}
}

func TestBootSignal_StopAfterWatcherConsumed(t *testing.T) {
	sigCh := make(chan os.Signal, 1)
	b := newBootSignal(sigCh)
	sigCh <- syscall.SIGINT
	select {
	case <-b.Context().Done():
	case <-time.After(2 * time.Second):
		t.Fatal("context not cancelled after signal")
	}
	if err := b.Stop(); err == nil || !strings.Contains(err.Error(), "boot interrupted") {
		t.Fatalf("got %v, want boot interrupted", err)
	}
}

func TestBootSignal_LateSignalBeforeStop(t *testing.T) {
	// Mirrors the audit repro: deliver into the buffer immediately before
	// Stop. Must not be lost across many iterations (NotifyContext missed
	// this race in 2000/2000 worst-case boots).
	for i := 0; i < 2000; i++ {
		sigCh := make(chan os.Signal, 1)
		b := newBootSignal(sigCh)
		sigCh <- syscall.SIGTERM
		if err := b.Stop(); err == nil {
			t.Fatalf("iteration %d: late buffered signal must interrupt boot", i)
		}
	}
}
