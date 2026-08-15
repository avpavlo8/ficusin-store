package catalog

import (
	"strings"
	"testing"
)

func TestCatalogPopularityQueryUsesConfirmedSales(t *testing.T) {
	wants := []string{
		"SUM(oi.quantity",
		"o.status <> 'cancelled'",
		"o.status = 'completed' OR o.payment_status = 'paid'",
		"INTERVAL '30 days'",
		"INTERVAL '90 days'",
		"INTERVAL '365 days'",
	}
	for _, want := range wants {
		if !strings.Contains(catalogListQuery, want) {
			t.Errorf("catalog query does not contain %q", want)
		}
	}
}
