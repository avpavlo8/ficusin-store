package order

import (
	"strings"
	"testing"
)

func TestPreorderRequestJoinsProductThroughVariant(t *testing.T) {
	if !strings.Contains(recordPreorderRequestsSQL, "JOIN product_variants pv ON pv.id = oi.variant_id") ||
		!strings.Contains(recordPreorderRequestsSQL, "p.id = pv.product_id") {
		t.Fatal("preorder request must resolve the product through the variant relation")
	}
	if strings.Contains(recordPreorderRequestsSQL, "p.id = oi.product_id") {
		t.Fatal("legacy TEXT product_id must not be compared with products.id BIGINT")
	}
}
