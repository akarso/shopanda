package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
)

func mustNewOrder(t *testing.T, customerID, currency string) order.Order {
	t.Helper()
	price := shared.MustNewMoney(1000, currency)
	item, err := order.NewItem("var-1", "SKU-001", "Test Product", 2, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder(id.New(), customerID, "", currency, []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	return o
}

func mustNewGuestOrder(t *testing.T, contactEmail, currency string) order.Order {
	t.Helper()
	price := shared.MustNewMoney(1000, currency)
	item, err := order.NewItem("var-1", "SKU-001", "Test Product", 2, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder(id.New(), "", contactEmail, currency, []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	return o
}

func mustNewAuthenticatedOrderWithEmail(t *testing.T, customerID, contactEmail, currency string) order.Order {
	t.Helper()
	price := shared.MustNewMoney(1000, currency)
	item, err := order.NewItem("var-1", "SKU-001", "Test Product", 2, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder(id.New(), customerID, contactEmail, currency, []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	return o
}

func TestOrderRepo_SaveAndFindByID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM order_items")
		db.Exec("DELETE FROM orders")
	})

	repo, err := postgres.NewOrderRepo(db)
	if err != nil {
		t.Fatalf("NewOrderRepo: %v", err)
	}
	ctx := context.Background()

	o := mustNewOrder(t, "cust-1", "EUR")
	if err := repo.Save(ctx, &o); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.FindByID(ctx, o.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByID returned nil")
	}
	if got.ID != o.ID {
		t.Errorf("ID = %q, want %q", got.ID, o.ID)
	}
	if got.CustomerID != "cust-1" {
		t.Errorf("CustomerID = %q, want cust-1", got.CustomerID)
	}
	if got.Status() != order.OrderStatusPending {
		t.Errorf("Status = %q, want pending", got.Status())
	}
	if got.Currency != "EUR" {
		t.Errorf("Currency = %q, want EUR", got.Currency)
	}
	if got.TotalAmount.Amount() != 2000 {
		t.Errorf("TotalAmount = %d, want 2000", got.TotalAmount.Amount())
	}
	gotItems := got.Items()
	if len(gotItems) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(gotItems))
	}
	item := gotItems[0]
	if item.VariantID != "var-1" {
		t.Errorf("VariantID = %q, want var-1", item.VariantID)
	}
	if item.SKU != "SKU-001" {
		t.Errorf("SKU = %q, want SKU-001", item.SKU)
	}
	if item.Name != "Test Product" {
		t.Errorf("Name = %q, want Test Product", item.Name)
	}
	if item.Quantity != 2 {
		t.Errorf("Quantity = %d, want 2", item.Quantity)
	}
	if item.UnitPrice.Amount() != 1000 {
		t.Errorf("UnitPrice = %d, want 1000", item.UnitPrice.Amount())
	}
}

func TestOrderRepo_FindByID_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewOrderRepo(db)
	if err != nil {
		t.Fatalf("NewOrderRepo: %v", err)
	}
	got, err := repo.FindByID(context.Background(), id.New())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got != nil {
		t.Error("expected nil for non-existent order")
	}
}

func TestOrderRepo_FindByCustomerID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM order_items")
		db.Exec("DELETE FROM orders")
	})

	repo, err := postgres.NewOrderRepo(db)
	if err != nil {
		t.Fatalf("NewOrderRepo: %v", err)
	}
	ctx := context.Background()

	custID := "cust-" + id.New()[:8]

	o1 := mustNewOrder(t, custID, "EUR")
	if err := repo.Save(ctx, &o1); err != nil {
		t.Fatalf("Save o1: %v", err)
	}
	o2 := mustNewOrder(t, custID, "EUR")
	if err := repo.Save(ctx, &o2); err != nil {
		t.Fatalf("Save o2: %v", err)
	}

	// Different customer — should not appear.
	o3 := mustNewOrder(t, "other-cust", "EUR")
	if err := repo.Save(ctx, &o3); err != nil {
		t.Fatalf("Save o3: %v", err)
	}

	orders, err := repo.FindByCustomerID(ctx, custID)
	if err != nil {
		t.Fatalf("FindByCustomerID: %v", err)
	}
	if len(orders) != 2 {
		t.Fatalf("len(orders) = %d, want 2", len(orders))
	}
	for i, o := range orders {
		if o.CustomerID != custID {
			t.Errorf("orders[%d].CustomerID = %q, want %q", i, o.CustomerID, custID)
		}
	}
	// Newest first: o2 was saved after o1.
	if orders[0].ID != o2.ID {
		t.Errorf("orders[0].ID = %q, want %q (newest first)", orders[0].ID, o2.ID)
	}
	if orders[1].ID != o1.ID {
		t.Errorf("orders[1].ID = %q, want %q", orders[1].ID, o1.ID)
	}
}

