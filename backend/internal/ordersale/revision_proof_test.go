package ordersale

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

func revisionProofFixture(t *testing.T) (RevisionCommand, StockProof, StockProof) {
	t.Helper()
	origin, current := liveStockProofFixture()
	target := origin
	target.RevisionID = 20
	target.Event.Lines = []Line{{OrderItemID: 99, VariantID: 10, SabyID: "1101", Quantity: 1, UnitPrice: "8.25"}}
	finish(&target.Event)
	c := RevisionCommand{Origin: origin, Current: origin, Target: target, CurrentRevisionID: 19, RevisionID: 20}
	next := current
	next.RevisionID = 20
	next.EventFingerprint = target.Event.Fingerprint
	next.Lines = []StockProofLine{{ID: 10, DocumentID: current.DocumentID, NomenclatureID: "1101", WarehouseID: 284, Direction: 1, Quantity: "1", UnitPrice: "8.25", LineSum: "8.25", Closed: true}}
	return c, current, next
}

func revisionNegative(p StockProof) StockProof {
	p.Posted, p.HasEntries = false, false
	p.Lines = append([]StockProofLine{}, p.Lines...)
	for i := range p.Lines {
		p.Lines[i].Closed = false
	}
	return p
}

func TestRevisionProofRequiresTargetExactActiveIDsAndHistoricalOwnership(t *testing.T) {
	c, _, p := revisionProofFixture(t)
	r := stockProofResult(t, p)
	r.RevisionVerified = true
	r.RevisionProof = r.StockProof
	r.Progress.OwnedRowIDs = []int64{1, 2, 3, 10}
	r.Progress.RetiredRowIDs = []int64{1, 2, 3}
	r.Progress.ActiveOwnedRowIDs = []int64{10}
	if !validRevisionProof(c, r) {
		t.Fatal("valid target proof rejected")
	}
	for _, mode := range []string{"oldfingerprint", "wrongrevision", "missinghistory", "retiredactive", "wrongactive", "unknown", "wrongmoney", "oldnativeidentity"} {
		t.Run(mode, func(t *testing.T) {
			copy := r
			raw, _ := json.Marshal(r)
			json.Unmarshal(raw, &copy)
			var proof StockProof
			json.Unmarshal(copy.RevisionProof, &proof)
			switch mode {
			case "oldfingerprint":
				proof.EventFingerprint = c.Current.Event.Fingerprint
			case "wrongrevision":
				proof.RevisionID--
			case "missinghistory":
				copy.Progress.OwnedRowIDs = []int64{10}
			case "retiredactive":
				copy.Progress.RetiredRowIDs = append(copy.Progress.RetiredRowIDs, 10)
			case "wrongactive":
				copy.Progress.ActiveOwnedRowIDs = []int64{11}
			case "unknown":
				copy.Progress.Unknown = true
			case "wrongmoney":
				proof.Lines[0].LineSum = "8.26"
			case "oldnativeidentity":
				proof.DocumentID = "2358"
			}
			copy.RevisionProof, _ = json.Marshal(proof)
			if validRevisionProof(c, copy) {
				t.Fatal("unsafe target accepted")
			}
		})
	}
}

func TestRevisionUnpostBindsCurrentContentsToTargetRevision(t *testing.T) {
	c, current, _ := revisionProofFixture(t)
	current = revisionNegative(current)
	current.RevisionID = c.RevisionID
	r := Result{RevisionUnpostVerified: true, Progress: Progress{DocumentID: current.DocumentID, DocumentUUID: current.DocumentUUID, RevisionUnpostVerified: true, OwnedRowIDs: []int64{1, 2, 3}, ActiveOwnedRowIDs: []int64{1, 2, 3}}}
	r.RevisionUnpostProof, _ = json.Marshal(current)
	if !validRevisionUnpostProof(c, r) {
		t.Fatal("current negative targetrevision proof rejected")
	}
	current.EventFingerprint = c.Target.Event.Fingerprint
	r.RevisionUnpostProof, _ = json.Marshal(current)
	if validRevisionUnpostProof(c, r) {
		t.Fatal("target fingerprint pretended to verify old composition")
	}
}

func retirementFixture(t *testing.T) (RevisionCommand, Result) {
	t.Helper()
	c, current, target := revisionProofFixture(t)
	proof := revisionNegative(target)
	proof.Lines = append([]StockProofLine{}, revisionNegative(current).Lines[1:]...)
	r := Result{Progress: Progress{DocumentID: target.DocumentID, DocumentUUID: target.DocumentUUID, RevisionUnpostVerified: true, OwnedRowIDs: []int64{1, 2, 3}, ActiveOwnedRowIDs: []int64{2, 3}, RetiredRowIDs: []int64{1}}}
	o, _ := requestHash(c.Origin)
	cur, _ := requestHash(c.Current)
	next, _ := requestHash(c.Target)
	readback, _ := json.Marshal(proof)
	r.RevisionRetirementProof, _ = json.Marshal(RevisionRetirementProof{Version: 1, RevisionID: c.RevisionID, OriginHash: o, CurrentHash: cur, TargetHash: next, DocumentID: target.DocumentID, DocumentUUID: target.DocumentUUID, RetiredRowIDs: []int64{1}, RemainingRowIDs: []int64{2, 3}, Readback: readback})
	return c, r
}

