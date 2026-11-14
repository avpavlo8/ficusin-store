package ordersale

import (
	"encoding/json"
	"testing"
)

func cancelProofFixture(t *testing.T, kind string) (Request, Result) {
	t.Helper()
	r, p := liveStockProofFixture()
	p.Posted = false
	p.HasEntries = false
	ids := []int64{}
	for i := range p.Lines {
		p.Lines[i].Closed = false
		ids = append(ids, p.Lines[i].ID)
	}
	result := Result{State: "cancelled", CancellationVerified: true, Progress: Progress{Stage: "cancelled", DesiredState: "cancelled", CreateAttempted: true, PostAttempted: kind == "unposted_full", DocumentID: p.DocumentID, DocumentUUID: p.DocumentUUID, OwnedRowIDs: ids}}
	result.CancellationProof, _ = json.Marshal(CancellationProof{Kind: kind, Document: &p})
	return r, result
}
func TestCancellationProofBindsCompleteOwnedReversal(t *testing.T) {
	r, result := cancelProofFixture(t, "unposted_full")
	if !validCancellationProof(r, result) {
		t.Fatal("exact reversal rejected")
	}
	for _, mode := range []string{"missingprice", "missingfalseflag", "posted", "closed", "wrongrowid", "unknown", "noattempt", "wrongbuyer", "wrongsum"} {
		t.Run(mode, func(t *testing.T) {
			r, result := cancelProofFixture(t, "unposted_full")
			var p CancellationProof
			json.Unmarshal(result.CancellationProof, &p)
			switch mode {
			case "posted":
				p.Document.Posted = true
			case "closed":
				p.Document.Lines[0].Closed = true
			case "wrongrowid":
				p.Document.Lines[0].ID = 999
			case "unknown":
				result.Progress.Unknown = true
			case "noattempt":
				result.Progress.PostAttempted = false
			case "wrongbuyer":
				p.Document.BuyerID = 0
			case "wrongsum":
				p.Document.Lines[0].LineSum = "20.41"
			}
			result.CancellationProof, _ = json.Marshal(p)
			if mode == "missingprice" || mode == "missingfalseflag" {
				var envelope map[string]json.RawMessage
				json.Unmarshal(result.CancellationProof, &envelope)
				var doc map[string]json.RawMessage
				json.Unmarshal(envelope["document"], &doc)
				if mode == "missingfalseflag" {
					delete(doc, "postingQueued")
				} else {
					var lines []map[string]json.RawMessage
					json.Unmarshal(doc["lines"], &lines)
					delete(lines[0], "unitPrice")
					doc["lines"], _ = json.Marshal(lines)
				}
				envelope["document"], _ = json.Marshal(doc)
				result.CancellationProof, _ = json.Marshal(envelope)
			}
			if validCancellationProof(r, result) {
				t.Fatal("unsafe reversal accepted", mode)
			}
		})
	}
}
func TestCancellationProofPartialDraftRequiresNeverPostedAndExactRetainedIDs(t *testing.T) {
	r, result := cancelProofFixture(t, "unposted_draft")
	var p CancellationProof
	json.Unmarshal(result.CancellationProof, &p)
	p.Document.BuyerID = 0
	p.Document.Lines = p.Document.Lines[:1]
	result.Progress.OwnedRowIDs = []int64{p.Document.Lines[0].ID}
	result.CancellationProof, _ = json.Marshal(p)
	if !validCancellationProof(r, result) {
		t.Fatal("owned never-posted partial draft rejected")
	}
	result.Progress.PostAttempted = true
	if validCancellationProof(r, result) {
		t.Fatal("unknown async posting treated as empty draft")
	}
	result.Progress.PostAttempted = false
	result.Progress.OwnedRowIDs = append(result.Progress.OwnedRowIDs, 999)
	if validCancellationProof(r, result) {
		t.Fatal("lost acknowledged row released hold")
	}
	result.Progress.OwnedRowIDs = nil
	p.Document.Lines = []StockProofLine{}
	result.CancellationProof, _ = json.Marshal(p)
	if !validCancellationProof(r, result) {
		t.Fatal("owned empty never-posted draft rejected")
	}
}
func TestCancellationProofNoRemoteRequiresAuthoritativeNeverAttempted(t *testing.T) {
	r, _ := liveStockProofFixture()
	result := Result{State: "cancelled", CancellationVerified: true, Progress: Progress{DesiredState: "cancelled"}, CancellationProof: json.RawMessage(`{"kind":"no_remote"}`)}
	if !validCancellationProof(r, result) {
		t.Fatal("local cancellation before any write rejected")
	}
	result.Progress.CreateAttempted = true
	if validCancellationProof(r, result) {
		t.Fatal("unknown create considered never-sent")
	}
}
