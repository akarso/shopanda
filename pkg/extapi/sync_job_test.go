package extapi

import (
	"testing"
)

func TestSyncJobType(t *testing.T) {
	got, err := SyncJobType("acme", "warehouse.stock")
	if err != nil {
		t.Fatalf("SyncJobType: %v", err)
	}
	want := "integration.sync.acme.warehouse.stock"
	if got != want {
		t.Fatalf("SyncJobType() = %q, want %q", got, want)
	}
}

func TestOnEvent(t *testing.T) {
	tr := OnEvent("catalog.product.updated")
	if tr.Kind != SyncTriggerEvent || tr.EventName != "catalog.product.updated" {
		t.Fatalf("OnEvent() = %+v", tr)
	}
}

func TestCron(t *testing.T) {
	tr := Cron("@every 5m")
	if tr.Kind != SyncTriggerCron || tr.CronSpec != "@every 5m" {
		t.Fatalf("Cron() = %+v", tr)
	}
}
