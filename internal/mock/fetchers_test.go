package mock

import (
	"testing"

	"github.com/paramify/evidence-tui-prototype/internal/catalog"
)

// TestBehaviorOverridesPointAtKnownIDs asserts override map keys exist in the embedded catalog.
func TestBehaviorOverridesPointAtKnownIDs(t *testing.T) {
	_, scripts, err := catalog.LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	known := map[string]bool{}
	for _, s := range scripts {
		known[s.ID] = true
	}

	for id := range behaviorOverrides {
		if !known[id] {
			t.Errorf("behaviorOverrides key %q does not exist in the embedded catalog", id)
		}
	}
	for id := range durationOverrides {
		if !known[id] {
			t.Errorf("durationOverrides key %q does not exist in the embedded catalog", id)
		}
	}
}

// TestCatalogReturnsFullList asserts the mock exposes every catalog entry.
func TestCatalogReturnsFullList(t *testing.T) {
	_, scripts, err := catalog.LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	got := Catalog()
	if len(got) != len(scripts) {
		t.Fatalf("Catalog() returned %d fetchers, expected %d (every catalog entry)", len(got), len(scripts))
	}
}
