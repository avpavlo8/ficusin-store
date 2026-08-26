package catalog

import (
	"strings"
	"testing"
)

func TestCatalogFilterDisplayModeComesFromCatalogFilters(t *testing.T) {
	for _, want := range []string{"filter.display_mode", "'displayMode',value.display_mode"} {
		if !strings.Contains(catalogListQuery, want) {
			t.Fatalf("catalog query must expose configured filter mode: missing %q", want)
		}
	}
	if strings.Contains(catalogListQuery, "CASE effective.code") {
		t.Fatal("storefront filter display mode must not be hard-coded by attribute code")
	}
}
