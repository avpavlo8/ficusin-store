package ordersale

import (
	"context"
	"encoding/json"
	"fmt"
)

// MockProvider is a deterministic staging-only state machine. It has no client,
// transport or external URL and rejects every request whose mode is not mock.
type MockProvider struct{}

func (MockProvider) Step(_ context.Context, r Request, p Progress) (Result, error) {
	if r.ExecutionMode != "mock" {
		return Result{}, fmt.Errorf("staging realization provider rejects live requests")
	}
	next := Result{Progress: p, State: "queued", Message: "Тестовый обмен СБИС; реальные остатки не меняются"}
	if p.DesiredState == "cancelled" {
		return mockCancel(r, p)
	}
	switch p.Stage {
	case "inspect":
		if !r.Event.CanPrepare || r.SiteWarehouseID <= 0 {
			return Result{Progress: p, State: "needs_attention", Message: "Проверьте состав, связь товаров и выбранный склад"}, nil
		}
		next.Progress.Stage = "create"
	case "create":
		next.Progress.DocumentID = fmt.Sprintf("STG-SALE-%d", r.IntentID)
		next.Progress.DocumentUUID = r.OperationKey
		next.Progress.Stage = "fill"
	case "reconcile-create":
		next.Progress.Stage = "fill"
		next.Progress.Unknown = false
	case "fill", "reconcile-fill":
		for i := range r.Event.Lines {
			next.Progress.OwnedRowIDs = appendUniqueMockID(next.Progress.OwnedRowIDs, int64(i+1))
		}
		next.Progress.Stage = "post"
		next.Progress.Unknown = false
	case "post", "reconcile-post":
		next.Progress.Stage = "verify-post"
		next.Progress.Unknown = false
	case "verify-post":
		next.State = "posted"
		next.Progress.Unknown = false
		next.StockVerified = true
		next.Message = "Тестовая реализация завершена. Документ СБИС и реальные списания не создавались."
		var err error
		next.StockProof, err = mockStockProof(r, next.Progress)
		if err != nil {
			return Result{}, err
		}
	default:
		return Result{Progress: p, State: "needs_attention", Message: "Неизвестный этап тестового обмена"}, nil
	}
	return next, nil
}

func appendUniqueMockID(ids []int64, id int64) []int64 {
	for _, old := range ids {
		if old == id {
			return ids
		}
	}
	return append(ids, id)
}
func mockCancel(r Request, p Progress) (Result, error) {
	result := Result{Progress: p, State: "cancelled", CancellationVerified: true, Message: "Тестовая отмена: реальный документ и остатки СБИС не изменялись"}
	result.Progress.Stage = "cancelled"
	result.Progress.Unknown = false
	if !p.CreateAttempted && p.DocumentID == "" {
		result.CancellationProof = []byte(`{"kind":"no_remote"}`)
		return result, nil
	}
	if p.DocumentID == "" {
		result.Progress.DocumentID = fmt.Sprintf("STG-SALE-%d", r.IntentID)
		result.Progress.DocumentUUID = r.OperationKey
	}
	raw, err := mockStockProof(r, result.Progress)
	if err != nil {
		return Result{}, err
	}
	var proof StockProof
	if err = json.Unmarshal(raw, &proof); err != nil {
		return Result{}, err
	}
	proof.Posted = false
	proof.HasEntries = false
	rows := []StockProofLine{}
	for _, line := range proof.Lines {
		for _, id := range p.OwnedRowIDs {
			if line.ID == id {
				line.Closed = false
				rows = append(rows, line)
			}
		}
	}
	proof.Lines = rows
	kind := "unposted_draft"
	if p.PostAttempted {
		kind = "unposted_full"
	}
	result.CancellationProof, err = json.Marshal(CancellationProof{Kind: kind, Document: &proof})
	return result, err
}