func TestOrderRepo_FindByCustomerID_Empty(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewOrderRepo(db)
	if err != nil {
		t.Fatalf("NewOrderRepo: %v", err)
	}
	orders, err := repo.FindByCustomerID(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("FindByCustomerID: %v", err)
	}
	if len(orders) != 0 {
		t.Errorf("len(orders) = %d, want 0", len(orders))
	}
}

func TestOrderRepo_FindByContactEmail(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM order_items")
		db.Exec("DELETE FROM orders")
	})

	repo, err := postgres.NewOrderRepo(db)
	if err != nil {
		t.Fatalf("NewOrderRepo: %v", err)
	}
	ctx := context.Background()

	contactEmail := "guest@example.com"

	// Create guest orders with the same contact email
	o1 := mustNewGuestOrder(t, contactEmail, "EUR")
	if err := repo.Save(ctx, &o1); err != nil {
		t.Fatalf("Save o1: %v", err)
	}
	o2 := mustNewGuestOrder(t, contactEmail, "EUR")
	if err := repo.Save(ctx, &o2); err != nil {
		t.Fatalf("Save o2: %v", err)
	}

	// Different email — should not appear
	o3 := mustNewGuestOrder(t, "other@example.com", "EUR")
	if err := repo.Save(ctx, &o3); err != nil {
		t.Fatalf("Save o3: %v", err)
	}

	// Authenticated order with contact email — should not appear (guest-only lookup)
	o4 := mustNewAuthenticatedOrderWithEmail(t, "cust-1", contactEmail, "EUR")
	if err := repo.Save(ctx, &o4); err != nil {
		t.Fatalf("Save o4: %v", err)
	}

	orders, err := repo.FindByContactEmail(ctx, contactEmail)
	if err != nil {
		t.Fatalf("FindByContactEmail: %v", err)
	}
	if len(orders) != 2 {
		t.Fatalf("len(orders) = %d, want 2 (2 guest orders only, authenticated excluded)", len(orders))
	}
	for i, o := range orders {
		if o.ContactEmail != contactEmail {
			t.Errorf("orders[%d].ContactEmail = %q, want %q", i, o.ContactEmail, contactEmail)
		}
		if o.CustomerID != "" {
			t.Errorf("orders[%d].CustomerID = %q, want empty (guest-only)", i, o.CustomerID)
		}
	}
	// Newest first: o2 was saved after o1
	if orders[0].ID != o2.ID {
		t.Errorf("orders[0].ID = %q, want %q (newest first)", orders[0].ID, o2.ID)
	}
	if orders[1].ID != o1.ID {
		t.Errorf("orders[1].ID = %q, want %q", orders[1].ID, o1.ID)
	}
}

func TestOrderRepo_FindByContactEmail_Empty(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewOrderRepo(db)
	if err != nil {
		t.Fatalf("NewOrderRepo: %v", err)
	}
	orders, err := repo.FindByContactEmail(context.Background(), "nonexistent@example.com")
	if err != nil {
		t.Fatalf("FindByContactEmail: %v", err)
	}
	if len(orders) != 0 {
		t.Errorf("len(orders) = %d, want 0", len(orders))
	}
}

