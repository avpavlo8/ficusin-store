package procurement

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type durableSalesTestStore struct {
	*salesStoreStub
	queue *PostgresStore
}

func (s *durableSalesTestStore) ClaimIntegrationSync(ctx context.Context, c, r, o string, l time.Duration) (*SyncClaim, error) {
	return s.queue.ClaimIntegrationSync(ctx, c, r, o, l)
}
func (s *durableSalesTestStore) FinishIntegrationSync(ctx context.Context, c SyncClaim, n int, f, to time.Time, latest *time.Time, retry time.Duration, e error) (bool, error) {
	return s.queue.FinishIntegrationSync(ctx, c, n, f, to, latest, retry, e)
}

type durableWindowSource struct {
	windowSourceStub
	err error
}

func (s *durableWindowSource) FetchSales(ctx context.Context, c string, f, to time.Time) ([]SalesRecord, error) {
	rows, _ := s.windowSourceStub.FetchSales(ctx, c, f, to)
	if s.err != nil {
		return nil, s.err
	}
	return rows, nil
}

func TestSalesWorkerUsesDurableWindowsAndCooldownAcrossRestartOnDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	bootstrap, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Close()
	schema := fmt.Sprintf("sales_worker_%d", time.Now().UnixNano())
	if _, err = bootstrap.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE") // Disposable test fixture only.
	if _, err = bootstrap.Exec(ctx, "CREATE TABLE "+schema+".procurement_integration_sync_state(LIKE public.procurement_integration_sync_state INCLUDING DEFAULTS INCLUDING CONSTRAINTS INCLUDING INDEXES)"); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	exec := func(sql string) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO procurement_integration_sync_state(channel,resource,next_attempt_at,next_deep_at) VALUES('ozon','sales',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`)
	store := &durableSalesTestStore{salesStoreStub: &salesStoreStub{replaced: map[string][]SalesRecord{}, states: map[string]string{}}, queue: NewPostgresStore(pool)}
	source := &durableWindowSource{}
	restart := func() { NewSalesWorker(store, source, slog.New(slog.NewTextHandler(io.Discard, nil))).run(ctx) }
	restart()
	var state string
	var deepScheduled, coverage bool
	if err = pool.QueryRow(ctx, `SELECT status,next_deep_at>CURRENT_TIMESTAMP+INTERVAL '6 days',period_from IS NOT NULL AND period_to IS NOT NULL AND latest_event_at IS NOT NULL AND rows_synced=1 FROM procurement_integration_sync_state`).Scan(&state, &deepScheduled, &coverage); err != nil || state != "ok" || !deepScheduled || !coverage {
		t.Fatalf("deep finish lost durable coverage: %s %v %v %v", state, deepScheduled, coverage, err)
	}
	restart() // A new worker must respect the stored next attempt, not reread a year.
	if len(source.days) != 1 {
		t.Fatal("restart bypassed the durable schedule")
	}
	exec(`UPDATE procurement_integration_sync_state SET next_attempt_at=CURRENT_TIMESTAMP,next_deep_at=CURRENT_TIMESTAMP+INTERVAL '1 day'`)
	source.err = salesRetryError{}
	restart()
	var cooled bool
	if err = pool.QueryRow(ctx, `SELECT status,cooldown_until>CURRENT_TIMESTAMP+INTERVAL '30 seconds' FROM procurement_integration_sync_state`).Scan(&state, &cooled); err != nil || state != "error" || !cooled {
		t.Fatalf("rate limit was not persisted: %s %v %v", state, cooled, err)
	}
	exec(`UPDATE procurement_integration_sync_state SET next_attempt_at=CURRENT_TIMESTAMP`)
	restart()
	if len(source.days) != 2 {
		t.Fatal("restart bypassed the persisted provider cooldown")
	}
	exec(`UPDATE procurement_integration_sync_state SET next_attempt_at=CURRENT_TIMESTAMP,cooldown_until=NULL`)
	source.err = nil
	restart()
	exec(`UPDATE procurement_integration_sync_state SET next_attempt_at=CURRENT_TIMESTAMP,next_deep_at=CURRENT_TIMESTAMP`)
	restart()
	want := []int{salesHistoryDays, salesRefreshDays, salesRefreshDays, salesHistoryDays}
	if len(source.days) != len(want) {
		t.Fatalf("unexpected provider reads: %v", source.days)
	}
	for i, d := range want {
		if source.days[i] != d {
			t.Fatalf("claim window ignored: got %v want %v", source.days, want)
		}
	}
}
