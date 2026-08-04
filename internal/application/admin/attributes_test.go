package admin_test

import (
	"context"
	"fmt"
	"testing"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/config"
)

type mockConfigRepo struct {
	store map[string]interface{}
}

func newMockConfigRepo() *mockConfigRepo {
	return &mockConfigRepo{store: make(map[string]interface{})}
}

func (m *mockConfigRepo) Get(_ context.Context, key string) (interface{}, error) {
	return m.store[key], nil
}

func (m *mockConfigRepo) Set(_ context.Context, key string, value interface{}) error {
	m.store[key] = value
	return nil
}

func (m *mockConfigRepo) SetMany(_ context.Context, entries map[string]interface{}) error {
	for key, value := range entries {
		m.store[key] = value
	}
	return nil
}

func (m *mockConfigRepo) Delete(_ context.Context, key string) error {
	delete(m.store, key)
	return nil
}

func (m *mockConfigRepo) All(_ context.Context) ([]config.Entry, error) {
	entries := make([]config.Entry, 0, len(m.store))
	for key, value := range m.store {
		entries = append(entries, config.Entry{Key: key, Value: value})
	}
	return entries, nil
}

func TestAttributeStore_CreateAndList(t *testing.T) {
	store := adminApp.NewAttributeStore(newMockConfigRepo())
	ctx := context.Background()

	if err := store.CreateAttribute(ctx, catalog.Attribute{
		Code:    "color",
		Label:   "Color",
		Type:    catalog.AttributeTypeSelect,
		Options: []string{"red", "blue"},
	}); err != nil {
		t.Fatalf("CreateAttribute: %v", err)
	}

	attrs, err := store.ListAttributes(ctx, "")
	if err != nil {
		t.Fatalf("ListAttributes: %v", err)
	}
	if len(attrs) != 1 || attrs[0].Code != "color" {
		t.Fatalf("attrs = %+v, want one color attribute", attrs)
	}
}

func TestAttributeStore_RejectsDuplicateCode(t *testing.T) {
	store := adminApp.NewAttributeStore(newMockConfigRepo())
	ctx := context.Background()

	attr := catalog.Attribute{Code: "weight", Label: "Weight", Type: catalog.AttributeTypeNumber}
	if err := store.CreateAttribute(ctx, attr); err != nil {
		t.Fatalf("CreateAttribute first: %v", err)
	}
	if err := store.CreateAttribute(ctx, attr); err == nil {
		t.Fatal("expected duplicate code error")
	}
}

func TestAttributeStore_DeleteRemovesFromGroups(t *testing.T) {
	repo := newMockConfigRepo()
	store := adminApp.NewAttributeStore(repo)
	ctx := context.Background()

	if err := store.CreateAttribute(ctx, catalog.Attribute{Code: "color", Label: "Color", Type: catalog.AttributeTypeText}); err != nil {
		t.Fatalf("CreateAttribute: %v", err)
	}
	if err := store.CreateGroup(ctx, catalog.AttributeGroup{
		Code: "apparel", Label: "Apparel", Attributes: []string{"color"},
	}); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := store.DeleteAttribute(ctx, "color"); err != nil {
		t.Fatalf("DeleteAttribute: %v", err)
	}

	group, err := store.GetGroup(ctx, "apparel")
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if len(group.Attributes) != 0 {
		t.Fatalf("group attributes = %v, want empty", group.Attributes)
	}
}

type failSetManyConfigRepo struct {
	*mockConfigRepo
}

func (m *failSetManyConfigRepo) SetMany(_ context.Context, _ map[string]interface{}) error {
	return fmt.Errorf("persist failed")
}

func TestAttributeStore_DeleteKeepsStateOnPersistFailure(t *testing.T) {
	repo := &failSetManyConfigRepo{mockConfigRepo: newMockConfigRepo()}
	store := adminApp.NewAttributeStore(repo)
	ctx := context.Background()

	if err := store.CreateAttribute(ctx, catalog.Attribute{Code: "color", Label: "Color", Type: catalog.AttributeTypeText}); err != nil {
		t.Fatalf("CreateAttribute: %v", err)
	}
	if err := store.CreateGroup(ctx, catalog.AttributeGroup{
		Code: "apparel", Label: "Apparel", Attributes: []string{"color"},
	}); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	if err := store.DeleteAttribute(ctx, "color"); err == nil {
		t.Fatal("expected delete persistence error")
	}

	if _, err := store.GetAttribute(ctx, "color"); err != nil {
		t.Fatalf("attribute should remain after failed delete: %v", err)
	}
	group, err := store.GetGroup(ctx, "apparel")
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if len(group.Attributes) != 1 || group.Attributes[0] != "color" {
		t.Fatalf("group attributes = %v, want [color]", group.Attributes)
	}
}

func TestAttributeStore_ListAttributesByGroup(t *testing.T) {
	store := adminApp.NewAttributeStore(newMockConfigRepo())
	ctx := context.Background()

	for _, attr := range []catalog.Attribute{
		{Code: "color", Label: "Color", Type: catalog.AttributeTypeText},
		{Code: "weight", Label: "Weight", Type: catalog.AttributeTypeNumber},
	} {
		if err := store.CreateAttribute(ctx, attr); err != nil {
			t.Fatalf("CreateAttribute %s: %v", attr.Code, err)
		}
	}
	if err := store.CreateGroup(ctx, catalog.AttributeGroup{
		Code: "physical", Label: "Physical", Attributes: []string{"weight"},
	}); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	filtered, err := store.ListAttributes(ctx, "physical")
	if err != nil {
		t.Fatalf("ListAttributes: %v", err)
	}
	if len(filtered) != 1 || filtered[0].Code != "weight" {
		t.Fatalf("filtered = %+v, want weight only", filtered)
	}
}

func TestAttributeStore_ListLayeredNavAttributes(t *testing.T) {
	ctx := context.Background()
	store := adminApp.NewAttributeStore(newMockConfigRepo())
	if err := store.CreateAttribute(ctx, catalog.Attribute{Code: "color", Label: "Color", Type: catalog.AttributeTypeText, UseInLayeredNav: true}); err != nil {
		t.Fatalf("CreateAttribute color: %v", err)
	}
	if err := store.CreateAttribute(ctx, catalog.Attribute{Code: "weight", Label: "Weight", Type: catalog.AttributeTypeNumber}); err != nil {
		t.Fatalf("CreateAttribute weight: %v", err)
	}

	attrs, err := store.ListLayeredNavAttributes(ctx)
	if err != nil {
		t.Fatalf("ListLayeredNavAttributes: %v", err)
	}
	if len(attrs) != 1 || attrs[0].Code != "color" {
		t.Fatalf("attrs = %+v, want color only", attrs)
	}
}

func TestAttributeToFormField(t *testing.T) {
	field, err := adminApp.AttributeToFormField(catalog.Attribute{
		Code: "size", Label: "Size", Type: catalog.AttributeTypeSelect,
		Required: true, Options: []string{"S", "M"},
	})
	if err != nil {
		t.Fatalf("AttributeToFormField: %v", err)
	}
	if field.Type != "select" || field.Name != "size" || !field.Required || len(field.Options) != 2 {
		t.Fatalf("field = %+v", field)
	}
}
