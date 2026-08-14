package main

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/lib/pq"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func TestRequireIOPath(t *testing.T) {
	orig := append([]string(nil), os.Args...)
	t.Cleanup(func() { os.Args = orig })

	tests := []struct {
		name    string
		args    []string
		cmd     string
		want    string
		wantErr string
	}{
		{
			name:    "missing path",
			args:    []string{"app", "import:products"},
			cmd:     "import:products",
			wantErr: "usage: app import:products <file.csv>",
		},
		{
			name: "ok",
			args: []string{"app", "import:products", "in.csv"},
			cmd:  "import:products",
			want: "in.csv",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Args = tt.args
			got, err := requireIOPath(tt.cmd)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("requireIOPath: %v", err)
			}
			if got != tt.want {
				t.Fatalf("path = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseEprExportArgs(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantPath      string
		wantEmpty     bool
		wantErrSubstr string
	}{
		{name: "path only", args: []string{"out.csv"}, wantPath: "out.csv"},
		{name: "include empty", args: []string{"--include-empty", "out.csv"}, wantPath: "out.csv", wantEmpty: true},
		{name: "missing path", args: nil, wantErrSubstr: "usage:"},
		{name: "unknown flag", args: []string{"--nope", "out.csv"}, wantErrSubstr: "unknown flag"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, empty, err := parseEprExportArgs(tt.args)
			if tt.wantErrSubstr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Fatalf("err = %v, want substr %q", err, tt.wantErrSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseEprExportArgs: %v", err)
			}
			if path != tt.wantPath || empty != tt.wantEmpty {
				t.Fatalf("got (%q, %v), want (%q, %v)", path, empty, tt.wantPath, tt.wantEmpty)
			}
		})
	}
}

func TestParseOssExportArgs(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantPath      string
		wantSummary   bool
		wantErrSubstr string
	}{
		{name: "path only defaults quarter", args: []string{"out.csv"}, wantPath: "out.csv"},
		{name: "summary", args: []string{"--summary", "out.csv"}, wantPath: "out.csv", wantSummary: true},
		{name: "from without to", args: []string{"--from=2026-01-01", "out.csv"}, wantErrSubstr: "--from and --to"},
		{name: "missing path", args: nil, wantErrSubstr: "usage:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, summary, from, to, err := parseOssExportArgs(tt.args)
			if tt.wantErrSubstr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Fatalf("err = %v, want substr %q", err, tt.wantErrSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseOssExportArgs: %v", err)
			}
			if path != tt.wantPath || summary != tt.wantSummary {
				t.Fatalf("got (%q, %v), want (%q, %v)", path, summary, tt.wantPath, tt.wantSummary)
			}
			if from.IsZero() || !to.After(from) {
				t.Fatalf("invalid range from=%v to=%v", from, to)
			}
			// Default quarter start should be on the 1st.
			if from.Day() != 1 {
				t.Fatalf("default from day = %d, want 1", from.Day())
			}
		})
	}
}

