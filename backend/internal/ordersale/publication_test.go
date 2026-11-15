package ordersale

import (
	"context"
	"encoding/json"
	"testing"
)

func TestUnknownCreationNeverExposesRetry(t *testing.T) {
	for _, status := range []string{"failed", "needs_attention"} {
		if retrySafe(status, Progress{Stage: "reconcile-create", Unknown: true}) {
			t.Fatal("unknown create without ID offered retry", status)
		}
	}
	if _, safe := recoverProgress(Progress{Stage: "create"}); safe {
		t.Fatal("unknown creation could be repeated")
	}
}
func TestStagingProviderCannotPublishOrProveLiveStock(t *testing.T) {
	request, _ := liveStockProofFixture()
	if _, err := (MockProvider{}).Step(context.Background(), Request{ExecutionMode: "live"}, Progress{Stage: "create"}); err == nil {
		t.Fatal("test provider accepted live request")
	}
	result := Result{Progress: Progress{Stage: "verify-post", DocumentID: "123", DocumentUUID: "00000000-0000-4000-8000-000000000001"}, State: "posted", StockVerified: true, StockProof: json.RawMessage(`{"test":true}`)}
	if validStockProof(request, result) {
		t.Fatal("test evidence accepted as a real stock deduction")
	}
	result.Progress.DocumentID = "STG-SALE-123"
	if validStockProof(request, result) {
		t.Fatal("test document accepted as real")
	}
	result.Progress.DocumentID = "123"
	result.StockProof = json.RawMessage(`{}`)
	if validStockProof(request, result) {
		t.Fatal("empty stock evidence accepted")
	}
}

func TestOnlyUnwrittenInspectCanRetryAttention(t *testing.T) {
	if !retrySafe("needs_attention", Progress{Stage: "inspect", DesiredState: "posted"}) {
		t.Fatal("initial read cannot retry")
	}
	cases := []Progress{
		{Stage: "inspect", Unknown: true}, {Stage: "inspect", CreateAttempted: true},
		{Stage: "inspect", PostAttempted: true}, {Stage: "inspect", DocumentID: "123"},
		{Stage: "inspect", DocumentUUID: "uuid"}, {Stage: "inspect", OwnedRowIDs: []int64{1}},
		{Stage: "inspect", Native: json.RawMessage(`{}`)}, {Stage: "inspect", IdentityConflict: true},
		{Stage: "inspect", ActiveRevisionID: 1}, {Stage: "inspect", RevisionPostAttempted: true},
		{Stage: "inspect", RevisionUnpostAttempted: true}, {Stage: "create"}, {Stage: "discover"},
	}
	for _, p := range cases {
		if retrySafe("needs_attention", p) {
			t.Fatalf("unsafe initial retry: %+v", p)
		}
	}
}
