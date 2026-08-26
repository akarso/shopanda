package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

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

func TestIOArgs(t *testing.T) {
	orig := append([]string(nil), os.Args...)
	t.Cleanup(func() { os.Args = orig })

	os.Args = []string{"app", "export:epr"}
	if got := ioArgs(); got != nil {
		t.Fatalf("ioArgs() = %v, want nil", got)
	}
	os.Args = []string{"app", "export:epr", "--include-empty", "out.csv"}
	got := ioArgs()
	if len(got) != 2 || got[0] != "--include-empty" || got[1] != "out.csv" {
		t.Fatalf("ioArgs() = %v", got)
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
			if from.Day() != 1 {
				t.Fatalf("default from day = %d, want 1", from.Day())
			}
		})
	}
}

type memLogger struct {
	info []string
	warn []string
	err  []string
}

func (m *memLogger) Info(event string, _ map[string]interface{}) {
	m.info = append(m.info, event)
}
func (m *memLogger) Warn(event string, _ map[string]interface{}) {
	m.warn = append(m.warn, event)
}
func (m *memLogger) Error(event string, _ error, _ map[string]interface{}) {
	m.err = append(m.err, event)
}

func stubIOOpenDB(t *testing.T) {
	t.Helper()
	prev := ioOpenDB
	t.Cleanup(func() { ioOpenDB = prev })
	ioOpenDB = func(string) (*sql.DB, error) {
		return sql.Open("pgx", "postgres://invalid:invalid@127.0.0.1:1/invalid?sslmode=disable")
	}
}

