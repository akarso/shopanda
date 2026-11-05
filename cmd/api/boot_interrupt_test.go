package main

import (
	"context"
	"strings"
	"testing"
)

func TestErrBootInterrupted_NilAndBackground(t *testing.T) {
	if err := errBootInterrupted(nil); err != nil {
		t.Fatalf("nil ctx: %v", err)
	}
	if err := errBootInterrupted(context.Background()); err != nil {
		t.Fatalf("background: %v", err)
	}
}

func TestErrBootInterrupted_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := errBootInterrupted(ctx)
	if err == nil || !strings.Contains(err.Error(), "boot interrupted") {
		t.Fatalf("got %v", err)
	}
}