func TestOrderRepo_FindByContactEmail_CaseInsensitive(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM order_items")
		db.Exec("DELETE FROM orders")
	})

	repo, err := postgres.NewOrderRepo(db)
	if err != nil {
		t.Fatalf("NewOrderRepo: %v", err)
	}
	ctx := context.Background()

	// Save with lowercase
	o := mustNewGuestOrder(t, "guest@example.com", "EUR")
	if err := repo.Save(ctx, &o); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Query with uppercase
	orders, err := repo.FindByContactEmail(ctx, "GUEST@EXAMPLE.COM")
	if err != nil {
		t.Fatalf("FindByContactEmail: %v", err)
	}
	if len(orders) != 1 {
		t.Errorf("len(orders) = %d, want 1 (case-insensitive match)", len(orders))
	}
}

func TestOrderRepo_UpdateStatus(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM order_items")
		db.Exec("DELETE FROM orders")
	})

	repo, err := postgres.NewOrderRepo(db)
	if err != nil {
		t.Fatalf("NewOrderRepo: %v", err)
	}
	ctx := context.Background()

	o := mustNewOrder(t, "cust-1", "EUR")
	if err := repo.Save(ctx, &o); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := o.Confirm(); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if err := repo.UpdateStatus(ctx, &o); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	got, err := repo.FindByID(ctx, o.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Status() != order.OrderStatusConfirmed {
		t.Errorf("Status = %q, want confirmed", got.Status())
	}
}

func TestOrderRepo_UpdateStatus_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewOrderRepo(db)
	if err != nil {
		t.Fatalf("NewOrderRepo: %v", err)
	}
	o := mustNewOrder(t, "cust-1", "EUR")
	// Never saved — should fail.
	if err := repo.UpdateStatus(context.Background(), &o); err == nil {
		t.Fatal("expected error for non-existent order")
	}
}

func TestOrderRepo_LinkToCustomer(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM order_items")
		db.Exec("DELETE FROM orders")
	})

	repo, err := postgres.NewOrderRepo(db)
	if err != nil {
		t.Fatalf("NewOrderRepo: %v", err)
	}
	ctx := context.Background()

	o := mustNewGuestOrder(t, "guest@example.com", "EUR")
	if err := repo.Save(ctx, &o); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := o.LinkToCustomer("cust-77"); err != nil {
		t.Fatalf("domain LinkToCustomer: %v", err)
	}
	if err := repo.LinkToCustomer(ctx, &o); err != nil {
		t.Fatalf("repo LinkToCustomer: %v", err)
	}

	got, err := repo.FindByID(ctx, o.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByID returned nil after link")
	}
	if got.CustomerID != "cust-77" {
		t.Errorf("CustomerID = %q, want cust-77", got.CustomerID)
	}

	// Linked order is no longer discoverable as a guest order.
	guests, err := repo.FindByContactEmail(ctx, "guest@example.com")
	if err != nil {
		t.Fatalf("FindByContactEmail: %v", err)
	}
	if len(guests) != 0 {
		t.Errorf("FindByContactEmail returned %d orders after link, want 0", len(guests))
	}

	// Linked orders show up in the customer's order history.
	history, err := repo.FindByCustomerID(ctx, "cust-77")
	if err != nil {
		t.Fatalf("FindByCustomerID: %v", err)
	}
	if len(history) != 1 || history[0].ID != o.ID {
		t.Errorf("FindByCustomerID = %v, want the linked order", history)
	}
}

func TestOrderRepo_LinkToCustomer_AlreadyLinked(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM order_items")
		db.Exec("DELETE FROM orders")
	})

	repo, err := postgres.NewOrderRepo(db)
	if err != nil {
		t.Fatalf("NewOrderRepo: %v", err)
	}
	ctx := context.Background()

	o := mustNewOrder(t, "cust-1", "EUR")
	if err := repo.Save(ctx, &o); err != nil {
		t.Fatalf("Save: %v", err)
	}

	relink := o
	relink.CustomerID = "cust-2"
	if err := repo.LinkToCustomer(ctx, &relink); err == nil {
		t.Fatal("expected error when relinking an already-linked order")
	}

	got, err := repo.FindByID(ctx, o.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.CustomerID != "cust-1" {
		t.Errorf("CustomerID = %q, want cust-1 (unchanged)", got.CustomerID)
	}
}

