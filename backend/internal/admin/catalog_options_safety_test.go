package admin

import (
	"os"
	"strings"
	"testing"
)

func TestAttributeOptionUpdatePreservesExistingCodes(t *testing.T) {
	raw, err := os.ReadFile("catalog_pim.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	if strings.Contains(source, "DELETE FROM attribute_options WHERE attribute_id=$1") {
		t.Fatal("dictionary update must not delete options referenced by existing values")
	}
	for _, want := range []string{"ON CONFLICT(attribute_id,code) DO UPDATE", "SET is_active=FALSE"} {
		if !strings.Contains(source, want) {
			t.Errorf("dictionary update lost preservation rule %q", want)
		}
	}
}