func TestRunIOCommand_ImportAndExport(t *testing.T) {
	cfg := &config.Config{}
	stubIOOpenDB(t)

	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.csv")
	if err := os.WriteFile(inPath, []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	atomicOut := filepath.Join(dir, "out-atomic.csv")
	atomicFailOut := filepath.Join(dir, "out-atomic-fail.csv")
	var afterOKRan bool

	tests := []struct {
		name     string
		opts     ioCommandOpts
		hook     func(t *testing.T) ioHook
		log      *memLogger
		wantErr  string
		wantInfo []string // optional ordered Info events when log != nil
		check    func(t *testing.T)
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
			hook: func(t *testing.T) ioHook {
				return func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
					if conn == nil || f == nil {
						t.Errorf("expected conn and file")
						return nil, errors.New("invalid conn or file")
					}
					return &ioOutcome{Fields: map[string]interface{}{"rows": 1}}, nil
				}
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
			hook: func(t *testing.T) ioHook {
				return func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
					return &ioOutcome{
						Fields: map[string]interface{}{"errors": 1},
						Errors: []string{"row 2: bad"},
					}, nil
				}
			},
			wantErr: "import completed with 1 row-level errors",
		},
		{
			name: "export atomic writes destination",
			opts: ioCommandOpts{
				Kind:          ioExport,
				Path:          atomicOut,
				StartEvent:    "export.test.start",
				CompleteEvent: "export.test.complete",
				RowErrorEvent: "export.test.row_error",
				AtomicExport:  true,
				TempPrefix:    "test-export-*.csv",
			},
			hook: func(t *testing.T) ioHook {
				return func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
					if _, err := f.WriteString("sku,qty\nA,1\n"); err != nil {
						return nil, err
					}
					return &ioOutcome{Fields: map[string]interface{}{"entries": 1}}, nil
				}
			},
			check: func(t *testing.T) {
				t.Helper()
				b, err := os.ReadFile(atomicOut)
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
			hook: func(t *testing.T) ioHook {
				return func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
					if _, err := f.WriteString("ok\n"); err != nil {
						return nil, err
					}
					return &ioOutcome{Fields: map[string]interface{}{"rows": 1}}, nil
				}
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
			hook: func(t *testing.T) ioHook {
				return func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
					t.Errorf("hook should not run")
					return nil, errors.New("hook should not run")
				}
			},
			wantErr: "open ",
		},
		{
			name: "complete after errors ordering",
			log:  &memLogger{},
			opts: ioCommandOpts{
				Kind:                ioImport,
				Path:                inPath,
				StartEvent:          "import.test.start",
				CompleteEvent:       "import.test.complete",
				RowErrorEvent:       "import.test.row_error",
				CompleteAfterErrors: true,
			},
			hook: func(t *testing.T) ioHook {
				return func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
					return &ioOutcome{Fields: map[string]interface{}{"rows": 1}}, nil
				}
			},
			wantInfo: []string{"import.test.start", "import.test.complete"},
		},
		{
			name: "afterOK success runs",
			opts: ioCommandOpts{
				Kind:          ioImport,
				Path:          inPath,
				StartEvent:    "import.test.start",
				CompleteEvent: "import.test.complete",
				RowErrorEvent: "import.test.row_error",
			},
			hook: func(t *testing.T) ioHook {
				afterOKRan = false
				return func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
					return &ioOutcome{
						Fields: map[string]interface{}{"rows": 1},
						AfterOK: func() error {
							afterOKRan = true
							return nil
						},
					}, nil
				}
			},
			check: func(t *testing.T) {
				t.Helper()
				if !afterOKRan {
					t.Fatal("AfterOK was not called")
				}
			},
		},
		{
			name: "afterOK failure",
			opts: ioCommandOpts{
				Kind:          ioImport,
				Path:          inPath,
				StartEvent:    "import.test.start",
				CompleteEvent: "import.test.complete",
				RowErrorEvent: "import.test.row_error",
			},
			hook: func(t *testing.T) ioHook {
				return func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
					return &ioOutcome{
						Fields: map[string]interface{}{"rows": 1},
						AfterOK: func() error {
							return errors.New("afterok failed")
						},
					}, nil
				}
			},
			wantErr: "afterok failed",
		},
		{
			name: "row error warn custom fail message",
			log:  &memLogger{},
			opts: ioCommandOpts{
				Kind:                ioImport,
				Path:                inPath,
				StartEvent:          "import.test.start",
				CompleteEvent:       "import.test.complete",
				RowErrorEvent:       "import.test.row_error",
				RowErrorWarn:        true,
				FailMessage:         "import completed with %d errors",
				CompleteAfterErrors: true,
			},
			hook: func(t *testing.T) ioHook {
				return func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
					return &ioOutcome{Errors: []string{"bad row"}}, nil
				}
			},
			wantErr: "import completed with 1 errors",
			check: func(t *testing.T) {
				t.Helper()
				// captured via outer log pointer — set below in loop
			},
		},
		{
			name: "atomic export hook failure leaves no destination or temp",
			opts: ioCommandOpts{
				Kind:          ioExport,
				Path:          atomicFailOut,
				StartEvent:    "export.test.start",
				CompleteEvent: "export.test.complete",
				RowErrorEvent: "export.test.row_error",
				AtomicExport:  true,
				TempPrefix:    "test-export-fail-*.csv",
			},
			hook: func(t *testing.T) ioHook {
				return func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
					if _, err := f.WriteString("partial\n"); err != nil {
						return nil, err
					}
					return nil, fmt.Errorf("export stock: boom")
				}
			},
			wantErr: "export stock: boom",
			check: func(t *testing.T) {
				t.Helper()
				if _, err := os.Stat(atomicFailOut); !os.IsNotExist(err) {
					t.Fatalf("destination should not exist, stat err=%v", err)
				}
				matches, err := filepath.Glob(filepath.Join(dir, "test-export-fail-*.csv"))
				if err != nil {
					t.Fatal(err)
				}
				if len(matches) != 0 {
					t.Fatalf("temp files left behind: %v", matches)
				}
			},
		},
		{
			name: "unknown io kind",
			opts: ioCommandOpts{
				Kind:          ioKind("nope"),
				Path:          inPath,
				StartEvent:    "x.start",
				CompleteEvent: "x.complete",
				RowErrorEvent: "x.row_error",
			},
			hook: func(t *testing.T) ioHook {
				return func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
					t.Errorf("hook should not run for unknown kind")
					return nil, errors.New("hook should not run")
				}
			},
			wantErr: `unknown io kind "nope"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := logger.Logger(&discardLog{})
			var mem *memLogger
			if tt.log != nil {
				mem = tt.log
				log = mem
			}
			// Reset mem logger between subtests when reusing pointer from table.
			if mem != nil {
				mem.info, mem.warn, mem.err = nil, nil, nil
			}

			err := runIOCommand(cfg, log, tt.opts, tt.hook(t))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want substr %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("runIOCommand: %v", err)
			}
			if len(tt.wantInfo) > 0 {
				if mem == nil {
					t.Fatal("wantInfo set without mem logger")
				}
				if strings.Join(mem.info, ",") != strings.Join(tt.wantInfo, ",") {
					t.Fatalf("info events = %v, want %v", mem.info, tt.wantInfo)
				}
			}
			if tt.name == "row error warn custom fail message" {
				if mem == nil || len(mem.warn) == 0 || mem.warn[0] != "import.test.row_error" {
					t.Fatalf("warn events = %v, want import.test.row_error", mem.warn)
				}
				if len(mem.err) != 0 {
					t.Fatalf("err events = %v, want none (RowErrorWarn)", mem.err)
				}
			}
			if tt.check != nil {
				tt.check(t)
			}
		})
	}
}

type discardLog struct{}

func (discardLog) Info(string, map[string]interface{})         {}
func (discardLog) Warn(string, map[string]interface{})         {}
func (discardLog) Error(string, error, map[string]interface{}) {}

func TestRunIOCommand_ImportMissingFileDoesNotOpenDB(t *testing.T) {
	cfg := &config.Config{}
	log := discardLog{}

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
		t.Errorf("hook should not run")
		return nil, errors.New("hook should not run")
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

// Ensure memLogger implements logger.Logger.
var _ logger.Logger = (*memLogger)(nil)
var _ logger.Logger = discardLog{}
