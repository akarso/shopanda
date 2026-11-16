package extref_test

import (
	"testing"

	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/pkg/pluginsdk/harness"
	"github.com/akarso/shopanda/plugins/extref"
)

func TestPlugin_AdaptInit(t *testing.T) {
	res, err := harness.AdaptInit(extref.New(), harness.Options{
		Satisfy: []pluginsdk.Capability{pluginsdk.CapCart},
	})
	if err != nil {
		t.Fatalf("AdaptInit: %v", err)
	}
	if res.Host == nil {
		t.Fatal("expected Host")
	}
}
