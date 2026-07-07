package graphql_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/application/admin"
	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/domain/rbac"
	cgraphql "github.com/akarso/shopanda/plugins/core/graphql"
)

type extFieldRepoStub struct{}

func (extFieldRepoStub) Create(context.Context, domainext.ExtensionField) error                { return nil }
func (extFieldRepoStub) Save(context.Context, domainext.ExtensionField) error                  { return nil }
func (extFieldRepoStub) FindByCode(context.Context, string) (domainext.ExtensionField, error) { return domainext.ExtensionField{}, nil }
func (extFieldRepoStub) ListActive(context.Context, domainext.TargetType) ([]domainext.ExtensionField, error) {
	return nil, nil
}
func (extFieldRepoStub) SoftDelete(context.Context, string) error { return nil }

type extValueRepoStub struct {
	values map[string]map[string]domainext.Value
}

func newExtValueRepoStub() *extValueRepoStub {
	return &extValueRepoStub{values: map[string]map[string]domainext.Value{}}
}

func targetKey(target domainext.Target) string {
	return string(target.Type) + ":" + target.ID
}

func (s *extValueRepoStub) ListByTarget(_ context.Context, target domainext.Target) ([]domainext.Value, error) {
	items := s.values[targetKey(target)]
	out := make([]domainext.Value, 0, len(items))
	for _, v := range items {
		out = append(out, v)
	}
	return out, nil
}

func (s *extValueRepoStub) ListByTargets(_ context.Context, targetType domainext.TargetType, targetIDs []string) ([]domainext.Value, error) {
	var out []domainext.Value
	for _, id := range targetIDs {
		for _, v := range s.values[targetKey(domainext.Target{Type: targetType, ID: id})] {
			out = append(out, v)
		}
	}
	return out, nil
}

func (s *extValueRepoStub) Upsert(_ context.Context, value domainext.Value) error {
	key := targetKey(domainext.Target{Type: value.TargetType, ID: value.TargetID})
	if s.values[key] == nil {
		s.values[key] = map[string]domainext.Value{}
	}
	s.values[key][value.FieldCode] = value
	return nil
}

func (s *extValueRepoStub) UpsertBatch(ctx context.Context, values []domainext.Value) error {
	for _, v := range values {
		if err := s.Upsert(ctx, v); err != nil {
			return err
		}
	}
	return nil
}

func (s *extValueRepoStub) Delete(_ context.Context, target domainext.Target, fieldCode string) error {
	key := targetKey(target)
	if s.values[key] != nil {
		delete(s.values[key], fieldCode)
	}
	return nil
}

func testResolverWithExtensions(t *testing.T) *cgraphql.Resolver {
	t.Helper()
	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code: "acme.public_note", Label: "Public note", Type: domainext.FieldTypeString, Scope: domainext.TargetProduct,
	}); err != nil {
		t.Fatalf("register public field: %v", err)
	}
	if err := reg.Register(domainext.FieldDef{
		Code: "acme.private_flag", Label: "Private flag", Type: domainext.FieldTypeBool, Scope: domainext.TargetProduct, Visibility: domainext.VisibilityPrivate,
	}); err != nil {
		t.Fatalf("register private field: %v", err)
	}
	fields := extensionapp.NewFieldService(reg, extFieldRepoStub{})
	repo := newExtValueRepoStub()
	values := extensionapp.NewValueService(reg, repo)
	_, err := values.UpsertBatch(context.Background(), domainext.Target{Type: domainext.TargetProduct, ID: "prod-1"}, []domainext.ValueInput{
		{FieldCode: "acme.public_note", Value: "hello"},
		{FieldCode: "acme.private_flag", Value: true},
	}, "seed", true)
	if err != nil {
		t.Fatalf("seed values: %v", err)
	}

	resolver, err := cgraphql.NewResolver(&stubProductRepo{}, &stubCategoryRepo{})
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	return resolver.WithExtensions(fields, values)
}

func TestHandler_ExtensionValuesPublicRoundTrip(t *testing.T) {
	schema, err := cgraphql.NewSchema(testResolverWithExtensions(t))
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}
	h := cgraphql.NewHandler(schema, testLogger())
	body := `{"query":"{ extensionValues(targetType:\"product\", targetId:\"prod-1\") { fieldCode value } }"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", bytes.NewBufferString(body))
	req = req.WithContext((&admin.AdminContext{
		AdminID: "admin-1", Permissions: []string{string(rbac.ExtensionsRead)},
	}).WithContext(req.Context()))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"fieldCode":"acme.public_note"`)) {
		t.Fatalf("missing public extension value: %s", rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte(`"fieldCode":"acme.private_flag"`)) {
		t.Fatalf("private value should be hidden without private permission: %s", rec.Body.String())
	}
}

func TestHandler_ExtensionFieldsIncludePrivateRequiresPermission(t *testing.T) {
	schema, err := cgraphql.NewSchema(testResolverWithExtensions(t))
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}
	h := cgraphql.NewHandler(schema, testLogger())
	body := `{"query":"{ extensionFields(scope:\"product\", includePrivate:true) { code visibility } }"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", bytes.NewBufferString(body))
	req = req.WithContext((&admin.AdminContext{
		AdminID: "admin-1", Permissions: []string{string(rbac.ExtensionsRead)},
	}).WithContext(req.Context()))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte(`acme.private_flag`)) {
		t.Fatalf("private field should be omitted without extensions.private.read: %s", rec.Body.String())
	}
}

func TestHandler_ExtensionMutationForbiddenWithoutCapability(t *testing.T) {
	schema, err := cgraphql.NewSchema(testResolverWithExtensions(t))
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}
	h := cgraphql.NewHandler(schema, testLogger())
	body := `{"query":"mutation($values:[ExtensionValueInput!]!){ upsertExtensionValues(targetType:\"product\", targetId:\"prod-1\", values:$values) { fieldCode } }","variables":{"values":[{"fieldCode":"acme.public_note","value":"next"}]}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", bytes.NewBufferString(body))
	req = req.WithContext((&admin.AdminContext{
		AdminID: "admin-1", Permissions: []string{string(rbac.ExtensionsRead)},
	}).WithContext(req.Context()))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Errors) == 0 || resp.Errors[0].Message != "insufficient permissions" {
		t.Fatalf("errors = %+v", resp.Errors)
	}
}
