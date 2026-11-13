package ordersale

import (
	"bytes"
	"encoding/json"
	"io"
)

// CancellationProof never overwrites the original positive stock receipt.
// no_remote is a local skip, unposted_draft proves a never-posted partial draft,
// unposted_full proves reversal of the complete owned native realization.
type CancellationProof struct {
	Kind     string      `json:"kind"`
	Document *StockProof `json:"document,omitempty"`
}

func validCancellationProof(request Request, result Result) bool {
	if result.State != "cancelled" || !result.CancellationVerified || result.Progress.DesiredState != "cancelled" || result.Progress.Unknown || result.Progress.IdentityConflict || len(result.CancellationProof) == 0 || len(result.CancellationProof) > maxStockProofBytes {
		return false
	}
	var proof CancellationProof
	d := json.NewDecoder(bytes.NewReader(result.CancellationProof))
	d.DisallowUnknownFields()
	if d.Decode(&proof) != nil || d.Decode(new(any)) != io.EOF {
		return false
	}
	if proof.Kind == "no_remote" {
		return completeProofObject(result.CancellationProof, "kind") && proof.Document == nil && !result.Progress.CreateAttempted && !result.Progress.PostAttempted && result.Progress.DocumentID == "" && result.Progress.DocumentUUID == "" && len(result.Progress.OwnedRowIDs) == 0
	}
	if !completeProofObject(result.CancellationProof, "kind", "document") || proof.Document == nil || (proof.Kind != "unposted_draft" && proof.Kind != "unposted_full") || !result.Progress.CreateAttempted {
		return false
	}
	partial := proof.Kind == "unposted_draft"
	if partial && result.Progress.PostAttempted || !partial && !result.Progress.PostAttempted {
		return false
	}
	ids := map[int64]bool{}
	for _, id := range result.Progress.OwnedRowIDs {
		if id <= 0 || ids[id] {
			return false
		}
		ids[id] = true
	}
	if len(ids) != len(proof.Document.Lines) {
		return false
	}
	for _, line := range proof.Document.Lines {
		if !ids[line.ID] {
			return false
		}
		delete(ids, line.ID)
	}
	if len(ids) != 0 {
		return false
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(result.CancellationProof, &envelope) != nil {
		return false
	}
	raw := envelope["document"]
	evidence := result
	evidence.State = "posted"
	evidence.StockVerified = true
	evidence.StockProof = raw
	return validStockEvidence(request, evidence, true, partial)
}
