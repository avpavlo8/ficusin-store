package saby

import (
	"context"
	"testing"
	"time"
)

func TestStockSnapshotCollectionTimeCannotBeReplacedByUploadTime(t *testing.T) {
	now := time.Date(2026, 11, 13, 18, 0, 0, 0, time.UTC)
	if observed, err := stockSnapshotObservedAt(context.Background(), now); err != nil || observed != nil {
		t.Fatalf("legacy caller invented a collection watermark: %v %v", observed, err)
	}
	started := now.Add(-time.Minute)
	observed, err := stockSnapshotObservedAt(WithStockSnapshotObservedAt(context.Background(), started), now)
	if err != nil || observed == nil || !observed.Equal(started) {
		t.Fatalf("collection start was lost: %v %v", observed, err)
	}
	for _, invalid := range []time.Time{{}, now.Add(time.Second), now.Add(-2*time.Hour - time.Second)} {
		if _, err := stockSnapshotObservedAt(WithStockSnapshotObservedAt(context.Background(), invalid), now); err == nil {
			t.Fatalf("accepted unsafe collection watermark %v", invalid)
		}
	}
}
