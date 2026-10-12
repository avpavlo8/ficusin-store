package order

import (
	"strings"
	"testing"
)

func TestPreorderRequestUsesPurchasedVariantSabyIdentity(t *testing.T) {
	if !strings.Contains(recordPreorderRequestsSQL, "JOIN product_variants pv ON pv.id = oi.variant_id") {
		t.Fatal("preorder request must retain the concrete purchased variant relation")
	}
	if !strings.Contains(recordPreorderRequestsSQL, "n.saby_id = pv.saby_id") {
		t.Fatal("preorder demand must follow the purchased SKU after a card merge")
	}
}
