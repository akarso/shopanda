package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/invoice"
	"github.com/akarso/shopanda/internal/domain/shared"
)

// Compile-time check that CreditNoteRepo implements invoice.CreditNoteRepository.
var _ invoice.CreditNoteRepository = (*CreditNoteRepo)(nil)

// CreditNoteRepo implements invoice.CreditNoteRepository using PostgreSQL.
type CreditNoteRepo struct {
	db *sql.DB
}

// NewCreditNoteRepo returns a new CreditNoteRepo backed by db.
func NewCreditNoteRepo(db *sql.DB) (*CreditNoteRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewCreditNoteRepo: nil *sql.DB")
	}
	return &CreditNoteRepo{db: db}, nil
}

// creditNoteScanner abstracts *sql.Row and *sql.Rows for shared hydration.
type creditNoteScanner interface {
	Scan(dest ...interface{}) error
}

const creditNoteCols = `id, credit_note_number, invoice_id, order_id, customer_id,
	reason, currency, subtotal_amount, tax_amount, total_amount, created_at`

// hydrateCreditNote reads a credit note header from any scanner.
func hydrateCreditNote(s creditNoteScanner) (*invoice.CreditNote, error) {
	var cn invoice.CreditNote
	var subtotalAmount, taxAmount, totalAmount int64
	err := s.Scan(
		&cn.ID, &cn.CreditNoteNumber, &cn.InvoiceID, &cn.OrderID, &cn.CustomerID,
		&cn.Reason, &cn.Currency,
		&subtotalAmount, &taxAmount, &totalAmount,
		&cn.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	sub, err := shared.NewMoney(subtotalAmount, cn.Currency)
	if err != nil {
		return nil, fmt.Errorf("credit_note_repo: subtotal money: %w", err)
	}
	tax, err := shared.NewMoney(taxAmount, cn.Currency)
	if err != nil {
		return nil, fmt.Errorf("credit_note_repo: tax money: %w", err)
	}
	tot, err := shared.NewMoney(totalAmount, cn.Currency)
	if err != nil {
		return nil, fmt.Errorf("credit_note_repo: total money: %w", err)
	}
	cn.SubtotalAmount = sub
	cn.TaxAmount = tax
	cn.TotalAmount = tot
	return &cn, nil
}

// FindByID returns a credit note with its items by ID.
// Returns (nil, nil) when not found.
func (r *CreditNoteRepo) FindByID(ctx context.Context, id string) (*invoice.CreditNote, error) {
	if id == "" {
		return nil, fmt.Errorf("credit_note_repo: find: empty id")
	}
	q := `SELECT ` + creditNoteCols + ` FROM credit_notes WHERE id = $1`
	cn, err := hydrateCreditNote(r.db.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("credit_note_repo: find by id: %w", err)
	}
	items, err := r.loadItems(ctx, cn.ID)
	if err != nil {
		return nil, err
	}
	if err := cn.SetItemsFromDB(items); err != nil {
		return nil, fmt.Errorf("credit_note_repo: set items: %w", err)
	}
	return cn, nil
}

// FindByInvoiceID returns all credit notes for an invoice, newest first.
func (r *CreditNoteRepo) FindByInvoiceID(ctx context.Context, invoiceID string) ([]invoice.CreditNote, error) {
	if invoiceID == "" {
		return nil, fmt.Errorf("credit_note_repo: find by invoice: empty invoice id")
	}
	q := `SELECT ` + creditNoteCols + ` FROM credit_notes WHERE invoice_id = $1
		ORDER BY created_at DESC`
	rows, err := r.db.QueryContext(ctx, q, invoiceID)
	if err != nil {
		return nil, fmt.Errorf("credit_note_repo: find by invoice id: %w", err)
	}
	defer rows.Close()

	var notes []invoice.CreditNote
	var ids []string
	for rows.Next() {
		cn, err := hydrateCreditNote(rows)
		if err != nil {
			return nil, fmt.Errorf("credit_note_repo: scan: %w", err)
		}
		notes = append(notes, *cn)
		ids = append(ids, cn.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("credit_note_repo: rows: %w", err)
	}
	if len(notes) == 0 {
		return notes, nil
	}

	for i := range notes {
		items, err := r.loadItems(ctx, notes[i].ID)
		if err != nil {
			return nil, err
		}
		if err := notes[i].SetItemsFromDB(items); err != nil {
			return nil, fmt.Errorf("credit_note_repo: set items: %w", err)
		}
	}
	return notes, nil
}

// Save persists a credit note and its items (insert-only).
// Assigns CreditNoteNumber from the DB sequence.
func (r *CreditNoteRepo) Save(ctx context.Context, cn *invoice.CreditNote) error {
	if cn == nil {
		return fmt.Errorf("credit_note_repo: save: credit note must not be nil")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("credit_note_repo: begin tx: %w", err)
	}
	defer tx.Rollback()

	const insertCN = `INSERT INTO credit_notes
		(id, invoice_id, order_id, customer_id, reason, currency,
		 subtotal_amount, tax_amount, total_amount, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING credit_note_number`
	var cnNumber int64
	err = tx.QueryRowContext(ctx, insertCN,
		cn.ID, cn.InvoiceID, cn.OrderID, cn.CustomerID,
		cn.Reason, cn.Currency,
		cn.SubtotalAmount.Amount(), cn.TaxAmount.Amount(), cn.TotalAmount.Amount(),
		cn.CreatedAt,
	).Scan(&cnNumber)
	if err != nil {
		return fmt.Errorf("credit_note_repo: insert credit note: %w", err)
	}

	items := cn.Items()
	if len(items) > 0 {
		const insertItem = `INSERT INTO credit_note_items
			(credit_note_id, variant_id, sku, name, quantity, unit_price, currency)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`
		stmt, err := tx.PrepareContext(ctx, insertItem)
		if err != nil {
			return fmt.Errorf("credit_note_repo: prepare item insert: %w", err)
		}
		defer stmt.Close()
		for i := range items {
			it := &items[i]
			_, err = stmt.ExecContext(ctx,
				cn.ID, it.VariantID, it.SKU, it.Name, it.Quantity,
				it.UnitPrice.Amount(), it.UnitPrice.Currency(),
			)
			if err != nil {
				return fmt.Errorf("credit_note_repo: insert item %q: %w", it.VariantID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("credit_note_repo: commit: %w", err)
	}
	cn.CreditNoteNumber = cnNumber
	return nil
}

// loadItems fetches all items for a credit note.
func (r *CreditNoteRepo) loadItems(ctx context.Context, creditNoteID string) ([]invoice.Item, error) {
	const q = `SELECT variant_id, sku, name, quantity, unit_price, currency
		FROM credit_note_items WHERE credit_note_id = $1`
	rows, err := r.db.QueryContext(ctx, q, creditNoteID)
	if err != nil {
		return nil, fmt.Errorf("credit_note_repo: load items: %w", err)
	}
	defer rows.Close()
	return scanCreditNoteItems(rows)
}

// scanCreditNoteItems reads item rows into a slice.
func scanCreditNoteItems(rows *sql.Rows) ([]invoice.Item, error) {
	var items []invoice.Item
	for rows.Next() {
		var variantID, sku, name string
		var quantity int
		var priceAmount int64
		var priceCurrency string
		if err := rows.Scan(&variantID, &sku, &name, &quantity, &priceAmount, &priceCurrency); err != nil {
			return nil, fmt.Errorf("credit_note_repo: scan item: %w", err)
		}
		unitPrice, err := shared.NewMoney(priceAmount, priceCurrency)
		if err != nil {
			return nil, fmt.Errorf("credit_note_repo: item money: %w", err)
		}
		items = append(items, invoice.Item{
			VariantID: variantID,
			SKU:       sku,
			Name:      name,
			Quantity:  quantity,
			UnitPrice: unitPrice,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("credit_note_repo: item rows: %w", err)
	}
	return items, nil
}
