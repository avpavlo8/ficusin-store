package ordersale

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type revisionStockFixture struct {
	t *testing.T
	p *pgxpool.Pool
}

func newRevisionStockFixture(t *testing.T) revisionStockFixture {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") || u.Path != "/ficusin_order_sale_test" {
		t.Fatal("requires dedicated loopback ficusin_order_sale_test")
	}
	ctx := context.Background()
	control, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := pgx.Identifier{fmt.Sprintf("revision_stock_%d", time.Now().UnixNano())}.Sanitize()
	if _, err = control.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		p.Close()
		control.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		control.Close(context.Background())
	})
	f := revisionStockFixture{t, p}
	f.exec(`CREATE TABLE orders(id BIGINT PRIMARY KEY);
 CREATE TABLE shipment_offers(id BIGINT PRIMARY KEY);
 CREATE TABLE warehouses(id BIGINT PRIMARY KEY);INSERT INTO warehouses VALUES(1);
 CREATE TABLE customers(id BIGINT PRIMARY KEY);
 CREATE TABLE order_items(id BIGINT PRIMARY KEY,order_id BIGINT,variant_id BIGINT);
 CREATE TABLE inventory(id BIGSERIAL PRIMARY KEY,warehouse_id BIGINT,variant_id BIGINT,available_qty INTEGER,reserved_qty INTEGER DEFAULT 0,synced_at TIMESTAMPTZ DEFAULT NOW(),UNIQUE(warehouse_id,variant_id));`)
	for _, name := range []string{"20261113_order_sale_intents.sql", "20261113_order_sale_publications.sql", "20261113_order_sale_publications_buyer.sql", "20261113_order_sale_publications_lifecycle.sql", "20261113_order_sale_zz_stock_visibility.sql", "20261114_order_sale_revisions.sql", "20261114_order_sale_z_revision_stock.sql"} {
		raw, e := os.ReadFile("../../../timeweb/migrations/" + name)
		if e != nil {
			t.Fatal(e)
		}
		f.exec(string(raw))
	}
	return f
}
func (f revisionStockFixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, e := f.p.Exec(context.Background(), q, args...); e != nil {
		f.t.Fatal(e)
	}
}
func (f revisionStockFixture) value(q string, args ...any) int64 {
	f.t.Helper()
	var n int64
	if e := f.p.QueryRow(context.Background(), q, args...).Scan(&n); e != nil {
		f.t.Fatal(e)
	}
	return n
}
func (f revisionStockFixture) stock(variant, qty int64, at string) {
	f.t.Helper()
	f.exec(`INSERT INTO inventory(warehouse_id,variant_id,available_qty,stock_observed_at)VALUES(1,$1,$2,$3::TIMESTAMPTZ)ON CONFLICT(warehouse_id,variant_id)DO UPDATE SET available_qty=EXCLUDED.available_qty,stock_observed_at=EXCLUDED.stock_observed_at`, variant, qty, at)
}
func (f revisionStockFixture) origin(id, variant, qty int64, at, mode string, ledger bool) {
	f.t.Helper()
	f.exec(`INSERT INTO orders VALUES($1)`, id)
	f.exec(`INSERT INTO order_items VALUES($1,$1,$2)`, id, variant)
	f.exec(`INSERT INTO order_sale_intents(id,event_key,order_id,source,snapshot,warehouse_id,event_trigger,created_at)VALUES($1,'order:'||$1::BIGINT::TEXT,$1,'automatic','{}',1,'created',$2::TIMESTAMPTZ)`, id, at)
	f.exec(`INSERT INTO order_sale_intent_lines(intent_id,order_item_id,variant_id,quantity)VALUES($1,$1,$2,$3)`, id, variant, qty)
	f.exec(`INSERT INTO order_sale_publications(intent_id,execution_mode,request,request_hash)VALUES($1,$2,jsonb_build_object('executionMode',$2::TEXT),'fixed-test-origin')`, id, mode)
	if ledger {
		f.revision(id, id*100, variant, qty)
		f.exec(`UPDATE order_sale_intents SET base_revision_id=$2,desired_revision_id=$2 WHERE id=$1`, id, id*100)
		f.layer(id, id*100, variant, qty, at)
	}
}
func (f revisionStockFixture) revision(intent, id, variant, qty int64) {
	f.t.Helper()
	f.exec(`INSERT INTO order_sale_revisions(id,intent_id,revision_number,request,request_hash,content_hash,order_items_snapshot)VALUES($1,$2,$1,'{}','test','test','[]')`, id, intent)
	if qty > 0 {
		f.exec(`INSERT INTO order_sale_revision_lines(revision_id,order_item_id,variant_id,name,sku,saby_id,quantity,unit_price,line_snapshot)VALUES($1,$2,$3,'Fixture','fixture','3899',$4,1,'{}')`, id, intent, variant, qty)
	}
}
func (f revisionStockFixture) layer(intent, revision, variant, qty int64, at string) {
	f.t.Helper()
	f.exec(`INSERT INTO order_sale_hold_layers(intent_id,revision_id,variant_id,quantity,admitted_at)VALUES($1,$2,$3,$4,$5::TIMESTAMPTZ)`, intent, revision, variant, qty, at)
}
func (f revisionStockFixture) effect(intent, revision, variant, qty int64, op string, posted bool, at string) int64 {
	f.t.Helper()
	id := f.value(`INSERT INTO order_sale_revision_effects(intent_id,revision_id,operation,posted,proof,verified_at)VALUES($1,$2,$3,$4,'{"fixedFixture":true}',$5::TIMESTAMPTZ)RETURNING id`, intent, revision, op, posted, at)
	if qty > 0 {
		f.exec(`INSERT INTO order_sale_revision_effect_lines(effect_id,variant_id,quantity)VALUES($1,$2,$3)`, id, variant, qty)
	}
	if op == "origin_post" {
		f.exec(`UPDATE order_sale_publications SET stock_verified=TRUE,stock_verified_at=$2::TIMESTAMPTZ WHERE intent_id=$1`, intent, at)
	}
	return id
}
func (f revisionStockFixture) expect(variant, free int64, owned map[int64]int64) {
	f.t.Helper()
	got := f.value(`SELECT available_qty FROM order_sale_inventory WHERE warehouse_id=1 AND variant_id=$1`, variant)
	if got != free {
		f.t.Fatalf("variant%d public=%d want%d", variant, got, free)
	}
	for order, want := range owned {
		actual := f.value(`SELECT e.available_qty-e.reserved_qty+COALESCE(order_sale_owned_quantity($1,1,$2),0) FROM order_sale_inventory e WHERE warehouse_id=1 AND variant_id=$2`, order, variant)
		if actual != want {
			f.t.Fatalf("variant%d order%d available=%d want%d", variant, order, actual, want)
		}
	}
}

