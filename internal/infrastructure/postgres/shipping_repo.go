package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/lib/pq"
)

// Compile-time check that ShippingRepo implements shipping.ShipmentRepository.
var _ shipping.ShipmentRepository = (*ShippingRepo)(nil)

// ShippingRepo implements shipping.ShipmentRepository using PostgreSQL.
type ShippingRepo struct {
	db *sql.DB
}

// NewShippingRepo returns a new ShippingRepo backed by db.
func NewShippingRepo(db *sql.DB) *ShippingRepo {
	return &ShippingRepo{db: db}
}

// hydrateShipment reads a shipment row from a *sql.Row.
func hydrateShipment(row *sql.Row) (*shipping.Shipment, error) {
	var id, orderID string
	var status, method string
	var cost int64
	var currency string
	var trackingNumber, providerRef sql.NullString
	var createdAt, updatedAt time.Time
	err := row.Scan(&id, &orderID, &method, &status,
		&cost, &currency, &trackingNumber, &providerRef, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	money, err := shared.NewMoney(cost, currency)
	if err != nil {
		return nil, fmt.Errorf("shipping_repo: cost money: %w", err)
	}
	var track string
	if trackingNumber.Valid {
		track = trackingNumber.String
	}
	var ref string
	if providerRef.Valid {
		ref = providerRef.String
	}
	return shipping.NewShipmentFromDB(id, orderID, shipping.ShippingMethod(method), status, money, track, ref, createdAt, updatedAt)
}

const shipmentColumns = `id, order_id, method, status, cost, currency, tracking_number, provider_ref, created_at, updated_at`

// FindByID returns a shipment by its ID.
// Returns (nil, nil) when not found.
func (r *ShippingRepo) FindByID(ctx context.Context, id string) (*shipping.Shipment, error) {
	if id == "" {
		return nil, fmt.Errorf("shipping_repo: find: empty id")
	}
	q := `SELECT ` + shipmentColumns + ` FROM shipments WHERE id = $1`
	s, err := hydrateShipment(r.db.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("shipping_repo: find by id: %w", err)
	}
	return s, nil
}

// FindByOrderID returns the shipment for a given order.
// Returns (nil, nil) when no shipment exists for the order.
func (r *ShippingRepo) FindByOrderID(ctx context.Context, orderID string) (*shipping.Shipment, error) {
	if orderID == "" {
		return nil, fmt.Errorf("shipping_repo: find by order: empty order id")
	}
	q := `SELECT ` + shipmentColumns + ` FROM shipments WHERE order_id = $1`
	s, err := hydrateShipment(r.db.QueryRowContext(ctx, q, orderID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("shipping_repo: find by order id: %w", err)
	}
	return s, nil
}

// Create persists a new shipment.
func (r *ShippingRepo) Create(ctx context.Context, s *shipping.Shipment) error {
	if s == nil {
		return fmt.Errorf("shipping_repo: create: shipment must not be nil")
	}
	const q = `INSERT INTO shipments (id, order_id, method, status, cost, currency, tracking_number, provider_ref, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	var track sql.NullString
	if s.TrackingNumber != "" {
		track = sql.NullString{String: s.TrackingNumber, Valid: true}
	}
	var ref sql.NullString
	if s.ProviderRef != "" {
		ref = sql.NullString{String: s.ProviderRef, Valid: true}
	}
	_, err := r.db.ExecContext(ctx, q,
		s.ID, s.OrderID, string(s.Method), string(s.Status()),
		s.Cost.Amount(), s.Cost.Currency(), track, ref,
		s.CreatedAt, s.UpdatedAt,
	)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return apperror.Conflict("shipment for this order already exists")
		}
		return fmt.Errorf("shipping_repo: create: %w", err)
	}
	return nil
}

// UpdateStatus updates the status, tracking_number, provider_ref, and updated_at of a shipment.
// Uses optimistic locking via updated_at to detect concurrent modifications.
func (r *ShippingRepo) UpdateStatus(ctx context.Context, s *shipping.Shipment, prevUpdatedAt time.Time) error {
	if s == nil {
		return fmt.Errorf("shipping_repo: update status: shipment must not be nil")
	}
	const q = `UPDATE shipments SET status = $1, tracking_number = $2, provider_ref = $3, updated_at = $4 WHERE id = $5 AND updated_at = $6`
	var track sql.NullString
	if s.TrackingNumber != "" {
		track = sql.NullString{String: s.TrackingNumber, Valid: true}
	}
	var ref sql.NullString
	if s.ProviderRef != "" {
		ref = sql.NullString{String: s.ProviderRef, Valid: true}
	}
	res, err := r.db.ExecContext(ctx, q, string(s.Status()), track, ref, s.UpdatedAt, s.ID, prevUpdatedAt)
	if err != nil {
		return fmt.Errorf("shipping_repo: update status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("shipping_repo: update status rows: %w", err)
	}
	if n == 0 {
		return apperror.Conflict("shipment was modified concurrently")
	}
	return nil
}
