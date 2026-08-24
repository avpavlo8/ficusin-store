package perf

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestQueryTracerRecordsOnlyAggregateTiming(t *testing.T) {
	ctx, metrics := WithRequest(context.Background())
	tracer := QueryTracer{}
	ctx = tracer.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: "SELECT secret FROM private_table", Args: []any{"sensitive"}})
	time.Sleep(time.Millisecond)
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
	queries, duration := metrics.Snapshot()
	if queries != 1 || duration <= 0 {
		t.Fatalf("queries=%d duration=%s", queries, duration)
	}
}
