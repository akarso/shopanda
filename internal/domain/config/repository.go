package config

import "context"

// Entry represents a single configuration row stored in the database.
type Entry struct {
	Key   string
	Value interface{}
}

// Repository abstracts database-backed configuration storage.
// The default implementation uses PostgreSQL; plugins can provide alternatives.
type Repository interface {
	// Get retrieves the value for key. Returns (nil, nil) on miss.
	Get(ctx context.Context, key string) (interface{}, error)

	// Set stores value under key (upsert).
	Set(ctx context.Context, key string, value interface{}) error

	// Delete removes the entry for key. A missing key is not an error.
	Delete(ctx context.Context, key string) error

	// All returns every stored config entry.
	All(ctx context.Context) ([]Entry, error)
}
