package catalogeditor

import (
	"encoding/json"
	"strconv"
)

// SabyPublicationRevision keeps a confirmed native identity when new content
// replaces a failed draft. A Create reply can contain Id before Number exists,
// so the remote-link table is not yet guaranteed to contain this receipt.
func SabyPublicationRevision(status string, previous, next Request, receipt Progress) (Progress, bool) {
	if previous.ProductID <= 0 || previous.VariantID <= 0 || previous.ProductID != next.ProductID || previous.VariantID != next.VariantID || receipt.Ambiguous {
		return Progress{}, false
	}
	id, err := strconv.ParseInt(receipt.IDs.ProductID, 10, 64)
	if err != nil || id <= 0 {
		return Progress{}, false
	}
	if next.SabyID != "" && next.SabyID != receipt.IDs.ProductID || next.ExistingID != "" && next.ExistingID != receipt.IDs.ProductID {
		return Progress{}, false
	}
	if next.Saby.Mode == "link" && (next.Saby.ExistingCode == "" || next.Saby.ExistingCode != receipt.IDs.OfferID) {
		return Progress{}, false
	}
	// Prepared hashes, uploaded media and an UpdateRequest belong to the old
	// content. Rebuild them from a fresh identity-checked read of the same Id.
	fresh := Progress{Stage: "hydrate", IDs: receipt.IDs, CreateAttempted: true}
	// A confirmed Create may fail before its initial price has been written.
	// Rebuild its content, but retain the obligation to publish that first
	// price. Finished cards return to ordinary content-only updates instead.
	var native struct {
		Created bool `json:"created"`
	}
	if len(receipt.Native) > 0 && json.Unmarshal(receipt.Native, &native) != nil {
		return Progress{}, false
	}
	if status == "failed" && receipt.Stage != "done" && native.Created {
		fresh.Native = json.RawMessage(`{"created":true}`)
	}
	return fresh, true
}
