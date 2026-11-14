package procurement

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type salesStoreStub struct {
	replaced map[string][]SalesRecord
	states   map[string]string
	modes    map[string][]string
	finished []salesFinish
	stale    bool
}

type salesFinish struct {
	claim    SyncClaim
	rows     int
	from, to time.Time
	latest   *time.Time
	retry    time.Duration
	err      error
}

func (*salesStoreStub) RequestIntegrationSync(context.Context, string, string) (IntegrationSyncStatus, error) {
	panic("worker must claim queued work")
}
func (*salesStoreStub) ListIntegrationSync(context.Context) ([]IntegrationSyncStatus, error) {
	panic("worker must claim queued work")
}
func (stub *salesStoreStub) ClaimIntegrationSync(_ context.Context, channel, resource, owner string, _ time.Duration) (*SyncClaim, error) {
	mode := "current"
	if modes, ok := stub.modes[channel]; ok {
		if len(modes) == 0 || modes[0] == "" {
			if len(modes) > 0 {
				stub.modes[channel] = modes[1:]
			}
			return nil, nil
		}
		mode = modes[0]
		stub.modes[channel] = modes[1:]
	}
	return &SyncClaim{Channel: channel, Resource: resource, Owner: owner, Token: 1, Mode: mode}, nil
}
func (stub *salesStoreStub) FinishIntegrationSync(_ context.Context, claim SyncClaim, rows int, from, to time.Time, latest *time.Time, retry time.Duration, err error) (bool, error) {
	stub.finished = append(stub.finished, salesFinish{claim, rows, from, to, latest, retry, err})
	return !stub.stale, nil
}

func (stub *salesStoreStub) RefreshSiteSales(context.Context, time.Time, time.Time) (int, error) {
	stub.states["site"] = "ok"
	return 1, nil
}
func (stub *salesStoreStub) ReplaceSales(_ context.Context, channel string, _, _ time.Time, records []SalesRecord) (int, error) {
	stub.replaced[channel] = records
	stub.states[channel] = "ok"
	return len(records), nil
}
func (stub *salesStoreStub) MarkSalesSync(_ context.Context, channel, status string, _ error) error {
	stub.states[channel] = status
	return nil
}

type salesSourceStub struct{}

func (salesSourceStub) Configured(string) bool { return true }
func (salesSourceStub) FetchSales(_ context.Context, channel string, from, _ time.Time) ([]SalesRecord, error) {
	return []SalesRecord{{Date: from, ExternalID: channel + "-1", Units: 2}}, nil
}

func TestSalesWorkerLeavesWildberriesToItsDedicatedMirror(t *testing.T) {
	store := &salesStoreStub{replaced: map[string][]SalesRecord{}, states: map[string]string{}}
	worker := NewSalesWorker(store, salesSourceStub{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	worker.now = func() time.Time { return time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC) }
	worker.run(context.Background())
	if store.states["site"] != "ok" || store.states["ozon"] != "ok" {
		t.Fatalf("states = %+v", store.states)
	}
	if _, called := store.replaced["wb"]; called || len(store.replaced["ozon"]) != 1 {
		t.Fatalf("replaced = %+v", store.replaced)
	}
}

type salesRetryError struct{}

func (salesRetryError) Error() string             { return "provider rate limited" }
func (salesRetryError) RetryDelay() time.Duration { return 37 * time.Second }

type failedSalesSource struct{}

func (failedSalesSource) Configured(channel string) bool { return channel == "ozon" }
func (failedSalesSource) FetchSales(context.Context, string, time.Time, time.Time) ([]SalesRecord, error) {
	return nil, salesRetryError{}
}
func (failedSalesSource) DescribeSalesFailure(_ context.Context, _ string, _, _ time.Time, err error) error {
	return fmt.Errorf("account diagnostics: %w", err)
}
func TestSalesWorkerFinishesClaimWithDetailedFailureAndCooldown(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(fmt.Sprintf("stale=%v", stale), func(t *testing.T) {
			store := &salesStoreStub{replaced: map[string][]SalesRecord{}, states: map[string]string{}, stale: stale}
			worker := NewSalesWorker(store, failedSalesSource{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			worker.run(context.Background())
			if len(store.finished) != 1 {
				t.Fatalf("claim was not finished: %+v", store.finished)
			}
			result := store.finished[0]
			if result.claim.Channel != "ozon" || result.claim.Resource != "sales" || result.claim.Owner != worker.owner || result.claim.Token != 1 || result.rows != 0 || result.latest != nil || result.retry != 37*time.Second || result.err == nil || !strings.Contains(result.err.Error(), "account diagnostics") || len(store.replaced) != 0 {
				t.Fatalf("failure lost durable context: %+v", result)
			}
			want := "error"
			if stale {
				want = "running"
			}
			if store.states["ozon"] != want {
				t.Fatalf("stale finisher overwrote successor state: %+v", store.states)
			}
		})
	}
}
