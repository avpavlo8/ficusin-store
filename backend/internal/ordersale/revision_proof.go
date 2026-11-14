package ordersale

import (
	"bytes"
	"encoding/json"
	"io"
	"sort"
)

func validRevisionCommand(c RevisionCommand) bool {
	if c.RevisionID <= 0 || c.CurrentRevisionID < 0 || c.RevisionID == c.CurrentRevisionID || c.Target.RevisionID != c.RevisionID || c.Current.RevisionID != 0 && c.Current.RevisionID != c.CurrentRevisionID {
		return false
	}
	for _, r := range []Request{c.Current, c.Target} {
		o := c.Origin
		if r.IntentID != o.IntentID || r.OperationKey != o.OperationKey || r.ExecutionMode != o.ExecutionMode || r.Event.OrderID != o.Event.OrderID || r.Event.Key != o.Event.Key || r.BuyerID != o.BuyerID || r.SiteWarehouseID != o.SiteWarehouseID || r.NativeWarehouseID != o.NativeWarehouseID || r.MovementWarehouseID != o.MovementWarehouseID || r.OrganizationID != o.OrganizationID || r.RegulationID != o.RegulationID {
			return false
		}
	}
	return true
}

func revisionProofIDs(ids []int64) (map[int64]bool, bool) {
	if len(ids) > maxStockProofLines*4 {
		return nil, false
	}
	out := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 || out[id] {
			return nil, false
		}
		out[id] = true
	}
	return out, true
}

func revisionSameIDs(a, b []int64) bool {
	x, ok := revisionProofIDs(a)
	if !ok || len(a) != len(b) {
		return false
	}
	for _, id := range b {
		if !x[id] {
			return false
		}
		delete(x, id)
	}
	return len(x) == 0
}

func revisionLineIDs(raw json.RawMessage) ([]int64, bool) {
	var p StockProof
	if json.Unmarshal(raw, &p) != nil {
		return nil, false
	}
	ids := make([]int64, 0, len(p.Lines))
	for _, line := range p.Lines {
		ids = append(ids, line.ID)
	}
	_, ok := revisionProofIDs(ids)
	return ids, ok
}

func revisionOwnership(p Progress) bool {
	owned, ok := revisionProofIDs(p.OwnedRowIDs)
	if !ok {
		return false
	}
	active, ok := revisionProofIDs(p.ActiveOwnedRowIDs)
	if !ok {
		return false
	}
	retired, ok := revisionProofIDs(p.RetiredRowIDs)
	if !ok || len(active)+len(retired) != len(owned) {
		return false
	}
	for id := range active {
		if !owned[id] || retired[id] {
			return false
		}
	}
	for id := range retired {
		if !owned[id] {
			return false
		}
	}
	return true
}

func revisionEvidence(r Request, result Result, raw json.RawMessage, negative, partial bool) bool {
	e := result
	e.State = "posted"
	e.StockVerified = true
	e.StockProof = raw
	return validStockEvidence(r, e, negative, partial)
}

func validRevisionProof(c RevisionCommand, r Result) bool {
	if !validRevisionCommand(c) || r.State != "posted" || !r.RevisionVerified || !revisionOwnership(r.Progress) {
		return false
	}
	ids, ok := revisionLineIDs(r.RevisionProof)
	return ok && revisionSameIDs(ids, r.Progress.ActiveOwnedRowIDs) && revisionEvidence(c.Target, r, r.RevisionProof, false, false)
}

func validRevisionUnpostProof(c RevisionCommand, r Result) bool {
	if !validRevisionCommand(c) || !r.RevisionUnpostVerified || !r.Progress.RevisionUnpostVerified || !revisionOwnership(r.Progress) {
		return false
	}
	current := c.Current
	current.RevisionID = c.RevisionID
	ids, ok := revisionLineIDs(r.RevisionUnpostProof)
	return ok && revisionSameIDs(ids, r.Progress.ActiveOwnedRowIDs) && revisionEvidence(current, r, r.RevisionUnpostProof, true, true)
}