func TestRunIOCommand_ImportAndExport(t *testing.T) {
	cfg := &config.Config{}
	log := logger.NewWithWriter(io.Discard, "error")

	prevOpen := ioOpenDB
	t.Cleanup(func() { ioOpenDB = prevOpen })
	ioOpenDB = func(string) (*sql.DB, error) {
		// Open without Ping so tests do not need a live Postgres.
		return sql.Open("postgres", "postgres://invalid:invalid@127.0.0.1:1/invalid?sslmode=disable")
	}

	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.csv")
	if err := os.WriteFile(inPath, []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		opts    ioCommandOpts
		hook    ioHook
		wantErr string
		check   func(t *testing.T)
	}{
		{
			name: "import success",
			opts: ioCommandOpts{
				Kind:          ioImport,
				Path:          inPath,
				StartEvent:    "import.test.start",
				CompleteEvent: "import.test.complete",
				RowErrorEvent: "import.test.row_error",
			},
			hook: func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
				if conn == nil || f == nil {
					t.Fatal("expected conn and file")
				}
				return &ioOutcome{Fields: map[string]interface{}{"rows": 1}}, nil
			},
		},
		{
			name: "import row errors fail",
			opts: ioCommandOpts{
				Kind:          ioImport,
				Path:          inPath,
				StartEvent:    "import.test.start",
				CompleteEvent: "import.test.complete",
				RowErrorEvent: "import.test.row_error",
			},
			hook: func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
				return &ioOutcome{
					Fields: map[string]interface{}{"errors": 1},
					Errors: []string{"row 2: bad"},
				}, nil
			},
			wantErr: "import completed with 1 row-level errors",
		},
		{
			name: "export atomic writes destination",
			opts: ioCommandOpts{
				Kind:          ioExport,
				Path:          filepath.Join(dir, "out-atomic.csv"),
				StartEvent:    "export.test.start",
				CompleteEvent: "export.test.complete",
				RowErrorEvent: "export.test.row_error",
				AtomicExport:  true,
				TempPrefix:    "test-export-*.csv",
			},
			hook: func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
				if _, err := f.WriteString("sku,qty\nA,1\n"); err != nil {
					return nil, err
				}
				return &ioOutcome{Fields: map[string]interface{}{"entries": 1}}, nil
			},
			check: func(t *testing.T) {
				t.Helper()
				b, err := os.ReadFile(filepath.Join(dir, "out-atomic.csv"))
				if err != nil {
					t.Fatal(err)
				}
				if string(b) != "sku,qty\nA,1\n" {
					t.Fatalf("export contents = %q", b)
				}
			},
		},
		{
			name: "export direct create",
			opts: ioCommandOpts{
				Kind:          ioExport,
				Path:          filepath.Join(dir, "out-direct.csv"),
				StartEvent:    "export.test.start",
				CompleteEvent: "export.test.complete",
				RowErrorEvent: "export.test.row_error",
				AtomicExport:  false,
			},
			hook: func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
				if _, err := f.WriteString("ok\n"); err != nil {
					return nil, err
				}
				return &ioOutcome{Fields: map[string]interface{}{"rows": 1}}, nil
			},
			check: func(t *testing.T) {
				t.Helper()
				b, err := os.ReadFile(filepath.Join(dir, "out-direct.csv"))
				if err != nil {
					t.Fatal(err)
				}
				if string(b) != "ok\n" {
					t.Fatalf("export contents = %q", b)
				}
			},
		},
		{
			name: "import missing file",
			opts: ioCommandOpts{
				Kind:          ioImport,
				Path:          filepath.Join(dir, "missing.csv"),
				StartEvent:    "import.test.start",
				CompleteEvent: "import.test.complete",
				RowErrorEvent: "import.test.row_error",
			},
			hook: func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
				t.Fatal("hook should not run")
				return nil, nil
			},
			wantErr: "open ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runIOCommand(cfg, log, tt.opts, tt.hook)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want substr %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("runIOCommand: %v", err)
			}
			if tt.check != nil {
				tt.check(t)
			}
		})
	}
}

func TestRunIOCommand_ImportMissingFileDoesNotOpenDB(t *testing.T) {
	cfg := &config.Config{}
	log := logger.NewWithWriter(io.Discard, "error")

	prevOpen := ioOpenDB
	t.Cleanup(func() { ioOpenDB = prevOpen })

	openedDB := false
	ioOpenDB = func(string) (*sql.DB, error) {
		openedDB = true
		return nil, errors.New("db must not open before import file validation")
	}

	missing := filepath.Join(t.TempDir(), "missing.csv")
	err := runIOCommand(cfg, log, ioCommandOpts{
		Kind:          ioImport,
		Path:          missing,
		StartEvent:    "import.test.start",
		CompleteEvent: "import.test.complete",
		RowErrorEvent: "import.test.row_error",
	}, func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
		t.Fatal("hook should not run")
		return nil, nil
	})
	if openedDB {
		t.Fatal("ioOpenDB was called before import file open failed")
	}
	if err == nil || !strings.Contains(err.Error(), "open "+missing) {
		t.Fatalf("err = %v, want open path error for %q", err, missing)
	}
	if strings.Contains(err.Error(), "database:") {
		t.Fatalf("err = %v, must not surface database failure for missing import file", err)
	}
}