func TestOrderRepo_LinkToCustomerByContactEmail(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM order_items")
		db.Exec("DELETE FROM orders")
	})

	repo, err := postgres.NewOrderRepo(db)
	if err != nil {
		t.Fatalf("NewOrderRepo: %v", err)
	}
	ctx := context.Background()

	o1 := mustNewGuestOrder(t, "guest@example.com", "EUR")
	o2 := mustNewGuestOrder(t, "Guest@Example.com", "EUR")
	other := mustNewGuestOrder(t, "other@example.com", "EUR")
	linkedAlready := mustNewAuthenticatedOrderWithEmail(t, "cust-1", "guest@example.com", "EUR")
	for _, o := range []*order.Order{&o1, &o2, &other, &linkedAlready} {
		if err := repo.Save(ctx, o); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	linked, err := repo.LinkToCustomerByContactEmail(ctx, "guest@example.com", "cust-77", o1.UpdatedAt)
	if err != nil {
		t.Fatalf("LinkToCustomerByContactEmail: %v", err)
	}
	if linked != 2 {
		t.Errorf("linked = %d, want 2 (case-insensitive match, guest orders only)", linked)
	}

	history, err := repo.FindByCustomerID(ctx, "cust-77")
	if err != nil {
		t.Fatalf("FindByCustomerID: %v", err)
	}
	if len(history) != 2 {
		t.Errorf("FindByCustomerID returned %d orders, want 2", len(history))
	}

	untouched, err := repo.FindByID(ctx, other.ID)
	if err != nil || untouched == nil {
		t.Fatalf("FindByID(other): %v, %v", untouched, err)
	}
	if untouched.CustomerID != "" {
		t.Errorf("unrelated guest order was linked to %q", untouched.CustomerID)
	}
	stillOwned, err := repo.FindByID(ctx, linkedAlready.ID)
	if err != nil || stillOwned == nil {
		t.Fatalf("FindByID(linkedAlready): %v, %v", stillOwned, err)
	}
	if stillOwned.CustomerID != "cust-1" {
		t.Errorf("already-linked order CustomerID = %q, want cust-1 (unchanged)", stillOwned.CustomerID)
	}

	// A second claim finds nothing left to link.
	again, err := repo.LinkToCustomerByContactEmail(ctx, "guest@example.com", "cust-88", o1.UpdatedAt)
	if err != nil {
		t.Fatalf("second LinkToCustomerByContactEmail: %v", err)
	}
	if again != 0 {
		t.Errorf("second claim linked = %d, want 0", again)
	}
}

func TestOrderRepo_LinkToCustomer_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewOrderRepo(db)
	if err != nil {
		t.Fatalf("NewOrderRepo: %v", err)
	}

	o := mustNewGuestOrder(t, "guest@example.com", "EUR")
	if err := o.LinkToCustomer("cust-77"); err != nil {
		t.Fatalf("domain LinkToCustomer: %v", err)
	}
	// Never saved — should fail.
	if err := repo.LinkToCustomer(context.Background(), &o); err == nil {
		t.Fatal("expected error for non-existent order")
	}
}

func TestOrderRepo_MultipleItems(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM order_items")
		db.Exec("DELETE FROM orders")
	})

	repo, err := postgres.NewOrderRepo(db)
	if err != nil {
		t.Fatalf("NewOrderRepo: %v", err)
	}
	ctx := context.Background()

	p1 := shared.MustNewMoney(1000, "EUR")
	p2 := shared.MustNewMoney(500, "EUR")
	i1, err := order.NewItem("var-1", "SKU-1", "Shirt", 2, p1)
	if err != nil {
		t.Fatalf("NewItem i1: %v", err)
	}
	i2, err := order.NewItem("var-2", "SKU-2", "Hat", 1, p2)
	if err != nil {
		t.Fatalf("NewItem i2: %v", err)
	}

	o, err := order.NewOrder(id.New(), "cust-1", "", "EUR", []order.Item{i1, i2})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	if err := repo.Save(ctx, &o); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.FindByID(ctx, o.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if len(got.Items()) != 2 {
		t.Fatalf("len(Items) = %d, want 2", len(got.Items()))
	}
	// 1000*2 + 500*1 = 2500
	if got.TotalAmount.Amount() != 2500 {
		t.Errorf("TotalAmount = %d, want 2500", got.TotalAmount.Amount())
	}
}
