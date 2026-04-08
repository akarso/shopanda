package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	domainCfg "github.com/akarso/shopanda/internal/domain/config"
)

// Compile-time check.
var _ domainCfg.Repository = (*ConfigRepo)(nil)

// configDB is the subset of *sql.DB / *sql.Tx used by ConfigRepo.
type configDB interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
}

// ConfigRepo implements config.Repository using PostgreSQL.
type ConfigRepo struct {
	db configDB
}

// NewConfigRepo returns a ConfigRepo backed by db.
// db may be a *sql.DB or *sql.Tx; pass a Tx to run operations inside an
// existing transaction (e.g. bulk import).
func NewConfigRepo(db configDB) *ConfigRepo {
	return &ConfigRepo{db: db}
}

// Get retrieves the value for key. Returns (nil, nil) on miss.
func (r *ConfigRepo) Get(ctx context.Context, key string) (interface{}, error) {
	var raw json.RawMessage
	err := r.db.QueryRowContext(ctx,
		`SELECT value FROM config WHERE key = $1`, key,
	).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config_repo: get %q: %w", key, err)
	}

	var val interface{}
	if err := json.Unmarshal(raw, &val); err != nil {
		return nil, fmt.Errorf("config_repo: unmarshal %q: %w", key, err)
	}
	return val, nil
}

// Set stores value under key (upsert). Value must not be nil.
func (r *ConfigRepo) Set(ctx context.Context, key string, value interface{}) error {
	if value == nil {
		return errors.New("config_repo: value must not be nil")
	}

	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("config_repo: marshal %q: %w", key, err)
	}

	_, err = r.db.ExecContext(ctx,
		`INSERT INTO config (key, value)
		 VALUES ($1, $2)
		 ON CONFLICT (key)
		 DO UPDATE SET value = EXCLUDED.value`,
		key, data,
	)
	if err != nil {
		return fmt.Errorf("config_repo: set %q: %w", key, err)
	}
	return nil
}

// Delete removes the entry for key. A missing key is not an error.
func (r *ConfigRepo) Delete(ctx context.Context, key string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM config WHERE key = $1`, key)
	if err != nil {
		return fmt.Errorf("config_repo: delete %q: %w", key, err)
	}
	return nil
}

// All returns every stored config entry.
func (r *ConfigRepo) All(ctx context.Context) ([]domainCfg.Entry, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT key, value FROM config ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("config_repo: all: %w", err)
	}
	defer rows.Close()

	var entries []domainCfg.Entry
	for rows.Next() {
		var key string
		var raw json.RawMessage
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, fmt.Errorf("config_repo: scan: %w", err)
		}
		var val interface{}
		if err := json.Unmarshal(raw, &val); err != nil {
			return nil, fmt.Errorf("config_repo: unmarshal %q: %w", key, err)
		}
		entries = append(entries, domainCfg.Entry{Key: key, Value: val})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("config_repo: rows: %w", err)
	}
	return entries, nil
}
