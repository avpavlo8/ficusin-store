package saby

import (
	"context"
	"errors"
	"time"
)

type stockSnapshotKey struct{}

// WithStockSnapshotObservedAt records when collection began, before the first
// provider GET. Upload/commit time cannot prove a snapshot includes a posting.
func WithStockSnapshotObservedAt(ctx context.Context, observed time.Time) context.Context {
	return context.WithValue(ctx, stockSnapshotKey{}, observed.UTC())
}

func stockSnapshotObservedAt(ctx context.Context, now time.Time) (*time.Time, error) {
	observed, ok := ctx.Value(stockSnapshotKey{}).(time.Time)
	if !ok {
		return nil, nil // Older callers cannot release unreflected realization holds.
	}
	if observed.IsZero() || observed.After(now) || observed.Before(now.Add(-2*time.Hour)) {
		return nil, errors.New("invalid Saby stock observation time")
	}
	return &observed, nil
}
