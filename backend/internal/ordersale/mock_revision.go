package ordersale

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type mockRevisionReceipt struct {
	Version                             int   `json:"version"`
	RevisionID                          int64 `json:"revisionId"`
	OriginHash, CurrentHash, TargetHash string
	Lines                               []StockProofLine `json:"lines"`
	OldIDs                              []int64          `json:"oldIds"`
	Target                              bool             `json:"target"`
}

func mockRevisionDocument(request Request, p Progress, rows []StockProofLine, negative bool) (StockProof, error) {
	raw, err := mockStockProof(request, p)
	if err != nil {
		return StockProof{}, err
	}
	var proof StockProof
	if err = json.Unmarshal(raw, &proof); err != nil {
		return proof, err
	}
	proof.Lines = append([]StockProofLine{}, rows...)
	proof.Posted, proof.HasEntries = !negative, !negative
	for i := range proof.Lines {
		proof.Lines[i].Closed = !negative
	}
	return proof, nil
}

func (MockProvider) StepRevision(_ context.Context, c RevisionCommand, p Progress) (Result, error) {
	if c.Origin.ExecutionMode != "mock" || c.Current.ExecutionMode != "mock" || c.Target.ExecutionMode != "mock" {
		return Result{}, fmt.Errorf("mock revision provider rejects live requests")
	}
	r := Result{Progress: p, State: "queued", Message: "Тестовая редакция того же документа; реальные остатки СБИС не меняются"}
	var receipt mockRevisionReceipt
	finish := func(stage string) (Result, error) {
		r.Progress.Stage = stage
		r.Progress.RevisionNative, _ = json.Marshal(receipt)
		return r, nil
	}
	attention := func() (Result, error) {
		r.State = "needs_attention"
		r.Message = "Не подтверждена привязка тестовой редакции"
		return r, nil
	}
	if !validRevisionCommand(c) || p.DocumentID != fmt.Sprintf("STG-SALE-%d", c.Origin.IntentID) || p.DocumentUUID != c.Origin.OperationKey {
		return attention()
	}
	o, _ := requestHash(c.Origin)
	current, _ := requestHash(c.Current)
	target, _ := requestHash(c.Target)
	if len(p.RevisionNative) == 0 {
		if p.Unknown {
			return attention()
		}
		raw, err := mockStockProof(c.Current, p)
		if err != nil {
			return Result{}, err
		}
		var proof StockProof
		if json.Unmarshal(raw, &proof) != nil {
			return attention()
		}
		ids := p.ActiveOwnedRowIDs
		if ids == nil {
			ids = p.OwnedRowIDs
		}
		if len(ids) > len(proof.Lines) {
			return attention()
		}
		rows := append([]StockProofLine{}, proof.Lines[:len(ids)]...)
		for i := range rows {
			rows[i].ID = ids[i]
		}
		receipt = mockRevisionReceipt{Version: 1, RevisionID: c.RevisionID, OriginHash: o, CurrentHash: current, TargetHash: target, Lines: rows, OldIDs: append([]int64{}, ids...)}
		r.Progress.ActiveOwnedRowIDs = append([]int64{}, ids...)
	} else if json.Unmarshal(p.RevisionNative, &receipt) != nil || receipt.Version != 1 || receipt.RevisionID != c.RevisionID || receipt.OriginHash != o || receipt.CurrentHash != current || receipt.TargetHash != target {
		return attention()
	}
	negative := func(request Request) (json.RawMessage, error) {
		proof, e := mockRevisionDocument(request, r.Progress, receipt.Lines, true)
		if e != nil {
			return nil, e
		}
		return json.Marshal(proof)
	}
	cancelled := func() (Result, error) {
		raw, err := negative(c.Target)
		if err != nil {
			return Result{}, err
		}
		var proof StockProof
		json.Unmarshal(raw, &proof)
		kind := "revision_unposted"
		if r.Progress.RevisionPostAttempted {
			kind = "unposted_full"
		}
		r.CancellationVerified = true
		r.State = "cancelled"
		r.Progress.Unknown = false
		r.CancellationProof, _ = json.Marshal(CancellationProof{Kind: kind, Document: &proof})
		return finish("cancelled")
	}
	stage := p.Stage
	if !strings.HasPrefix(stage, "revision-") {
		if len(p.RevisionNative) > 0 && stage != "verified" {
			return attention()
		}
		if stage == "verified" && len(p.RevisionNative) > 0 {
			stage = "revision-verify-post"
		} else {
			stage = "revision-inspect"
		}
	}
	if p.DesiredState == "cancelled" && p.RevisionUnpostVerified && !p.RevisionPostAttempted {
		return cancelled()
	}
	switch stage {
	case "revision-inspect":
		return finish("revision-unpost")
	case "revision-unpost":
		return finish("revision-verify-unpost")
	case "revision-verify-unpost":
		current := c.Current
		current.RevisionID = c.RevisionID
		var err error
		r.RevisionUnpostProof, err = negative(current)
		if err != nil {
			return Result{}, err
		}
		r.RevisionUnpostVerified, r.Progress.RevisionUnpostVerified, r.Progress.Unknown = true, true, false
		if p.DesiredState == "cancelled" {
			return cancelled()
		}
		return finish("revision-delete")
	case "revision-delete":
		return finish("revision-verify-delete")
	case "revision-verify-delete":
		r.Progress.ActiveOwnedRowIDs = []int64{}
		for _, id := range receipt.OldIDs {
			r.Progress.RetiredRowIDs = appendUniqueMockID(r.Progress.RetiredRowIDs, id)
		}
		receipt.Lines = []StockProofLine{}
		readback, err := negative(c.Target)
		if err != nil {
			return Result{}, err
		}
		r.RevisionRetirementProof, _ = json.Marshal(RevisionRetirementProof{Version: 1, RevisionID: c.RevisionID, OriginHash: o, CurrentHash: current, TargetHash: target, DocumentID: p.DocumentID, DocumentUUID: p.DocumentUUID, RetiredRowIDs: append([]int64{}, r.Progress.RetiredRowIDs...), RemainingRowIDs: append([]int64{}, r.Progress.ActiveOwnedRowIDs...), Readback: readback})
		r.Progress.Unknown = false
		if p.DesiredState == "cancelled" {
			return cancelled()
		}
		return finish("revision-prepare")
	case "revision-prepare":
		return finish("revision-verify-prepare")
	case "revision-verify-prepare":
		r.Progress.Unknown = false
		return finish("revision-save")
	case "revision-save":
		raw, err := mockStockProof(c.Target, p)
		if err != nil {
			return Result{}, err
		}
		var proof StockProof
		json.Unmarshal(raw, &proof)
		maxID := int64(0)
		for _, id := range p.OwnedRowIDs {
			if maxID < id {
				maxID = id
			}
		}
		r.Progress.ActiveOwnedRowIDs = []int64{}
		for i := range proof.Lines {
			maxID++
			proof.Lines[i].ID = maxID
			proof.Lines[i].Closed = false
			r.Progress.ActiveOwnedRowIDs = append(r.Progress.ActiveOwnedRowIDs, maxID)
			r.Progress.OwnedRowIDs = appendUniqueMockID(r.Progress.OwnedRowIDs, maxID)
		}
		receipt.Lines, receipt.Target = proof.Lines, true
		return finish("revision-verify-save")
	case "revision-verify-save":
		// A recovered mock write is deterministic and has no external transport.
		if !receipt.Target {
			return attention()
		}
		r.Progress.Unknown = false
		return finish("revision-post")
	case "revision-post":
		return finish("revision-verify-post")
	case "revision-verify-post":
		r.Progress.Unknown = false
		if p.DesiredState == "cancelled" {
			return finish("revision-cancel")
		}
		proof, err := mockRevisionDocument(c.Target, r.Progress, receipt.Lines, false)
		if err != nil {
			return Result{}, err
		}
		r.RevisionProof, _ = json.Marshal(proof)
		r.RevisionVerified, r.StockVerified = true, true
		r.StockProof = r.RevisionProof
		r.State = "posted"
		return finish("verified")
	case "revision-cancel":
		return finish("revision-verify-cancel")
	case "revision-verify-cancel":
		return cancelled()
	}
	return attention()
}
