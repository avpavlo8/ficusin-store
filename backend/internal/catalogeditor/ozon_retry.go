package catalogeditor

// OzonCreationRetry carries creation provenance across an explicitly changed
// payload. It does not establish remote ownership: the provider must still
// read the exact article, nonempty barcode and any previously known remote ID.
func OzonCreationRetry(previous, next Request, receipt Progress) (Progress, bool) {
	if !receipt.CreateAttempted || previous.ProductID <= 0 || previous.VariantID <= 0 ||
		previous.ProductID != next.ProductID || previous.VariantID != next.VariantID ||
		previous.Action != "create" || next.Action != "create" ||
		previous.Channel.Mode != "create" || next.Channel.Mode != "create" ||
		previous.Barcode == "" || previous.Barcode != next.Barcode ||
		previous.SellerArticle("ozon") == "" || previous.SellerArticle("ozon") != next.SellerArticle("ozon") {
		return Progress{}, false
	}
	receipt.Stage = "reconcile"
	receipt.ReapplyPayload = true
	receipt.UpdateRequest = nil // A corrected request must not reuse old content.
	receipt.Checks = 0
	return receipt, true
}
