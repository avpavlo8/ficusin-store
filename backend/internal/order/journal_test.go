package order

import (
	"strings"
	"testing"
)

func TestPreorderRequestUsesExplicitProductAndVariantRelations(t *testing.T) {
	const explicitProductJoin = "LEFT JOIN products p ON p.id = oi.product_id"
	if !strings.Contains(recordPreorderRequestsSQL, "JOIN product_variants pv ON pv.id = oi.variant_id") {
		t.Fatal("preorder request must retain the concrete purchased variant relation")
	}
	if !strings.Contains(recordPreorderRequestsSQL, explicitProductJoin) {
		t.Fatal("order item must use the explicit products.id foreign key")
	}
	if strings.Contains(recordPreorderRequestsSQL, "p.id = pv.product_id") {
		t.Fatal("product identity must come from the order item snapshot, not be reconstructed through the current variant")
	}
}
