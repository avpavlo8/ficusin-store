package procurement

import (
	"encoding/json"
	"testing"
)

func TestPlanDocumentValidation(t *testing.T) {
	if _, err := parsePlanDocument(json.RawMessage(`{"supplierId":1,"items":[{"sabyId":"plant","expectedUnitPrice":"1,15","potDiameterCm":"19","heightCm":"60"}]}`)); err != nil {
		t.Fatalf("valid plan rejected: %v", err)
	}
	for _, value := range []string{"NaN", "Infinity", "-1", "10000000000", "abc"} {
		if _, err := planDecimal(value); err == nil {
			t.Errorf("invalid amount %q accepted", value)
		}
	}
	if size, err := planSize(""); err != nil || size != -1 {
		t.Fatalf("missing size must use distinct sentinel: %v, %v", size, err)
	}
}