func TestRevisionRetirementRequiresFreshExactAbsenceAndFrozenHashes(t *testing.T) {
	c, r := retirementFixture(t)
	if !validRevisionRetirementProof(c, r) {
		t.Fatal("exact retirement rejected")
	}
	for _, mode := range []string{"retiredstillpresent", "unknown", "wronghash", "droppedknownid", "wrongremaining", "postattempted", "missingunpost", "missingflag"} {
		t.Run(mode, func(t *testing.T) {
			_, copy := retirementFixture(t)
			var p RevisionRetirementProof
			json.Unmarshal(copy.RevisionRetirementProof, &p)
			switch mode {
			case "retiredstillpresent":
				p.RetiredRowIDs = []int64{2}
				copy.Progress.RetiredRowIDs = []int64{2}
			case "unknown":
				copy.Progress.Unknown = true
			case "wronghash":
				p.TargetHash = p.OriginHash
			case "droppedknownid":
				copy.Progress.OwnedRowIDs = []int64{2, 3}
			case "wrongremaining":
				p.RemainingRowIDs = []int64{3}
			case "postattempted":
				copy.Progress.RevisionPostAttempted = true
			case "missingunpost":
				copy.Progress.RevisionUnpostVerified = false
			case "missingflag":
				var doc map[string]json.RawMessage
				json.Unmarshal(p.Readback, &doc)
				delete(doc, "entriesQueued")
				p.Readback, _ = json.Marshal(doc)
			}
			copy.RevisionRetirementProof, _ = json.Marshal(p)
			if validRevisionRetirementProof(c, copy) {
				t.Fatal("unsafe retirement accepted")
			}
		})
	}
}

func TestRevisionCancellationAcceptsOpenPrefixButNeverUnknownPost(t *testing.T) {
	c, r := retirementFixture(t)
	var retirement RevisionRetirementProof
	json.Unmarshal(r.RevisionRetirementProof, &retirement)
	var p StockProof
	json.Unmarshal(retirement.Readback, &p)
	r.State, r.CancellationVerified, r.Progress.DesiredState, r.Progress.CreateAttempted = "cancelled", true, "cancelled", true
	r.CancellationProof, _ = json.Marshal(CancellationProof{Kind: "revision_unposted", Document: &p})
	if !validRevisionCancellationProof(c, r) {
		t.Fatal("exact current prefix cancellation rejected")
	}
	r.Progress.RevisionPostAttempted = true
	if validRevisionCancellationProof(c, r) {
		t.Fatal("old unpost accepted after new post attempt")
	}
	_, _, target := revisionProofFixture(t)
	target = revisionNegative(target)
	r.Progress.OwnedRowIDs = []int64{1, 2, 3, 10}
	r.Progress.RetiredRowIDs = []int64{1, 2, 3}
	r.Progress.ActiveOwnedRowIDs = []int64{10}
	r.CancellationProof, _ = json.Marshal(CancellationProof{Kind: "unposted_full", Document: &target})
	if !validRevisionCancellationProof(c, r) {
		t.Fatal("full target reversal rejected")
	}
	r.Progress.Unknown = true
	if validRevisionCancellationProof(c, r) {
		t.Fatal("unknown reversal accepted")
	}
}

func TestMockRevisionRetainsSameDocumentOriginAndValidProofs(t *testing.T) {
	c, _, _ := revisionProofFixture(t)
	c.Origin.ExecutionMode, c.Current.ExecutionMode, c.Target.ExecutionMode = "mock", "mock", "mock"
	p := Progress{Stage: "verified", DocumentID: fmt.Sprintf("STG-SALE-%d", c.Origin.IntentID), DocumentUUID: c.Origin.OperationKey, Native: json.RawMessage(`{"original":"immutable"}`), OwnedRowIDs: []int64{1, 2, 3}, ActiveOwnedRowIDs: []int64{1, 2, 3}, CreateAttempted: true, PostAttempted: true, DesiredState: "posted"}
	var final Result
	seenUnpost, seenRetire := false, false
	for i := 0; i < 30; i++ {
		if p.Stage == "revision-unpost" {
			p.RevisionUnpostAttempted = true
		}
		if p.Stage == "revision-post" {
			p.RevisionPostAttempted = true
		}
		var err error
		final, err = (MockProvider{}).StepRevision(context.Background(), c, p)
		if err != nil {
			t.Fatal(err)
		}
		if final.RevisionUnpostVerified {
			seenUnpost = true
			if !validRevisionUnpostProof(c, final) {
				t.Fatal("invalid mock unpost")
			}
		}
		if len(final.RevisionRetirementProof) > 0 {
			seenRetire = true
			if !validRevisionRetirementProof(c, final) {
				t.Fatal("invalid mock retirement")
			}
		}
		p = final.Progress
		if final.State != "queued" {
			break
		}
	}
	if final.State != "posted" || !seenUnpost || !seenRetire || !validRevisionProof(c, final) || string(p.Native) != `{"original":"immutable"}` || p.DocumentID != fmt.Sprintf("STG-SALE-%d", c.Origin.IntentID) {
		t.Fatalf("mock revision invalid: %s %s", final.State, final.Message)
	}
	p.DesiredState = "cancelled"
	for i := 0; i < 10; i++ {
		var err error
		final, err = (MockProvider{}).StepRevision(context.Background(), c, p)
		if err != nil {
			t.Fatal(err)
		}
		p = final.Progress
		if final.State != "queued" {
			break
		}
	}
	if !validRevisionCancellationProof(c, final) {
		t.Fatal("mock cancellation proof invalid")
	}
	bad := c
	bad.Target.ExecutionMode = "live"
	if _, err := (MockProvider{}).StepRevision(context.Background(), bad, p); err == nil {
		t.Fatal("mock accepted live")
	}
}

