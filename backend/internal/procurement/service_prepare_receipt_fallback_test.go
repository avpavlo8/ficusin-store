package procurement

import (
	"context"
	"errors"
	"testing"
)

type failingSabyReceiptExecutor struct{}

func (failingSabyReceiptExecutor) Configured(channel string) bool { return channel == "saby" }
func (failingSabyReceiptExecutor) Execute(context.Context, ActionItem) (ActionExecution, error) {
	return ActionExecution{}, nil
}
func (failingSabyReceiptExecutor) RefreshSabyCatalog(context.Context) (ChannelLinkResult, error) {
	return ChannelLinkResult{}, errors.New("temporary Saby timeout")
}

func TestPrepareReceiptFallsBackToCachedSabySnapshot(t *testing.T) {
	store := &storeStub{}
	service := NewServiceWithExecutor(store, failingSabyReceiptExecutor{})
	batch, err := service.PrepareBatch(context.Background(), Actor{}, 18, "receipt", []string{"saby_receipt"})
	if err != nil {
		t.Fatalf("PrepareBatch must not fail only because Saby catalog refresh failed: %v", err)
	}
	if batch.Kind != "receipt" {
		t.Fatalf("batch=%+v", batch)
	}
}
