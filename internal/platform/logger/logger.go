package logger

import (
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"
)

// Logger defines the structured logging interface.
type Logger interface {
	Info(event string, ctx map[string]interface{})
	Warn(event string, ctx map[string]interface{})
	Error(event string, err error, ctx map[string]interface{})
}

// entry is the JSON structure written per log line.
type entry struct {
	Timestamp string                 `json:"timestamp"`
	Level     string                 `json:"level"`
	Event     string                 `json:"event"`
	Message   string                 `json:"message,omitempty"`
	Context   map[string]interface{} `json:"context,omitempty"`
}

// JSONLogger writes structured JSON lines to an io.Writer.
type JSONLogger struct {
	mu    sync.Mutex
	out   io.Writer
	level Level
}

// Level represents log severity.
type Level int

const (
	LevelInfo Level = iota
	LevelWarn
	LevelError
)

// ParseLevel converts a string to a Level. Defaults to LevelInfo.
func ParseLevel(s string) Level {
	switch s {
	case "warn":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

func (l Level) String() string {
	switch l {
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "info"
	}
}

// New creates a JSONLogger that writes to stdout at the given level.
func New(level string) *JSONLogger {
	return &JSONLogger{
		out:   os.Stdout,
		level: ParseLevel(level),
	}
}

// NewWithWriter creates a JSONLogger that writes to w. Useful for testing.
func NewWithWriter(w io.Writer, level string) *JSONLogger {
	return &JSONLogger{
		out:   w,
		level: ParseLevel(level),
	}
}

func (l *JSONLogger) Info(event string, ctx map[string]interface{}) {
	if l.level > LevelInfo {
		return
	}
	l.write("info", event, nil, ctx)
}

func (l *JSONLogger) Warn(event string, ctx map[string]interface{}) {
	if l.level > LevelWarn {
		return
	}
	l.write("warn", event, nil, ctx)
}

func (l *JSONLogger) Error(event string, err error, ctx map[string]interface{}) {
	l.write("error", event, err, ctx)
}

func (l *JSONLogger) write(level, event string, err error, ctx map[string]interface{}) {
	e := entry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     level,
		Event:     event,
		Context:   ctx,
	}
	if err != nil {
		e.Message = err.Error()
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	data, jsonErr := json.Marshal(e)
	if jsonErr != nil {
		return
	}
	data = append(data, '\n')
	l.out.Write(data)
}