// Replacement deletes the old composition completely before adding the new
// one. During that transition a negative receipt may contain a prefix of either
// plan. Never add their quantities together when the same group occurs in both.
func revisionUnionRequest(c RevisionCommand) (Request, bool) {
	r := c.Target
	type group struct {
		line Line
		q    int
	}
	all := map[string]group{}
	for _, request := range []Request{c.Current, c.Target} {
		local := map[string]group{}
		for _, line := range request.Event.Lines {
			price, ok := stockProofMoneyValue(line.UnitPrice)
			if !ok || line.Quantity <= 0 || line.VariantID <= 0 || !validProofNomenclature(line.SabyID, r.ExecutionMode) {
				return r, false
			}
			key := line.SabyID + ":" + price.RatString()
			g := local[key]
			if g.q > int(^uint(0)>>1)-line.Quantity {
				return r, false
			}
			g.q += line.Quantity
			g.line = line
			local[key] = g
		}
		for key, g := range local {
			if all[key].q < g.q {
				all[key] = g
			}
		}
	}
	keys := make([]string, 0, len(all))
	for key := range all {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	r.Event.Lines = make([]Line, 0, len(keys))
	for _, key := range keys {
		g := all[key]
		g.line.Quantity = g.q
		r.Event.Lines = append(r.Event.Lines, g.line)
	}
	return r, len(r.Event.Lines) > 0
}

func validRevisionRetirementProof(c RevisionCommand, r Result) bool {
	if !validRevisionCommand(c) || !r.Progress.RevisionUnpostVerified || r.Progress.RevisionPostAttempted || !revisionOwnership(r.Progress) || len(r.RevisionRetirementProof) == 0 || len(r.RevisionRetirementProof) > maxStockProofBytes {
		return false
	}
	var p RevisionRetirementProof
	d := json.NewDecoder(bytes.NewReader(r.RevisionRetirementProof))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || !completeProofObject(r.RevisionRetirementProof, "version", "revisionId", "originHash", "currentHash", "targetHash", "documentId", "documentUUID", "retiredRowIds", "remainingRowIds", "readback") {
		return false
	}
	o, oe := requestHash(c.Origin)
	current, ce := requestHash(c.Current)
	target, te := requestHash(c.Target)
	if oe != nil || ce != nil || te != nil || p.Version != 1 || p.RevisionID != c.RevisionID || p.OriginHash != o || p.CurrentHash != current || p.TargetHash != target || p.DocumentID != r.Progress.DocumentID || p.DocumentUUID != r.Progress.DocumentUUID || !revisionSameIDs(p.RetiredRowIDs, r.Progress.RetiredRowIDs) || !revisionSameIDs(p.RemainingRowIDs, r.Progress.ActiveOwnedRowIDs) {
		return false
	}
	ids, ok := revisionLineIDs(p.Readback)
	if !ok || !revisionSameIDs(ids, p.RemainingRowIDs) {
		return false
	}
	union, ok := revisionUnionRequest(c)
	return ok && revisionEvidence(union, r, p.Readback, true, true)
}

func validRevisionCancellationProof(c RevisionCommand, r Result) bool {
	if !validRevisionCommand(c) || r.State != "cancelled" || !r.CancellationVerified || r.Progress.DesiredState != "cancelled" || !r.Progress.CreateAttempted || !r.Progress.RevisionUnpostVerified || !revisionOwnership(r.Progress) || len(r.CancellationProof) == 0 || len(r.CancellationProof) > maxStockProofBytes {
		return false
	}
	var p CancellationProof
	d := json.NewDecoder(bytes.NewReader(r.CancellationProof))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || p.Document == nil || !completeProofObject(r.CancellationProof, "kind", "document") {
		return false
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(r.CancellationProof, &envelope) != nil {
		return false
	}
	ids, ok := revisionLineIDs(envelope["document"])
	if !ok || !revisionSameIDs(ids, r.Progress.ActiveOwnedRowIDs) {
		return false
	}
	if p.Kind == "unposted_full" {
		return r.Progress.RevisionPostAttempted && revisionEvidence(c.Target, r, envelope["document"], true, false)
	}
	if p.Kind != "revision_unposted" || r.Progress.RevisionPostAttempted {
		return false
	}
	union, ok := revisionUnionRequest(c)
	return ok && revisionEvidence(union, r, envelope["document"], true, true)
}
