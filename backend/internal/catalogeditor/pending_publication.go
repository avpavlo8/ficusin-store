package catalogeditor

// PendingPublicationProgress promotes a saved revision only after the old
// operation is finished, or before any uncertain creation could have occurred.
// A rejected Ozon draft retains its creation proof and starts with a read.
func PendingPublicationProgress(channel, status string, previous, next Request, receipt Progress) (Progress, bool) {
	if next.ProductID <= 0 || next.VariantID <= 0 || next.ProductID != previous.ProductID || next.VariantID != previous.VariantID {
		return Progress{}, false
	}
	if (channel == "wb" || channel == "ozon") && (next.SellerArticle(channel) == "" || next.Barcode == "") {
		return Progress{}, false
	}
	if status == "created" || status == "synced" || status == "linked" {
		return Progress{}, true
	}
	if status != "failed" {
		return Progress{}, false
	}
	if channel == "ozon" {
		if retry, ok := OzonCreationRetry(previous, next, receipt); ok {
			return retry, true
		}
	}
	if receipt.Ambiguous {
		return Progress{}, false
	}
	if receipt.IDs.ProductID != "" || !receipt.CreateAttempted {
		return Progress{}, true
	}
	// A WB error-list entry without a matching upload batch is not proof that
	// this attempt was rejected. Preserve the receipt and require reconciliation.
	return Progress{}, false
}
