package procurement

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

type stalePriceWorkerStore struct {
	storeStub
	checked bool
}

func (store *stalePriceWorkerStore) ClaimActionGroup(context.Context, string) ([]ActionItem, error) {
	return []ActionItem{{ID: 1, Channel: "wb", LockToken: 1}}, nil
}

func (store *stalePriceWorkerStore) GuardClaimedPriceActions(_ context.Context, items []ActionItem) (bool, error) {
	store.checked = len(items) == 1 && items[0].Channel == "wb"
	return false, nil
}

type stalePriceWorkerExecutor struct{ uploads int }

func (*stalePriceWorkerExecutor) Configured(string) bool { return true }
func (*stalePriceWorkerExecutor) Execute(context.Context, ActionItem) (ActionExecution, error) {
	panic("stale price must not be exported")
}
func (executor *stalePriceWorkerExecutor) ExecuteGroup(context.Context, []ActionItem) []ActionOutcome {
	executor.uploads++
	return nil
}

func TestActionWorkerDoesNotUploadStaleMarketplacePrice(t *testing.T) {
	store := &stalePriceWorkerStore{}
	executor := &stalePriceWorkerExecutor{}
	worker := NewActionWorker(store, executor, slog.New(slog.NewTextHandler(io.Discard, nil)))
	worker.runOne(context.Background())
	if !store.checked || executor.uploads != 0 {
		t.Fatalf("price guard checked=%v uploads=%d", store.checked, executor.uploads)
	}
}
