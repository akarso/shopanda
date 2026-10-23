package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	cacheApp "github.com/akarso/shopanda/internal/application/cache"
	"github.com/akarso/shopanda/internal/domain/cache"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func TestParseCacheClearArgs(t *testing.T) {
	req, err := parseCacheClearArgs([]string{"--prefix=product:1:"})
	if err != nil || req.Prefix != "product:1:" || req.All {
		t.Fatalf("prefix: req=%+v err=%v", req, err)
	}
	req, err = parseCacheClearArgs([]string{"--all"})
	if err != nil || !req.All {
		t.Fatalf("all: req=%+v err=%v", req, err)
	}
	if _, err := parseCacheClearArgs(nil); err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("empty args err = %v", err)
	}
	if _, err := parseCacheClearArgs([]string{"--prefix=a", "--all"}); err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("both selectors err = %v", err)
	}
	if _, err := parseCacheClearArgs([]string{"--nope"}); err == nil || !strings.Contains(err.Error(), "unknown argument") {
		t.Fatalf("unknown err = %v", err)
	}
}

func TestRunCacheStats_UnknownArgument(t *testing.T) {
	err := runCacheStats(io.Discard, &config.Config{}, logger.New("error"), []string{"--nope"})
	if err == nil || !strings.Contains(err.Error(), "unknown argument") {
		t.Fatalf("err = %v, want unknown argument", err)
	}
}

func TestFormatCacheStats_TextAndJSON(t *testing.T) {
	tags := int64(4)
	snap := cacheApp.Snapshot{
		L2: cache.Stats{Backend: "postgres", Keys: 12, TagRows: &tags},
		L1: []cacheApp.L1Snapshot{{Name: "rbac.catalog", Entries: 1, Hits: 9, Misses: 1}},
	}
	var buf bytes.Buffer
	if err := formatCacheStats(&buf, snap, false); err != nil {
		t.Fatalf("text: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"postgres", "12", "4", "rbac.catalog", "hits=9"} {
		if !strings.Contains(out, want) {
			t.Fatalf("text missing %q:\n%s", want, out)
		}
	}

	buf.Reset()
	if err := formatCacheStats(&buf, snap, true); err != nil {
		t.Fatalf("json: %v", err)
	}
	if !strings.Contains(buf.String(), `"backend": "postgres"`) {
		t.Fatalf("json = %s", buf.String())
	}

	buf.Reset()
	tagsApprox := int64(3)
	approx := cacheApp.Snapshot{L2: cache.Stats{Backend: "postgres", Keys: 99, TagRows: &tagsApprox, Approximate: true}}
	if err := formatCacheStats(&buf, approx, false); err != nil {
		t.Fatalf("approx text: %v", err)
	}
	out = buf.String()
	if !strings.Contains(out, "99 (approximate)") || !strings.Contains(out, "3 (approximate)") {
		t.Fatalf("approx text = %q, want keys and tag rows marked approximate", out)
	}
}

func TestFormatCacheStats_CLIHasNoL1(t *testing.T) {
	var buf bytes.Buffer
	if err := formatCacheStats(&buf, cacheApp.Snapshot{L2: cache.Stats{Backend: "redis"}}, false); err != nil {
		t.Fatalf("format: %v", err)
	}
	if !strings.Contains(buf.String(), "none in this process") {
		t.Fatalf("output = %q, want CLI L1 note", buf.String())
	}
}

func TestFormatCacheClear(t *testing.T) {
	n := int64(3)
	var buf bytes.Buffer
	if err := formatCacheClear(&buf, cacheApp.ClearResult{Mode: cacheApp.ClearAll, Deleted: &n}); err != nil {
		t.Fatalf("format: %v", err)
	}
	if !strings.Contains(buf.String(), "all cache") || !strings.Contains(buf.String(), "3 deleted") {
		t.Fatalf("output = %q", buf.String())
	}
}

func TestRunCacheClear_UnknownArgument(t *testing.T) {
	err := runCacheClear(io.Discard, &config.Config{}, logger.New("error"), []string{"--nope"})
	if err == nil || !strings.Contains(err.Error(), "unknown argument") {
		t.Fatalf("err = %v", err)
	}
}
