// This opt-in ops runner is copied into the EXACT deployed candidate's backend
// module. It invokes core EnqueueCreated and cancellation hooks and observes the real STG worker. It has
// no external API client, migrations, settings updates or sequence resets.
package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/ordersale"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fixture struct {
	ProductID, VariantID, OrderID, ItemID, IntentID, PublicationID, InventoryID int64
	Marker, SKU, SabyID                                                         string
}
type report struct {
	Probe         string `json:"probe"`
	Result        string `json:"result"`
	Cleanup       string `json:"cleanup"`
	ProductID     int64  `json:"productId,omitempty"`
	VariantID     int64  `json:"variantId,omitempty"`
	OrderID       int64  `json:"orderId,omitempty"`
	IntentID      int64  `json:"intentId,omitempty"`
	PublicationID int64  `json:"publicationId,omitempty"`
	InventoryID   int64  `json:"inventoryId,omitempty"`
}

func coded(value string) error { return errors.New(value) }
func checkContext() (*pgxpool.Config, error) {
	if os.Getenv("APP_ENV") != "staging" || os.Getenv("SMOKE_STAGE_GUARD") != "280757" || len(os.Getenv("SMOKE_SOURCE_SHA")) != 40 {
		return nil, coded("runner_guard_failed")
	}
	parsed, err := url.Parse(os.Getenv("DATABASE_URL"))
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.User == nil || parsed.User.Username() != "ficusin_stage_20261031" || parsed.Path != "/ficusin_staging_20261031" || parsed.Hostname() == "" {
		return nil, coded("database_target_failed")
	}
	cfg, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		return nil, coded("database_config_failed")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: parsed.Hostname()}
	if strings.EqualFold(os.Getenv("DATABASE_SSL_VERIFY"), "false") || os.Getenv("DATABASE_SSL_VERIFY") == "0" {
		tlsConfig.InsecureSkipVerify = true
	} // Exact existing stage TLS policy, never plaintext.
	ca := strings.ReplaceAll(strings.Trim(strings.TrimSpace(os.Getenv("DATABASE_SSL_CA")), "\"'"), `\n`, "\n")
	if ca != "" {
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM([]byte(ca)) {
			return nil, coded("database_ca_failed")
		}
		tlsConfig.RootCAs = roots
	}
	cfg.ConnConfig.TLSConfig = tlsConfig
	cfg.ConnConfig.Fallbacks = nil
	cfg.ConnConfig.ConnectTimeout = 15 * time.Second
	cfg.ConnConfig.RuntimeParams["search_path"] = "public"
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "10000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "3000"
	cfg.MaxConns = 2
	return cfg, nil
}
func guard(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) error {
	var db, user, mode string
	var privileged, enabled, active bool
	var warehouse int64
	if err := q.QueryRow(ctx, `SELECT current_database(),current_user,rolsuper OR rolcreaterole OR rolcreatedb FROM pg_roles WHERE rolname=current_user`).Scan(&db, &user, &privileged); err != nil || db != "ficusin_staging_20261031" || user != "ficusin_stage_20261031" || privileged {
		return coded("actual_database_guard_failed")
	}
	if err := q.QueryRow(ctx, `SELECT execution_mode,enabled,site_warehouse_id,EXISTS(SELECT 1 FROM warehouses WHERE id=1 AND is_active<>0) FROM order_sale_dispatch_control WHERE singleton`).Scan(&mode, &enabled, &warehouse, &active); err != nil || mode != "mock" || !enabled || warehouse != 1 || !active {
		return coded("worker_mock_guard_failed")
	}
	return nil
}
func create(ctx context.Context, pool *pgxpool.Pool, f *fixture) error {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return coded("random_failed")
	}
	f.Marker = "STG-SALE-SMOKE-" + hex.EncodeToString(nonce)
	// A synthetic, positive numeric nomenclature key also exercises exact decimal
	// parsing. No live integration is called, and no stock balance is inserted.
	n := new(big.Int).SetBytes(nonce)
	n.Mod(n, big.NewInt(900000000000000))
	n.Add(n, big.NewInt(9000000000000000))
	f.SabyID = n.String()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return coded("fixture_begin_failed")
	}
	defer tx.Rollback(ctx)
	if err = guard(ctx, tx); err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `WITH code AS(SELECT nextval('ficusin_product_code_seq') AS value) INSERT INTO products(product_code,slug,name,status,saby_fields) SELECT value,value::TEXT,$1,'draft',ARRAY[]::TEXT[] FROM code RETURNING id`, f.Marker).Scan(&f.ProductID)
	if err != nil {
		return coded("fixture_product_failed")
	}
	err = tx.QueryRow(ctx, `INSERT INTO product_variants(product_id,saby_id,label,base_price_minor,is_active) VALUES($1,$2,$3,123456,0) RETURNING id,sku`, f.ProductID, f.SabyID, f.Marker).Scan(&f.VariantID, &f.SKU)
	if err != nil {
		return coded("fixture_variant_failed")
	}
	err = tx.QueryRow(ctx, `INSERT INTO inventory(warehouse_id,variant_id,available_qty,reserved_qty,synced_at,stock_observed_at) VALUES(1,$1,1,0,NOW(),NOW()) RETURNING id`, f.VariantID).Scan(&f.InventoryID)
	if err != nil {
		return coded("fixture_inventory_failed")
	}
	// A synthetic new/unpaid order has no customer or notification recipient.
	// It is linked to core creation intent in the same transaction; no payment,
	// shipping or native stock API is called by this runner.
	err = tx.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status,telegram_notified_at) VALUES($1,$1,'','','pickup',0,1234.56,1234.56,'online','pending','new',NOW()) RETURNING id`, f.Marker).Scan(&f.OrderID)
	if err != nil {
		return coded("fixture_order_failed")
	}
	err = tx.QueryRow(ctx, `INSERT INTO order_items(order_id,product_id,variant_id,sku,product_name,variant_label,unit_price,quantity) VALUES($1,$2,$3,$4,$5,$5,1234.56,1) RETURNING id`, f.OrderID, f.ProductID, f.VariantID, f.SKU, f.Marker).Scan(&f.ItemID)
	if err != nil {
		return coded("fixture_item_failed")
	}
	event, err := ordersale.EnqueueCreated(ctx, tx, f.OrderID)
	if err != nil || event.IntentID <= 0 || !event.CanPrepare {
		return coded("core_enqueue_failed")
	}
	f.IntentID = event.IntentID
	// Ensure the opt-in control created a mock job now, rather than silently
	// passing an intent-only test while the real dispatcher is disabled.
	var mode string
	err = tx.QueryRow(ctx, `SELECT id,execution_mode FROM order_sale_publications WHERE intent_id=$1`, f.IntentID).Scan(&f.PublicationID, &mode)
	if err != nil || mode != "mock" {
		return coded("publication_not_enqueued")
	}
	if err = tx.Commit(ctx); err != nil {
		return coded("fixture_commit_uncertain")
	}
	return nil
}

type receipt struct {
	Test            bool   `json:"test"`
	ExecutionMode   string `json:"executionMode"`
	IntentID        int64  `json:"intentId"`
	EventKey        string `json:"eventKey"`
	OperationKey    string `json:"operationKey"`
	DocumentID      string `json:"documentId"`
	DocumentUUID    string `json:"documentUUID"`
	SiteWarehouseID int64  `json:"siteWarehouseId"`
	Lines           []struct {
		DocumentID     string      `json:"documentId"`
		NomenclatureID string      `json:"nomenclatureId"`
		WarehouseID    int64       `json:"warehouseId"`
		Direction      int64       `json:"direction"`
		Quantity       json.Number `json:"quantity"`
		UnitPrice      json.Number `json:"unitPrice"`
		LineSum        json.Number `json:"lineSum"`
		Closed         bool        `json:"closed"`
	} `json:"lines"`
}
type request struct {
	IntentID        int64  `json:"intentId"`
	ExecutionMode   string `json:"executionMode"`
	OperationKey    string `json:"operationKey"`
	SiteWarehouseID int64  `json:"siteWarehouseId"`
	Event           struct {
		OrderID     int64  `json:"orderId"`
		OrderNumber string `json:"orderNumber"`
		Lines       []struct {
			OrderItemID int64  `json:"orderItemId"`
			VariantID   int64  `json:"variantId"`
			SabyID      string `json:"sabyId"`
			Quantity    int    `json:"quantity"`
		} `json:"lines"`
	} `json:"event"`
}

func sameNumber(value json.Number, expected string) bool {
	a, ok := new(big.Rat).SetString(value.String())
	b, bok := new(big.Rat).SetString(expected)
	return ok && bok && a.Cmp(b) == 0
}
func validate(f fixture, mode, status, docID, docUUID string, verified bool, rawRequest, rawProof []byte) error {
	var req request
	var proof receipt
	if json.Unmarshal(rawRequest, &req) != nil || json.Unmarshal(rawProof, &proof) != nil {
		return coded("invalid_receipt_json")
	}
	if mode != "mock" || status != "posted" || !verified || docID != fmt.Sprintf("STG-SALE-%d", f.IntentID) || docUUID == "" || req.ExecutionMode != "mock" || req.IntentID != f.IntentID || req.OperationKey != docUUID || req.SiteWarehouseID != 1 || req.Event.OrderID != f.OrderID || req.Event.OrderNumber != f.Marker || len(req.Event.Lines) != 1 {
		return coded("mock_receipt_mismatch")
	}
	line := req.Event.Lines[0]
	if line.OrderItemID != f.ItemID || line.VariantID != f.VariantID || line.SabyID != f.SabyID || line.Quantity != 1 {
		return coded("request_line_mismatch")
	}
	if !proof.Test || proof.ExecutionMode != "mock" || proof.IntentID != f.IntentID || proof.EventKey != fmt.Sprintf("order:%d", f.OrderID) || proof.OperationKey != docUUID || proof.DocumentID != docID || proof.DocumentUUID != docUUID || proof.SiteWarehouseID != 1 || len(proof.Lines) != 1 {
		return coded("proof_mismatch")
	}
	pl := proof.Lines[0]
	if pl.DocumentID != docID || pl.NomenclatureID != f.SabyID || pl.Direction != 1 || !pl.Closed || !sameNumber(pl.Quantity, "1") || !sameNumber(pl.UnitPrice, "1234.56") || !sameNumber(pl.LineSum, "1234.56") {
		return coded("proof_line_mismatch")
	}
	return nil
}
func observe(ctx context.Context, pool *pgxpool.Pool, f fixture) error {
	deadline := time.Now().Add(110 * time.Second)
	for time.Now().Before(deadline) {
		if err := guard(ctx, pool); err != nil {
			return err
		}
		var mode, status, docID, docUUID string
		var verified bool
		var req, proof []byte
		err := pool.QueryRow(ctx, `SELECT execution_mode,status,document_id,document_uuid,stock_verified,request,stock_proof FROM order_sale_publications WHERE id=$1 AND intent_id=$2`, f.PublicationID, f.IntentID).Scan(&mode, &status, &docID, &docUUID, &verified, &req, &proof)
		if err != nil {
			return coded("publication_observation_failed")
		}
		if mode != "mock" {
			return coded("publication_mode_changed")
		}
		if status == "posted" {
			return validate(f, mode, status, docID, docUUID, verified, req, proof)
		}
		if status == "failed" || status == "needs_attention" {
			return coded("mock_worker_rejected_fixture")
		}
		select {
		case <-ctx.Done():
			return coded("smoke_cancelled")
		case <-time.After(time.Second):
		}
	}
	return coded("mock_worker_timeout")
}

// Confirm public effective stock is held while posted in mock mode, then
// request undo through the same transactional hook used by real cancellation.
func cancelAndObserve(ctx context.Context, pool *pgxpool.Pool, f fixture) error {
	var effective, physical, reserved int
	if err := pool.QueryRow(ctx, `SELECT effective.available_qty,raw.available_qty,raw.reserved_qty FROM inventory raw JOIN order_sale_inventory effective ON effective.id=raw.id WHERE raw.id=$1 AND raw.variant_id=$2 AND raw.warehouse_id=1`, f.InventoryID, f.VariantID).Scan(&effective, &physical, &reserved); err != nil || effective != 0 || physical != 1 || reserved != 0 {
		return coded("mock_stock_hold_mismatch")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return coded("cancel_begin_failed")
	}
	defer tx.Rollback(ctx)
	if err = guard(ctx, tx); err != nil {
		return err
	}
	var id int64
	if err = tx.QueryRow(ctx, `SELECT id FROM orders WHERE id=$1 AND order_number=$2 AND customer_name=$2 AND customer_id IS NULL AND status='new' FOR UPDATE`, f.OrderID, f.Marker).Scan(&id); err != nil || id != f.OrderID {
		return coded("cancel_ownership_failed")
	}
	if err = ordersale.RequestCancellation(ctx, tx, f.OrderID); err != nil {
		return coded("core_cancellation_failed")
	}
	tag, err := tx.Exec(ctx, `UPDATE orders SET status='cancelled',payment_status='cancelled',cancellation_reason='Synthetic STG worker smoke' WHERE id=$1 AND order_number=$2 AND status='new'`, f.OrderID, f.Marker)
	if err != nil || tag.RowsAffected() != 1 {
		return coded("cancel_transition_failed")
	}
	if err = tx.Commit(ctx); err != nil {
		return coded("cancel_commit_uncertain")
	}
	deadline := time.Now().Add(110 * time.Second)
	for time.Now().Before(deadline) {
		if err = guard(ctx, pool); err != nil {
			return err
		}
		var state, mode, docID, docUUID string
		var verified, intentVerified, originalVerified bool
		var rawProof, originalProof, rawRequest []byte
		err = pool.QueryRow(ctx, `SELECT p.status,p.execution_mode,p.document_id,p.document_uuid,p.cancellation_verified,i.cancellation_verified,p.stock_verified,p.cancellation_proof,p.stock_proof,p.request FROM order_sale_publications p JOIN order_sale_intents i ON i.id=p.intent_id WHERE p.id=$1 AND i.id=$2 AND i.order_id=$3 AND p.desired_state='cancelled'`, f.PublicationID, f.IntentID, f.OrderID).Scan(&state, &mode, &docID, &docUUID, &verified, &intentVerified, &originalVerified, &rawProof, &originalProof, &rawRequest)
		if err != nil || mode != "mock" {
			return coded("cancellation_observation_failed")
		}
		if state == "cancelled" {
			if !verified || !intentVerified || validate(f, mode, "posted", docID, docUUID, originalVerified, rawRequest, originalProof) != nil {
				return coded("cancellation_lost_original_proof")
			}
			var proof struct {
				Kind     string   `json:"kind"`
				Document *receipt `json:"document"`
			}
			if json.Unmarshal(rawProof, &proof) != nil || proof.Kind != "unposted_full" || proof.Document == nil {
				return coded("cancellation_proof_mismatch")
			}
			p := proof.Document
			if !p.Test || p.ExecutionMode != "mock" || p.IntentID != f.IntentID || p.EventKey != fmt.Sprintf("order:%d", f.OrderID) || p.DocumentID != docID || p.DocumentUUID != docUUID || p.OperationKey != docUUID || p.SiteWarehouseID != 1 || len(p.Lines) != 1 {
				return coded("cancellation_proof_mismatch")
			}
			line := p.Lines[0]
			if line.Closed || line.DocumentID != docID || line.NomenclatureID != f.SabyID || line.Direction != 1 || !sameNumber(line.Quantity, "1") || !sameNumber(line.UnitPrice, "1234.56") || !sameNumber(line.LineSum, "1234.56") {
				return coded("cancellation_line_mismatch")
			}
			if err = pool.QueryRow(ctx, `SELECT effective.available_qty,raw.available_qty,raw.reserved_qty FROM inventory raw JOIN order_sale_inventory effective ON effective.id=raw.id WHERE raw.id=$1 AND raw.variant_id=$2 AND raw.warehouse_id=1`, f.InventoryID, f.VariantID).Scan(&effective, &physical, &reserved); err != nil || effective != 1 || physical != 1 || reserved != 0 {
				return coded("mock_stock_return_mismatch")
			}
			return nil
		}
		if state == "needs_attention" || state == "failed" {
			return coded("mock_cancellation_rejected")
		}
		select {
		case <-ctx.Done():
			return coded("smoke_cancelled")
		case <-time.After(time.Second):
		}
	}
	return coded("mock_cancellation_timeout")
}

type transactionBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

func cleanup(ctx context.Context, pool transactionBeginner, f fixture) error {
	if f.OrderID == 0 || f.IntentID == 0 {
		return nil
	} // Uncommitted transaction rolled back.
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return coded("cleanup_begin_failed")
		}
		var state string
		err = tx.QueryRow(ctx, `SELECT status FROM order_sale_publications WHERE id=$1 AND intent_id=$2 FOR UPDATE`, f.PublicationID, f.IntentID).Scan(&state)
		if errors.Is(err, pgx.ErrNoRows) {
			var exists bool
			checkErr := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM orders WHERE id=$1)`, f.OrderID).Scan(&exists)
			tx.Rollback(ctx)
			if checkErr != nil {
				return coded("cleanup_ownership_query_failed")
			}
			if !exists {
				return nil
			} // Uncertain fixture COMMIT committed nothing.
			return coded("cleanup_ownership_failed")
		}
		if err != nil {
			tx.Rollback(ctx)
			return coded("cleanup_lock_failed")
		}
		if state == "processing" {
			tx.Rollback(ctx)
			select {
			case <-ctx.Done():
				return coded("cleanup_processing_retained")
			case <-time.After(time.Second):
			}
			continue
		}
		// Lock every parent in the identity chain as well as the publication.
		// This excludes concurrent edits/child inserts while checking the exact
		// one-item fixture, so an order CASCADE cannot consume another line.
		var ownOrder int64
		err = tx.QueryRow(ctx, `SELECT o.id FROM orders o JOIN order_items oi ON oi.order_id=o.id JOIN product_variants v ON v.id=oi.variant_id JOIN products p ON p.id=v.product_id JOIN inventory stock ON stock.variant_id=v.id AND stock.id=$9 AND stock.warehouse_id=1 AND stock.available_qty=1 AND stock.reserved_qty=0 JOIN order_sale_intents i ON i.order_id=o.id JOIN order_sale_publications pub ON pub.intent_id=i.id WHERE o.id=$1 AND oi.id=$2 AND v.id=$3 AND p.id=$4 AND i.id=$5 AND pub.id=$6 AND o.order_number=$7 AND o.customer_name=$7 AND o.customer_id IS NULL AND o.delivery_method='pickup' AND o.status IN ('new','cancelled') AND p.name=$7 AND p.status='draft' AND v.label=$7 AND v.is_active=0 AND v.saby_id=$8 AND oi.product_id=p.id AND oi.sku=v.sku AND oi.quantity=1 AND oi.unit_price=1234.56 AND pub.execution_mode='mock' AND i.event_key='order:'||o.id::TEXT AND NOT EXISTS(SELECT 1 FROM order_items other WHERE other.order_id=o.id AND other.id<>oi.id) AND NOT EXISTS(SELECT 1 FROM product_variants other WHERE other.product_id=p.id AND other.id<>v.id) AND NOT EXISTS(SELECT 1 FROM order_sale_intents other WHERE other.order_id=o.id AND other.id<>i.id) AND NOT EXISTS(SELECT 1 FROM inventory other WHERE other.variant_id=v.id AND other.id<>stock.id) FOR UPDATE OF o,oi,v,p,i,stock`, f.OrderID, f.ItemID, f.VariantID, f.ProductID, f.IntentID, f.PublicationID, f.Marker, f.SabyID, f.InventoryID).Scan(&ownOrder)
		if err != nil || ownOrder != f.OrderID {
			tx.Rollback(ctx)
			return coded("cleanup_ownership_failed")
		}
		// No CAS/lease bypass: the row lock excludes a new worker claim. Restrict
		// every delete to the immutable captured identity, not names or ID ranges.
		statements := []struct {
			sql  string
			args []any
		}{
			{`DELETE FROM order_sale_publications WHERE id=$1 AND intent_id=$2 AND execution_mode='mock'`, []any{f.PublicationID, f.IntentID}},
			{`DELETE FROM order_sale_intent_lines WHERE intent_id=$1 AND order_item_id=$2`, []any{f.IntentID, f.ItemID}},
			{`DELETE FROM order_sale_intents WHERE id=$1 AND order_id=$2`, []any{f.IntentID, f.OrderID}},
			{`DELETE FROM order_items WHERE id=$1 AND order_id=$2 AND variant_id=$3`, []any{f.ItemID, f.OrderID, f.VariantID}},
			{`DELETE FROM orders WHERE id=$1 AND order_number=$2 AND customer_id IS NULL`, []any{f.OrderID, f.Marker}},
			{`DELETE FROM inventory WHERE id=$1 AND variant_id=$2 AND warehouse_id=1`, []any{f.InventoryID, f.VariantID}},
			{`DELETE FROM product_variants WHERE id=$1 AND product_id=$2 AND saby_id=$3`, []any{f.VariantID, f.ProductID, f.SabyID}},
			{`DELETE FROM products WHERE id=$1 AND name=$2 AND status='draft'`, []any{f.ProductID, f.Marker}},
		}
		for _, s := range statements {
			tag, err := tx.Exec(ctx, s.sql, s.args...)
			if err != nil || tag.RowsAffected() != 1 {
				tx.Rollback(ctx)
				return coded("cleanup_delete_failed")
			}
		}
		if err = tx.Commit(ctx); err != nil {
			return coded("cleanup_commit_uncertain")
		}
		return nil
	}
	return coded("cleanup_processing_retained")
}
func main() {
	result := report{Probe: "staging_sale_smoke", Result: "runner_failed", Cleanup: "not_needed"}
	cfg, err := checkContext()
	if err != nil {
		result.Result = err.Error()
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}
	signalCtx, stopSignal := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignal()
	ctx, cancel := context.WithTimeout(signalCtx, 300*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		result.Result = "database_connection_failed"
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}
	defer pool.Close()
	var f fixture
	err = create(ctx, pool, &f)
	if err == nil {
		err = observe(ctx, pool, f)
	}
	if err == nil {
		err = cancelAndObserve(ctx, pool, f)
	}
	result.ProductID = f.ProductID
	result.VariantID = f.VariantID
	result.OrderID = f.OrderID
	result.IntentID = f.IntentID
	result.PublicationID = f.PublicationID
	result.InventoryID = f.InventoryID
	cleanupCtx, stop := context.WithTimeout(context.Background(), 55*time.Second)
	defer stop()
	cleanupErr := cleanup(cleanupCtx, pool, f)
	if cleanupErr != nil {
		result.Cleanup = "retained"
		result.Result = cleanupErr.Error()
	} else {
		result.Cleanup = "removed"
		if err != nil {
			result.Result = err.Error()
		} else {
			result.Result = "passed"
		}
	}
	json.NewEncoder(os.Stdout).Encode(result)
	if result.Result != "passed" {
		os.Exit(1)
	}
}
