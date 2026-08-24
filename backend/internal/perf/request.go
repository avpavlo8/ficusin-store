package perf

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
)

type requestMetricsKey struct{}

// RequestMetrics contains only aggregate timings. SQL text and arguments are
// deliberately never retained: they can contain customer data or secrets.
type RequestMetrics struct {
	queries atomic.Int64
	sqlNS   atomic.Int64
}

func WithRequest(ctx context.Context) (context.Context, *RequestMetrics) {
	metrics := &RequestMetrics{}
	return context.WithValue(ctx, requestMetricsKey{}, metrics), metrics
}

func (metrics *RequestMetrics) Snapshot() (queries int64, sql time.Duration) {
	return metrics.queries.Load(), time.Duration(metrics.sqlNS.Load())
}

type queryStartedKey struct{}

// QueryTracer is attached to pgx once for the whole pool. It records count and
// elapsed time only when an HTTP request installed RequestMetrics in context.
type QueryTracer struct{}

func (QueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	if metrics, ok := ctx.Value(requestMetricsKey{}).(*RequestMetrics); ok {
		metrics.queries.Add(1)
		return context.WithValue(ctx, queryStartedKey{}, time.Now())
	}
	return ctx
}

func (QueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	metrics, measured := ctx.Value(requestMetricsKey{}).(*RequestMetrics)
	started, startedOK := ctx.Value(queryStartedKey{}).(time.Time)
	if measured && startedOK {
		metrics.sqlNS.Add(time.Since(started).Nanoseconds())
	}
}