func TestMockRevisionCancellationFromEveryCommittedBoundary(t *testing.T) {
	c, _, _ := revisionProofFixture(t)
	c.Origin.ExecutionMode, c.Current.ExecutionMode, c.Target.ExecutionMode = "mock", "mock", "mock"
	p := Progress{Stage: "revision-inspect", DocumentID: fmt.Sprintf("STG-SALE-%d", c.Origin.IntentID), DocumentUUID: c.Origin.OperationKey, Native: json.RawMessage(`{"original":"immutable"}`), OwnedRowIDs: []int64{1, 2, 3}, ActiveOwnedRowIDs: []int64{1, 2, 3}, CreateAttempted: true, PostAttempted: true, DesiredState: "posted"}
	states := []Progress{}
	for i := 0; i < 25; i++ {
		raw, _ := json.Marshal(p)
		var copy Progress
		json.Unmarshal(raw, &copy)
		states = append(states, copy)
		if p.Stage == "revision-post" {
			p.RevisionPostAttempted = true
		}
		if p.Stage == "revision-unpost" {
			p.RevisionUnpostAttempted = true
		}
		r, err := (MockProvider{}).StepRevision(context.Background(), c, p)
		if err != nil {
			t.Fatal(err)
		}
		p = r.Progress
		if r.State != "queued" {
			break
		}
	}
	if len(states) < 10 {
		t.Fatal("missing revision boundaries")
	}
	for _, initial := range states {
		t.Run(initial.Stage, func(t *testing.T) {
			p := initial
			p.DesiredState = "cancelled"
			var result Result
			for i := 0; i < 10; i++ {
				var err error
				result, err = (MockProvider{}).StepRevision(context.Background(), c, p)
				if err != nil {
					t.Fatal(err)
				}
				p = result.Progress
				if result.State != "queued" {
					break
				}
			}
			if !validRevisionCancellationProof(c, result) || string(p.Native) != `{"original":"immutable"}` {
				t.Fatalf("cancel boundary invalid: %s %s", result.State, result.Message)
			}
		})
	}
}

func TestMockRevisionEmptyOriginRetirementSerializesExplicitEmptyArrays(t *testing.T) {
	c, _, _ := revisionProofFixture(t)
	c.Origin.ExecutionMode, c.Current.ExecutionMode, c.Target.ExecutionMode = "mock", "mock", "mock"
	p := Progress{Stage: "revision-inspect", DocumentID: fmt.Sprintf("STG-SALE-%d", c.Origin.IntentID), DocumentUUID: c.Origin.OperationKey, CreateAttempted: true, DesiredState: "posted"}
	seen := false
	var final Result
	for i := 0; i < 25; i++ {
		if p.Stage == "revision-unpost" {
			p.RevisionUnpostAttempted = true
		}
		if p.Stage == "revision-post" {
			p.RevisionPostAttempted = true
		}
		var err error
		final, err = (MockProvider{}).StepRevision(context.Background(), c, p)
		if err != nil {
			t.Fatal(err)
		}
		if len(final.RevisionRetirementProof) > 0 {
			seen = true
			if !validRevisionRetirementProof(c, final) {
				t.Fatalf("empty origin retirement rejected: %s", final.RevisionRetirementProof)
			}
			var envelope map[string]json.RawMessage
			json.Unmarshal(final.RevisionRetirementProof, &envelope)
			if string(envelope["retiredRowIds"]) != "[]" || string(envelope["remainingRowIds"]) != "[]" {
				t.Fatal("missing array was treated as confirmed empty IDs")
			}
		}
		raw, _ := json.Marshal(final.Progress)
		json.Unmarshal(raw, &p)
		if final.State != "queued" {
			break
		}
	}
	if !seen || !validRevisionProof(c, final) {
		t.Fatalf("header-only origin did not revise: %s %s", final.State, final.Message)
	}
}
