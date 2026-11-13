package ordersale

import (
	"context"
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
