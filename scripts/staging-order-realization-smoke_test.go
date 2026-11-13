package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"os"
	"testing"
)

func TestExactMockReceipt(t *testing.T) {
	f := fixture{OrderID: 10, ItemID: 11, VariantID: 12, IntentID: 13, Marker: "STG-SALE-SMOKE-test", SabyID: "9000000000000001"}
	req := request{IntentID: f.IntentID, ExecutionMode: "mock", OperationKey: "00000000-0000-4000-8000-000000000001", SiteWarehouseID: 1}
	req.Event.OrderID = f.OrderID
	req.Event.OrderNumber = f.Marker
	rawReq := []byte(`{"intentId":13,"executionMode":"mock","operationKey":"00000000-0000-4000-8000-000000000001","siteWarehouseId":1,"event":{"orderId":10,"orderNumber":"STG-SALE-SMOKE-test","lines":[{"orderItemId":11,"variantId":12,"sabyId":"9000000000000001","quantity":1}]}}`)
	raw := []byte(`{"test":true,"executionMode":"mock","intentId":13,"eventKey":"order:10","operationKey":"00000000-0000-4000-8000-000000000001","documentId":"STG-SALE-13","documentUUID":"00000000-0000-4000-8000-000000000001","siteWarehouseId":1,"lines":[{"documentId":"STG-SALE-13","nomenclatureId":"9000000000000001","direction":1,"quantity":1.0,"unitPrice":1234.560,"lineSum":1234.56,"closed":true}]}`)
	if err := validate(f, "mock", "posted", "STG-SALE-13", req.OperationKey, true, rawReq, raw); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"test", "executionMode", "intentId", "eventKey", "documentId", "documentUUID", "siteWarehouseId"} {
		var value map[string]any
		_ = json.Unmarshal(raw, &value)
		delete(value, field)
		bad, _ := json.Marshal(value)
		if err := validate(f, "mock", "posted", "STG-SALE-13", req.OperationKey, true, rawReq, bad); err == nil {
			t.Fatalf("accepted missing proof %s", field)
		}
	}
	for _, change := range []func(*fixture){func(f *fixture) { f.OrderID++ }, func(f *fixture) { f.ItemID++ }, func(f *fixture) { f.VariantID++ }, func(f *fixture) { f.IntentID++ }, func(f *fixture) { f.Marker = "other" }, func(f *fixture) { f.SabyID = "123" }} {
		other := f
		change(&other)
		if validate(other, "mock", "posted", "STG-SALE-13", req.OperationKey, true, rawReq, raw) == nil {
			t.Fatal("accepted unrelated fixture")
		}
	}
	if validate(f, "live", "posted", "STG-SALE-13", req.OperationKey, true, rawReq, raw) == nil {
		t.Fatal("accepted live")
	}
	if validate(f, "mock", "posted", "STG-SALE-13", req.OperationKey, false, rawReq, raw) == nil {
		t.Fatal("accepted unverified")
	}
}
func TestMoneyComparisonExactAndRunnerTarget(t *testing.T) {
	if !sameNumber(json.Number("1234.560"), "1234.56") || sameNumber(json.Number("1234.5600000000001"), "1234.56") {
		t.Fatal("money was rounded")
	}
	t.Setenv("APP_ENV", "production")
	t.Setenv("SMOKE_STAGE_GUARD", "280757")
	t.Setenv("DATABASE_URL", "postgres://production.invalid/prod")
	if _, err := checkContext(); err == nil {
		t.Fatal("production accepted")
	}
	t.Setenv("APP_ENV", "staging")
	t.Setenv("SMOKE_SOURCE_SHA", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if _, err := checkContext(); err == nil {
		t.Fatal("wrong database accepted")
	}
	t.Setenv("DATABASE_URL", "postgres://ficusin_stage_20261031@isolated.invalid/ficusin_staging_20261031?sslmode=disable")
	t.Setenv("DATABASE_SSL_VERIFY", "true")
	os.Unsetenv("DATABASE_SSL_CA")
	cfg, err := checkContext()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.TLSConfig == nil || len(cfg.ConnConfig.Fallbacks) != 0 || cfg.ConnConfig.TLSConfig.InsecureSkipVerify {
		t.Fatal("plaintext or nonverified fallback accepted")
	}
}

// The real cleanup function is driven through pgx's transaction contract. These
// failures prove it rolls back before any destructive call when ownership is
// unknown, and never treats a failed existence query as successful cleanup.
type cleanupRow struct {
	value any
	err   error
}

func (r cleanupRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	switch v := r.value.(type) {
	case string:
		*dest[0].(*string) = v
	case bool:
		*dest[0].(*bool) = v
	case int64:
		*dest[0].(*int64) = v
	}
	return nil
}

type cleanupTx struct {
	pgx.Tx
	rows               []cleanupRow
	execs              int
	failDelete         int
	commits, rollbacks int
	queries            []string
}

func (t *cleanupTx) QueryRow(_ context.Context, q string, _ ...any) pgx.Row {
	t.queries = append(t.queries, q)
	r := t.rows[0]
	t.rows = t.rows[1:]
	return r
}
func (t *cleanupTx) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	t.execs++
	if t.execs == t.failDelete {
		return pgconn.CommandTag{}, errors.New("fake deletion failure")
	}
	return pgconn.NewCommandTag("DELETE 1"), nil
}
func (t *cleanupTx) Commit(context.Context) error   { t.commits++; return nil }
func (t *cleanupTx) Rollback(context.Context) error { t.rollbacks++; return nil }

type cleanupDB struct{ tx *cleanupTx }

func (d cleanupDB) Begin(context.Context) (pgx.Tx, error) { return d.tx, nil }
func TestCleanupOwnershipAndFailureBoundaries(t *testing.T) {
	f := fixture{OrderID: 10, ItemID: 11, VariantID: 12, ProductID: 13, IntentID: 14, PublicationID: 15, InventoryID: 16, Marker: "STG-SALE-SMOKE-unique", SabyID: "9000000000000001"}
	tests := []struct {
		name               string
		rows               []cleanupRow
		failDelete         int
		wantError          string
		wantExecs, commits int
	}{
		{name: "unknown commit no order", rows: []cleanupRow{{err: pgx.ErrNoRows}, {value: false}}},
		{name: "unavailable ownership is not removed", rows: []cleanupRow{{err: pgx.ErrNoRows}, {err: errors.New("database unavailable")}}, wantError: "cleanup_ownership_query_failed"},
		{name: "order without owned publication retained", rows: []cleanupRow{{err: pgx.ErrNoRows}, {value: true}}, wantError: "cleanup_ownership_failed"},
		{name: "changed fixture retained", rows: []cleanupRow{{value: "posted"}, {err: pgx.ErrNoRows}}, wantError: "cleanup_ownership_failed"},
		{name: "delete failure rolls back all deletes", rows: []cleanupRow{{value: "posted"}, {value: int64(10)}}, failDelete: 3, wantExecs: 3, wantError: "cleanup_delete_failed"},
		{name: "exact fixture removed", rows: []cleanupRow{{value: "posted"}, {value: int64(10)}}, wantExecs: 8, commits: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tx := &cleanupTx{rows: tc.rows, failDelete: tc.failDelete}
			err := cleanup(context.Background(), cleanupDB{tx}, f)
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != tc.wantError || tx.execs != tc.wantExecs || tx.commits != tc.commits {
				t.Fatalf("got err=%s execs=%d commits=%d", got, tx.execs, tx.commits)
			}
			if tc.wantError != "" && tx.rollbacks == 0 {
				t.Fatal("failure did not rollback")
			}
		})
	}
}
