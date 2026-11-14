package procurement

import (
	"context"
	"errors"
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

func (*stalePriceWorkerStore) FinishAction(context.Context, int64, string, int64, ActionExecution, error) (bool, error) {
	panic("stale action must not finish")
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

type fencedGroupWorkerStore struct {
	finished []ActionItem
	guardErr error
	applied  bool
}

func (*fencedGroupWorkerStore) ClaimActionGroup(context.Context, string) ([]ActionItem, error) {
	return []ActionItem{{ID: 1, Channel: "wb", LockOwner: "owner-a", LockToken: 7}, {ID: 2, Channel: "wb", LockOwner: "owner-b", LockToken: 9}}, nil
}
func (s *fencedGroupWorkerStore) GuardClaimedPriceActions(context.Context, []ActionItem) (bool, error) {
	return s.guardErr == nil, s.guardErr
}
func (s *fencedGroupWorkerStore) FinishAction(_ context.Context, id int64, owner string, token int64, _ ActionExecution, _ error) (bool, error) {
	s.finished = append(s.finished, ActionItem{ID: id, LockOwner: owner, LockToken: token})
	return s.applied, nil
}

type orderedGroupExecutor struct{ calls int }

func (e *orderedGroupExecutor) ExecuteGroup(context.Context, []ActionItem) []ActionOutcome {
	e.calls++
	return []ActionOutcome{{ItemID: 2, Result: ActionExecution{Completed: true}}, {ItemID: 1, Err: errors.New("provider failure")}}
}
func TestActionWorkerUsesPerItemFenceAndDoesNotReplayStaleFinishes(t *testing.T) {
	store := &fencedGroupWorkerStore{applied: false}
	executor := &orderedGroupExecutor{}
	NewActionWorker(store, executor, slog.New(slog.NewTextHandler(io.Discard, nil))).runOne(context.Background())
	if executor.calls != 1 || len(store.finished) != 2 || store.finished[0].ID != 2 || store.finished[0].LockOwner != "owner-b" || store.finished[0].LockToken != 9 || store.finished[1].ID != 1 || store.finished[1].LockToken != 7 {
		t.Fatalf("fencing/replay changed: calls=%d finish=%+v", executor.calls, store.finished)
	}
}
func TestActionWorkerFailsClosedWhenPriceGuardErrors(t *testing.T) {
	store := &fencedGroupWorkerStore{guardErr: errors.New("database unavailable")}
	executor := &orderedGroupExecutor{}
	NewActionWorker(store, executor, slog.New(slog.NewTextHandler(io.Discard, nil))).runOne(context.Background())
	if executor.calls != 0 || len(store.finished) != 0 {
		t.Fatal("failed last-boundary guard still wrote/finished actions")
	}
}
