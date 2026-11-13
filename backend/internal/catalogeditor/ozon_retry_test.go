package catalogeditor

import "testing"

func TestOzonCreationRetryRequiresUnchangedCreationIdentity(t *testing.T) {
	previous := Request{ProductID: 11, VariantID: 12, Action: "create", Barcode: "2000000000015", Marketplace: Marketplace{Article: "own-article"}, Channel: Channel{Mode: "create"}, Packaging: Packaging{Weight: 1}}
	next := previous
	next.Packaging.Weight = 1000
	receipt := Progress{Stage: "ozon-poll", TaskID: 123, CreateAttempted: true, Ambiguous: true, UpdateRequest: &previous, Checks: 7}
	retry, ok := OzonCreationRetry(previous, next, receipt)
	if !ok || retry.Stage != "reconcile" || !retry.ReapplyPayload || !retry.CreateAttempted || !retry.Ambiguous || retry.TaskID != 123 || retry.UpdateRequest != nil || retry.Checks != 0 {
		t.Fatalf("creation receipt was not retained for read-before-update: %+v", retry)
	}
	for _, name := range []string{"unattempted", "product", "variant", "article", "barcode", "empty barcode", "link", "resolve"} {
		t.Run(name, func(t *testing.T) {
			candidate, proof := next, receipt
			switch name {
			case "unattempted":
				proof.CreateAttempted = false
			case "product":
				candidate.ProductID++
			case "variant":
				candidate.VariantID++
			case "article":
				candidate.Channel.Article = strPointer("another-article")
			case "barcode":
				candidate.Barcode = "another-barcode"
			case "empty barcode":
				candidate.Barcode = ""
			case "link":
				candidate.Channel.Mode = "link"
			case "resolve":
				candidate.Action = "resolve"
			}
			if _, ok := OzonCreationRetry(previous, candidate, proof); ok {
				t.Fatal("unproven creation identity accepted")
			}
		})
	}
}

func strPointer(value string) *string { return &value }
