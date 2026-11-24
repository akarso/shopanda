package extension_test

import (
	"context"
	"testing"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
)

func TestPublicOrderItemAPIByVariant(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code:        "acme.gift.message",
		Label:       "Gift",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetOrderItem,
		StorageMode: domainext.StorageSnapshot,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := reg.Register(domainext.FieldDef{
		Code:        "acme.secret",
		Label:       "Secret",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetOrderItem,
		StorageMode: domainext.StorageSnapshot,
		Visibility:  domainext.VisibilityPrivate,
	}); err != nil {
		t.Fatalf("register private: %v", err)
	}
	repo := &orderItemPublicMemRepo{values: map[string]domainext.Value{}}
	msg := "hi"
	secret := "nope"
	target := domainext.OrderItemTarget("o1", "v1")
	repo.values[string(target.Type)+":"+target.ID+":acme.gift.message"] = domainext.Value{
		FieldCode:  "acme.gift.message",
		TargetType: target.Type,
		TargetID:   target.ID,
		Payload:    domainext.ValuePayload{StringValue: &msg},
	}
	repo.values[string(target.Type)+":"+target.ID+":acme.secret"] = domainext.Value{
		FieldCode:  "acme.secret",
		TargetType: target.Type,
		TargetID:   target.ID,
		Payload:    domainext.ValuePayload{StringValue: &secret},
	}
	api := extensionapp.NewValueService(reg, repo)

	byVariant, err := extensionapp.PublicOrderItemAPIByVariant(context.Background(), api, "o1", []string{"v1"}, nil)
	if err != nil {
		t.Fatalf("PublicOrderItemAPIByVariant: %v", err)
	}
	items := byVariant["v1"]
	if len(items) != 1 || items[0].FieldCode != "acme.gift.message" || items[0].Value != "hi" {
		t.Fatalf("items = %+v (private must be omitted)", items)
	}
	m := extensionapp.ExtensionMapFromAPIItems(items)
	if m["acme.gift.message"] != "hi" {
		t.Fatalf("map = %+v", m)
	}
	rows := extensionapp.ExtensionDisplayRows(reg, items)
	if len(rows) != 1 || rows[0].Label != "Gift" || rows[0].Value != "hi" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestPublicOrderItemAPIByVariant_NilAPI(t *testing.T) {
	got, err := extensionapp.PublicOrderItemAPIByVariant(context.Background(), nil, "o1", []string{"v1"}, nil)
	if err != nil || got != nil {
		t.Fatalf("nil api = %v err=%v", got, err)
	}
}

func TestFormatExtensionDisplayValue(t *testing.T) {
	if _, ok := extensionapp.FormatExtensionDisplayValue("string", nil); ok {
		t.Fatal("nil must be skipped")
	}
	if got, ok := extensionapp.FormatExtensionDisplayValue("bool", true); !ok || got != "Yes" {
		t.Fatalf("bool true = %q ok=%v", got, ok)
	}
	if _, ok := extensionapp.FormatExtensionDisplayValue("json", map[string]interface{}{"a": 1}); ok {
		t.Fatal("json object must be skipped")
	}
}

type orderItemPublicMemRepo struct {
	values map[string]domainext.Value
}

func (m *orderItemPublicMemRepo) ListByTarget(_ context.Context, target domainext.Target) ([]domainext.Value, error) {
	var out []domainext.Value
	for _, v := range m.values {
		if v.TargetType == target.Type && v.TargetID == target.ID {
			out = append(out, v)
		}
	}
	return out, nil
}
func (m *orderItemPublicMemRepo) ListByTargets(_ context.Context, targetType domainext.TargetType, targetIDs []string) ([]domainext.Value, error) {
	want := map[string]struct{}{}
	for _, id := range targetIDs {
		want[id] = struct{}{}
	}
	var out []domainext.Value
	for _, v := range m.values {
		if v.TargetType == targetType {
			if _, ok := want[v.TargetID]; ok {
				out = append(out, v)
			}
		}
	}
	return out, nil
}
func (m *orderItemPublicMemRepo) Upsert(_ context.Context, value domainext.Value) error {
	m.values[string(value.TargetType)+":"+value.TargetID+":"+value.FieldCode] = value
	return nil
}
func (m *orderItemPublicMemRepo) UpsertBatch(ctx context.Context, values []domainext.Value) error {
	for _, v := range values {
		if err := m.Upsert(ctx, v); err != nil {
			return err
		}
	}
	return nil
}
func (m *orderItemPublicMemRepo) UpsertBatchSelective(ctx context.Context, values []domainext.Value, _ map[string]struct{}) error {
	return m.UpsertBatch(ctx, values)
}
func (m *orderItemPublicMemRepo) ListTargetIDsByTypeAndIDPrefix(context.Context, domainext.TargetType, string) ([]string, error) {
	return nil, nil
}
func (m *orderItemPublicMemRepo) Delete(_ context.Context, target domainext.Target, fieldCode string) error {
	delete(m.values, string(target.Type)+":"+target.ID+":"+fieldCode)
	return nil
}
