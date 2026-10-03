package factory

import (
	"sort"
	"testing"
)

func TestListDefinitionsIsSortedByID(t *testing.T) {
	defs := ListDefinitions()
	if len(defs) < 2 {
		t.Skip("needs at least two registered definitions")
	}
	ids := make([]string, len(defs))
	for i, d := range defs {
		ids[i] = d.ID
	}
	if !sort.StringsAreSorted(ids) {
		t.Errorf("definitions not sorted by ID: %v", ids)
	}
	// Stable across calls — the map iteration order must not leak through.
	for n := 0; n < 5; n++ {
		again := ListDefinitions()
		for i := range again {
			if again[i].ID != ids[i] {
				t.Fatalf("order changed between calls: %v vs %v", ids, again)
			}
		}
	}
}