func TestRevisionStockLedgerPostgres(t *testing.T) {
	const a = "2026-01-01T00:00:01Z"
	const b = "2026-01-01T00:00:02Z"
	const c = "2026-01-01T00:00:03Z"
	const d = "2026-01-01T00:00:04Z"
	const e = "2026-01-01T00:00:05Z"
	const g = "2026-01-01T00:00:06Z"
	const h = "2026-01-01T00:00:07Z"
	const j = "2026-01-01T00:00:08Z"
	const k = "2026-01-01T00:00:09Z"
	t.Run("old cohort fallback preserves FIFO", func(t *testing.T) {
		f := newRevisionStockFixture(t)
		f.stock(1, 1, a)
		f.origin(10, 1, 1, a, "live", false)
		f.origin(20, 1, 1, b, "live", false)
		f.expect(1, -1, map[int64]int64{10: 1, 20: 0})
	})
	t.Run("an added unit never jumps an existing order", func(t *testing.T) {
		f := newRevisionStockFixture(t)
		f.stock(1, 2, a)
		f.origin(10, 1, 1, a, "live", true)
		f.origin(20, 1, 1, b, "live", true)
		f.revision(10, 1001, 1, 2)
		f.layer(10, 1001, 1, 1, c)
		f.exec(`UPDATE order_sale_intents SET desired_revision_id=1001 WHERE id=10`)
		f.expect(1, -1, map[int64]int64{10: 1, 20: 1})
	})
	t.Run("editing can use its held unit and genuinely free stock", func(t *testing.T) {
		f := newRevisionStockFixture(t)
		f.stock(1, 5, a)
		f.origin(10, 1, 1, a, "live", true)
		f.origin(20, 1, 2, b, "live", true)
		f.expect(1, 2, map[int64]int64{10: 3, 20: 4})
		f.exec(`UPDATE inventory SET reserved_qty=1 WHERE variant_id=1`)
		f.expect(1, 2, map[int64]int64{10: 2, 20: 3})
	})
	t.Run("same document cycles use each snapshot frontier and release LIFO", func(t *testing.T) {
		f := newRevisionStockFixture(t)
		f.stock(1, 2, a)
		f.origin(10, 1, 1, a, "live", true)
		f.effect(10, 1000, 1, 1, "origin_post", true, b)
		f.stock(1, 1, c)
		f.expect(1, 1, map[int64]int64{10: 2})
		f.revision(10, 1001, 1, 2)
		f.layer(10, 1001, 1, 1, c)
		f.exec(`UPDATE order_sale_intents SET desired_revision_id=1001,active_revision_id=1001 WHERE id=10`)
		f.effect(10, 1001, 1, 1, "revision_unpost", false, d)
		f.expect(1, 0, map[int64]int64{10: 2})
		f.effect(10, 1001, 1, 2, "revision_post", true, e)
		f.stock(1, 0, g)
		f.expect(1, 0, map[int64]int64{10: 2})
		f.revision(10, 1002, 1, 1)
		f.exec(`UPDATE order_sale_intents SET desired_revision_id=1002,active_revision_id=1002 WHERE id=10`)
		f.effect(10, 1002, 1, 2, "revision_unpost", false, h)
		f.exec(`SELECT order_sale_refresh_hold_releases(10)`)
		if n := f.value(`SELECT COALESCE(SUM(quantity),0) FROM order_sale_hold_releases`); n != 0 {
			t.Fatal("released before fresh collection", n)
		}
		f.stock(1, 2, j)
		f.expect(1, 1, map[int64]int64{10: 2})
		if n := f.value(`SELECT SUM(r.quantity) FROM order_sale_hold_releases r JOIN order_sale_hold_layers l ON l.id=r.layer_id WHERE l.revision_id=1001`); n != 1 {
			t.Fatal("newest unit was not released", n)
		}
		f.effect(10, 1002, 1, 1, "revision_post", true, k)
		f.expect(1, 1, map[int64]int64{10: 2})
		f.stock(1, 1, "2026-01-01T00:00:10Z")
		f.expect(1, 1, map[int64]int64{10: 2})
		f.exec(`UPDATE order_sale_intents SET cancellation_verified=TRUE WHERE id=10`)
		f.effect(10, 1002, 1, 1, "cancel_unpost", false, "2026-01-01T00:00:11Z")
		f.exec(`SELECT order_sale_refresh_hold_releases(10)`)
		f.expect(1, 1, nil)
		f.stock(1, 2, "2026-01-01T00:00:12Z")
		f.expect(1, 2, map[int64]int64{10: 2})
		if n := f.value(`SELECT COUNT(*) FROM order_sale_revision_effects WHERE intent_id=10`); n != 6 {
			t.Fatal("effect history changed", n)
		}
	})
	t.Run("an older unpost snapshot cannot free the newly reposted quantity", func(t *testing.T) {
		f := newRevisionStockFixture(t)
		f.stock(1, 2, a)
		f.origin(10, 1, 1, a, "live", true)
		f.effect(10, 1000, 1, 1, "origin_post", true, b)
		f.revision(10, 1001, 1, 2)
		f.layer(10, 1001, 1, 1, c)
		f.exec(`UPDATE order_sale_intents SET desired_revision_id=1001,active_revision_id=1001 WHERE id=10`)
		f.effect(10, 1001, 1, 1, "revision_unpost", false, d)
		f.effect(10, 1001, 1, 2, "revision_post", true, g)
		f.stock(1, 2, e)
		f.expect(1, 0, map[int64]int64{10: 2})
		if n := f.value(`SELECT COUNT(*)FROM order_sale_hold_releases`); n != 0 {
			t.Fatal("late intermediate snapshot released hold")
		}
	})
	t.Run("a first posting of the edited plan never falls back to the original quantity", func(t *testing.T) {
		f := newRevisionStockFixture(t)
		f.stock(1, 2, a)
		f.origin(10, 1, 1, a, "live", true)
		f.revision(10, 1001, 1, 2)
		f.layer(10, 1001, 1, 1, b)
		f.exec(`UPDATE order_sale_intents SET desired_revision_id=1001,active_revision_id=1001 WHERE id=10`)
		// Publication acknowledgement can precede the effect-ledger timestamp.
		// Collection between them must not infer an original one-unit debit.
		f.exec(`UPDATE order_sale_publications SET stock_verified=TRUE,stock_verified_at=$1::TIMESTAMPTZ WHERE intent_id=10`, b)
		f.effect(10, 1001, 1, 2, "revision_post", true, d)
		f.stock(1, 2, c)
		f.expect(1, 0, map[int64]int64{10: 2})
	})
	t.Run("old variant proof never credits the newly added variant", func(t *testing.T) {
		f := newRevisionStockFixture(t)
		f.stock(1, 0, c)
		f.stock(2, 0, c)
		f.origin(10, 1, 1, a, "live", true)
		f.effect(10, 1000, 1, 1, "origin_post", true, b)
		f.revision(10, 1001, 2, 1)
		f.layer(10, 1001, 2, 1, d)
		f.exec(`UPDATE order_sale_intents SET desired_revision_id=1001,active_revision_id=1001 WHERE id=10`)
		f.expect(2, -1, map[int64]int64{10: 0})
	})
	t.Run("only a verified undone quantity grants temporary owner credit", func(t *testing.T) {
		f := newRevisionStockFixture(t)
		f.stock(1, 0, a)
		f.origin(10, 1, 1, a, "live", true)
		f.revision(10, 1001, 1, 1)
		f.exec(`UPDATE order_sale_intents SET desired_revision_id=1001,active_revision_id=1001 WHERE id=10`)
		f.effect(10, 1001, 1, 0, "revision_unpost", false, b)
		f.expect(1, -1, map[int64]int64{10: 0})
	})
	t.Run("mock cancellation restores the locally held stock without a native snapshot", func(t *testing.T) {
		f := newRevisionStockFixture(t)
		f.stock(1, 2, a)
		f.origin(10, 1, 1, a, "mock", true)
		f.effect(10, 1000, 1, 1, "origin_post", true, b)
		f.expect(1, 1, map[int64]int64{10: 2})
		f.exec(`UPDATE order_sale_intents SET cancellation_verified=TRUE WHERE id=10`)
		f.effect(10, 1000, 1, 1, "cancel_unpost", false, c)
		f.exec(`SELECT order_sale_refresh_hold_releases(10)`)
		f.expect(1, 2, nil)
	})
	t.Run("verified cancellation before any writer releases its purely local hold", func(t *testing.T) {
		f := newRevisionStockFixture(t)
		f.stock(1, 1, a)
		f.origin(10, 1, 1, a, "live", true)
		f.expect(1, 0, nil)
		f.exec(`UPDATE order_sale_publications SET status='cancelled',desired_state='cancelled',cancellation_verified=TRUE,cancellation_verified_at=NOW(),cancellation_proof='{"kind":"no_remote"}' WHERE intent_id=10`)
		f.exec(`UPDATE order_sale_intents SET cancellation_verified=TRUE WHERE id=10`)
		f.expect(1, 1, nil)
		f.origin(20, 1, 1, b, "live", true)
		f.exec(`DELETE FROM order_sale_publications WHERE intent_id=20`)
		f.expect(1, 0, nil)
		f.exec(`UPDATE order_sale_intents SET cancellation_verified=TRUE WHERE id=20`)
		f.expect(1, 1, nil)
	})
	t.Run("watermark import skips an intent held by editing instead of deadlocking", func(t *testing.T) {
		f := newRevisionStockFixture(t)
		f.stock(1, 2, a)
		f.origin(10, 1, 2, a, "live", true)
		f.effect(10, 1000, 1, 2, "origin_post", true, b)
		f.revision(10, 1001, 1, 1)
		f.exec(`UPDATE order_sale_intents SET desired_revision_id=1001,active_revision_id=1001 WHERE id=10`)
		f.effect(10, 1001, 1, 2, "revision_unpost", false, c)
		tx, err := f.p.Begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(context.Background())
		if _, err = tx.Exec(context.Background(), `SELECT id FROM order_sale_intents WHERE id=10 FOR UPDATE`); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, err = f.p.Exec(ctx, `UPDATE inventory SET available_qty=2,stock_observed_at=$1::TIMESTAMPTZ WHERE variant_id=1`, d); err != nil {
			t.Fatal("import blocked by editor", err)
		}
		if n := f.value(`SELECT COUNT(*) FROM order_sale_hold_releases`); n != 0 {
			t.Fatal("skipped locked intent was changed")
		}
		if err = tx.Commit(context.Background()); err != nil {
			t.Fatal(err)
		}
		f.exec(`SELECT order_sale_refresh_hold_releases(10)`)
		f.expect(1, 1, map[int64]int64{10: 2})
	})
	t.Run("the first newly imported inventory row also releases a verified decrease", func(t *testing.T) {
		f := newRevisionStockFixture(t)
		f.origin(10, 1, 2, a, "live", true)
		f.effect(10, 1000, 1, 2, "origin_post", true, b)
		f.revision(10, 1001, 1, 1)
		f.exec(`UPDATE order_sale_intents SET desired_revision_id=1001,active_revision_id=1001 WHERE id=10`)
		f.effect(10, 1001, 1, 2, "revision_unpost", false, c)
		f.stock(1, 2, d)
		f.expect(1, 1, map[int64]int64{10: 2})
		if n := f.value(`SELECT SUM(quantity) FROM order_sale_hold_releases`); n != 1 {
			t.Fatal("new inventory row did not release old excess", n)
		}
	})
}
