package extension_test

import (
	"reflect"
	"sort"
	"testing"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
)

func methodNames(t reflect.Type) []string {
	var out []string
	for i := 0; i < t.NumMethod(); i++ {
		out = append(out, t.Method(i).Name)
	}
	sort.Strings(out)
	return out
}

// Request-facing handlers (HTTP, admin, GraphQL) depend on ValueAPI: only gated
// methods. Adding an ungated method here must be a deliberate, reviewed change.
func TestValueAPI_ExposesOnlyGatedMethods(t *testing.T) {
	want := []string{"Delete", "List", "ListByOrderItems", "Registry", "UpsertBatch"}
	got := methodNames(reflect.TypeOf((*extensionapp.ValueAPI)(nil)).Elem())
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ValueAPI methods = %v, want %v", got, want)
	}
}

// Core services depend on CoreValueOps: the intentionally ungated operations.
func TestCoreValueOps_IsTheUngatedSet(t *testing.T) {
	want := []string{"CopyTarget", "DeleteAllForTarget", "DeleteOrphanCartItemExtensions", "SnapshotCartItemToOrderItem"}
	got := methodNames(reflect.TypeOf((*extensionapp.CoreValueOps)(nil)).Elem())
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CoreValueOps methods = %v, want %v", got, want)
	}
}

func TestValueService_ImplementsBothPorts(t *testing.T) {
	var _ extensionapp.ValueAPI = (*extensionapp.ValueService)(nil)
	var _ extensionapp.CoreValueOps = (*extensionapp.ValueService)(nil)
}
