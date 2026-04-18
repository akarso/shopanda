package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/lib/pq"
)

// Compile-time check.
var _ shipping.ZoneRepository = (*ZoneRepo)(nil)

// ZoneRepo implements shipping.ZoneRepository using PostgreSQL.
type ZoneRepo struct {
	db *sql.DB
}

// NewZoneRepo returns a new ZoneRepo backed by db.
func NewZoneRepo(db *sql.DB) (*ZoneRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewZoneRepo: nil *sql.DB")
	}
	return &ZoneRepo{db: db}, nil
}

// ── Zone operations ─────────────────────────────────────────────────────

// ListZones returns all shipping zones ordered by priority descending.
func (r *ZoneRepo) ListZones(ctx context.Context) ([]shipping.Zone, error) {
	const q = `SELECT id, name, countries, priority, active, created_at, updated_at
		FROM shipping_zones ORDER BY priority DESC, name ASC`
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("zone_repo: list: %w", err)
	}
	defer rows.Close()

	var zones []shipping.Zone
	for rows.Next() {
		var z shipping.Zone
		if err := rows.Scan(&z.ID, &z.Name, pq.Array(&z.Countries),
			&z.Priority, &z.Active, &z.CreatedAt, &z.UpdatedAt); err != nil {
			return nil, fmt.Errorf("zone_repo: list scan: %w", err)
		}
		zones = append(zones, z)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("zone_repo: list rows: %w", err)
	}
	return zones, nil
}

// FindZoneByID returns a zone by its ID. Returns (nil, nil) when not found.
func (r *ZoneRepo) FindZoneByID(ctx context.Context, id string) (*shipping.Zone, error) {
	if id == "" {
		return nil, fmt.Errorf("zone_repo: find: empty id")
	}
	const q = `SELECT id, name, countries, priority, active, created_at, updated_at
		FROM shipping_zones WHERE id = $1`
	var z shipping.Zone
	err := r.db.QueryRowContext(ctx, q, id).Scan(&z.ID, &z.Name,
		pq.Array(&z.Countries), &z.Priority, &z.Active, &z.CreatedAt, &z.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("zone_repo: find by id: %w", err)
	}
	return &z, nil
}

// CreateZone persists a new shipping zone.
func (r *ZoneRepo) CreateZone(ctx context.Context, z *shipping.Zone) error {
	if z == nil {
		return fmt.Errorf("zone_repo: create: zone must not be nil")
	}
	const q = `INSERT INTO shipping_zones (id, name, countries, priority, active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err := r.db.ExecContext(ctx, q, z.ID, z.Name, pq.Array(z.Countries),
		z.Priority, z.Active, z.CreatedAt, z.UpdatedAt)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return apperror.Conflict("shipping zone already exists")
		}
		return fmt.Errorf("zone_repo: create: %w", err)
	}
	return nil
}

// UpdateZone updates a shipping zone's mutable fields.
func (r *ZoneRepo) UpdateZone(ctx context.Context, z *shipping.Zone) error {
	if z == nil {
		return fmt.Errorf("zone_repo: update: zone must not be nil")
	}
	const q = `UPDATE shipping_zones SET name = $1, countries = $2, priority = $3, active = $4, updated_at = $5
		WHERE id = $6`
	result, err := r.db.ExecContext(ctx, q, z.Name, pq.Array(z.Countries),
		z.Priority, z.Active, z.UpdatedAt, z.ID)
	if err != nil {
		return fmt.Errorf("zone_repo: update: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("zone_repo: update rows: %w", err)
	}
	if rows == 0 {
		return apperror.NotFound("shipping zone not found")
	}
	return nil
}

// DeleteZone removes a zone and its rate tiers (cascaded by FK).
func (r *ZoneRepo) DeleteZone(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("zone_repo: delete: empty id")
	}
	const q = `DELETE FROM shipping_zones WHERE id = $1`
	result, err := r.db.ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("zone_repo: delete: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("zone_repo: delete rows: %w", err)
	}
	if rows == 0 {
		return apperror.NotFound("shipping zone not found")
	}
	return nil
}

// ── RateTier operations ─────────────────────────────────────────────────

// ListRateTiers returns all rate tiers for a zone ordered by min_weight.
func (r *ZoneRepo) ListRateTiers(ctx context.Context, zoneID string) ([]shipping.RateTier, error) {
	if zoneID == "" {
		return nil, fmt.Errorf("zone_repo: list rates: empty zone id")
	}
	const q = `SELECT id, zone_id, min_weight, max_weight, price, currency
		FROM shipping_rate_tiers WHERE zone_id = $1 ORDER BY min_weight ASC`
	rows, err := r.db.QueryContext(ctx, q, zoneID)
	if err != nil {
		return nil, fmt.Errorf("zone_repo: list rates: %w", err)
	}
	defer rows.Close()

	var tiers []shipping.RateTier
	for rows.Next() {
		var rt shipping.RateTier
		var price int64
		var currency string
		if err := rows.Scan(&rt.ID, &rt.ZoneID, &rt.MinWeight, &rt.MaxWeight,
			&price, &currency); err != nil {
			return nil, fmt.Errorf("zone_repo: list rates scan: %w", err)
		}
		m, err := shared.NewMoney(price, currency)
		if err != nil {
			return nil, fmt.Errorf("zone_repo: rate tier money: %w", err)
		}
		rt.Price = m
		tiers = append(tiers, rt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("zone_repo: list rates rows: %w", err)
	}
	return tiers, nil
}

// CreateRateTier persists a new rate tier.
func (r *ZoneRepo) CreateRateTier(ctx context.Context, rt *shipping.RateTier) error {
	if rt == nil {
		return fmt.Errorf("zone_repo: create rate: rate tier must not be nil")
	}
	const q = `INSERT INTO shipping_rate_tiers (id, zone_id, min_weight, max_weight, price, currency)
		VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := r.db.ExecContext(ctx, q, rt.ID, rt.ZoneID, rt.MinWeight, rt.MaxWeight,
		rt.Price.Amount(), rt.Price.Currency())
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return apperror.Conflict("rate tier already exists")
		}
		if errors.As(err, &pqErr) && pqErr.Code == "23503" {
			return apperror.Validation("shipping zone not found")
		}
		return fmt.Errorf("zone_repo: create rate: %w", err)
	}
	return nil
}

// UpdateRateTier updates a rate tier's fields.
func (r *ZoneRepo) UpdateRateTier(ctx context.Context, rt *shipping.RateTier) error {
	if rt == nil {
		return fmt.Errorf("zone_repo: update rate: rate tier must not be nil")
	}
	const q = `UPDATE shipping_rate_tiers SET zone_id = $1, min_weight = $2, max_weight = $3, price = $4, currency = $5
		WHERE id = $6`
	result, err := r.db.ExecContext(ctx, q, rt.ZoneID, rt.MinWeight, rt.MaxWeight,
		rt.Price.Amount(), rt.Price.Currency(), rt.ID)
	if err != nil {
		return fmt.Errorf("zone_repo: update rate: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("zone_repo: update rate rows: %w", err)
	}
	if rows == 0 {
		return apperror.NotFound("rate tier not found")
	}
	return nil
}

// DeleteRateTier removes a rate tier by ID.
func (r *ZoneRepo) DeleteRateTier(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("zone_repo: delete rate: empty id")
	}
	const q = `DELETE FROM shipping_rate_tiers WHERE id = $1`
	result, err := r.db.ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("zone_repo: delete rate: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("zone_repo: delete rate rows: %w", err)
	}
	if rows == 0 {
		return apperror.NotFound("rate tier not found")
	}
	return nil
}
