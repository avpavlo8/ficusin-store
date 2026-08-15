package admin

import "testing"

func TestSafeMappingToken(t *testing.T) {
	for _, value := range []string{"wildberries", "offer_id", "partner-2"} {
		if !safeMappingToken(value) { t.Fatalf("valid token rejected: %s",value) }
	}
	for _, value := range []string{"", "WB SKU", "../secret", "озон"} {
		if safeMappingToken(value) { t.Fatalf("unsafe token accepted: %s",value) }
	}
}
