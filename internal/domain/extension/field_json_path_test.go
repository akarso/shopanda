package extension

import (
	"encoding/json"
	"testing"
)

// The Postgres guard reads definition->'access'->>'PluginOnlyWrite'
// (internal/infrastructure/postgres/extension_field_repo.go). If these JSON keys
// change, the guard would silently stop protecting rows.
func TestExtensionField_JSONPathUsedByPostgresGuard(t *testing.T) {
	raw, err := json.Marshal(ExtensionField{Access: Access{PluginOnlyWrite: true}})
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Access map[string]interface{} `json:"access"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if got, ok := m.Access["PluginOnlyWrite"].(bool); !ok || !got {
		t.Fatalf("definition->'access'->>'PluginOnlyWrite' missing in %s", raw)
	}
}
