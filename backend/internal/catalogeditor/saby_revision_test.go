package catalogeditor

import (
	"encoding/json"
	"testing"
)

func TestSabyRevisionRetainsIdentityBeforeBusinessCodeExists(t *testing.T) {
	before := Request{ProductID: 11, VariantID: 12, Action: "create", Saby: Saby{Mode: "create"}}
	next := before
	next.Description = "Corrected content"
	ids := RemoteIDs{ProductID: "3980", UUID: "00000000-0000-4000-8000-000000003980", SKU: "site-877", Barcode: "2000000000015"}
	receipt := Progress{Stage: "hydrate", CreateAttempted: true, IDs: ids, Native: json.RawMessage(`{"created":true,"preparedHash":"old","images":[{}]}`), UpdateRequest: &before}
	for _, status := range []string{"failed", "synced"} {
		fresh, ok := PendingPublicationProgress("saby", status, before, next, receipt)
		if !ok || fresh.Stage != "hydrate" || fresh.IDs != ids || !fresh.CreateAttempted || len(fresh.Native) != 0 || fresh.UpdateRequest != nil {
			t.Fatalf("%s revision lost native identity or reused old content: %+v/%t", status, fresh, ok)
		}
	}
	for _, scenario := range []string{"ambiguous", "different variant", "different native id", "different link", "invalid id"} {
		t.Run(scenario, func(t *testing.T) {
			candidate, proof := next, receipt
			switch scenario {
			case "ambiguous":
				proof.Ambiguous = true
			case "different variant":
				candidate.VariantID++
			case "different native id":
				candidate.SabyID = "9999"
			case "different link":
				candidate.Saby = Saby{Mode: "link", ExistingCode: "other-card"}
			case "invalid id":
				proof.IDs.ProductID = "0"
			}
			if _, ok := PendingPublicationProgress("saby", "failed", before, candidate, proof); ok {
				t.Fatal("unresolved or changed identity discarded the prior operation")
			}
		})
	}
}
