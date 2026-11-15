package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/config"
	"github.com/avpavlo8/ficusin-store/backend/internal/ordersale"
	"github.com/avpavlo8/ficusin-store/backend/internal/store"
	"github.com/jackc/pgx/v5"
)

type private struct {
	DatabaseURL, CA string
	VerifyTLS       bool
}

func main() {
	if err := run(); err != nil {
		fmt.Println(`{"ok":false,"reason":"fixed_order_recovery_guard_or_service_failed"}`)
		os.Exit(1)
	}
}
func run() error {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		return errors.New("actions only")
	}
	apply := len(os.Args) == 2 && os.Args[1] == "--apply"
	if apply && os.Getenv("GITHUB_RUN_ATTEMPT") != "1" {
		return errors.New("no blind replay")
	}
	var secret private
	if json.NewDecoder(os.Stdin).Decode(&secret) != nil {
		return errors.New("private config missing")
	}
	parsed, err := url.Parse(secret.DatabaseURL)
	if err != nil || parsed.Path != "/default_db" || parsed.User == nil {
		return errors.New("database scope")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := store.Open(ctx, config.Database{URL: secret.DatabaseURL, CA: secret.CA, VerifyTLS: secret.VerifyTLS, MaxConnections: 1})
	if err != nil {
		return err
	}
	defer pool.Close()
	options := pgx.TxOptions{AccessMode: pgx.ReadOnly}
	if apply {
		options.AccessMode = pgx.ReadWrite
	}
	tx, err := pool.BeginTx(ctx, options)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var db, user, access string
	if err = tx.QueryRow(ctx, "SELECT current_database(),current_user,current_setting('transaction_read_only')").Scan(&db, &user, &access); err != nil {
		return err
	}
	expectedAccess := "on"
	if apply {
		expectedAccess = "off"
	}
	if db != "default_db" || user != parsed.User.Username() || access != expectedAccess {
		return errors.New("database identity")
	}
	if apply {
		if _, err = tx.Exec(ctx, "SELECT id FROM orders WHERE id=74 FOR UPDATE"); err != nil {
			return err
		}
	}
	var number, status, method, payment, delivery string
	if err = tx.QueryRow(ctx, "SELECT order_number,status,payment_method,payment_status,delivery_method FROM orders WHERE id=74").Scan(&number, &status, &method, &payment, &delivery); err != nil {
		return err
	}
	if number != "0000-23" || status != "assembling" || method != "online" || payment != "paid" || delivery != "cdek" {
		return errors.New("order changed")
	}
	var mode string
	var enabled bool
	var site, native, movement, organization, regulation, buyer int64
	controlSQL := "SELECT execution_mode,enabled,site_warehouse_id,native_warehouse_id,movement_warehouse_id,organization_id,regulation_id,buyer_id FROM order_sale_dispatch_control WHERE singleton"
	if apply {
		controlSQL += " FOR SHARE"
	}
	if err = tx.QueryRow(ctx, controlSQL).Scan(&mode, &enabled, &site, &native, &movement, &organization, &regulation, &buyer); err != nil {
		return err
	}
	if mode != "live" || !enabled || site != 1 || native != 283 || movement != 284 || organization != 259 || regulation != 122650 || buyer != 18749 {
		return errors.New("unverified live dispatch")
	}
	var warehouseCount int
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM warehouses WHERE id=1 AND is_active<>0 AND saby_id='saby-ryazan-main'").Scan(&warehouseCount); err != nil || warehouseCount != 1 {
		return errors.New("warehouse changed")
	}
	rows, err := tx.Query(ctx, `SELECT oi.variant_id,oi.quantity,oi.unit_price::text,
 COALESCE((SELECT CASE WHEN count(DISTINCT external_id)=1 THEN min(external_id) ELSE '' END FROM product_external_ids WHERE variant_id=oi.variant_id AND provider='saby' AND id_type='id' AND status='active' HAVING count(*)>0),v.saby_id,'')
 FROM order_items oi JOIN product_variants v ON v.id=oi.variant_id WHERE oi.order_id=74 ORDER BY oi.id`)
	if err != nil {
		return err
	}
	count := 0
	for rows.Next() {
		var variant int64
		var quantity int
		var price, sku string
		if err = rows.Scan(&variant, &quantity, &price, &sku); err != nil {
			rows.Close()
			return err
		}
		if variant != 59 || quantity != 1 || price != "3827.00" || sku != "1117" {
			rows.Close()
			return errors.New("lines changed")
		}
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil || count != 1 {
		return errors.New("line scope")
	}
	var existing int64
	if err = tx.QueryRow(ctx, "SELECT COALESCE(min(id),0) FROM order_sale_intents WHERE order_id=74").Scan(&existing); err != nil {
		return err
	}
	if existing > 0 {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"alreadyEnqueued": true, "orderNumber": number, "intentId": existing, "newWrites": false})
	}
	var sent int
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM stock_movements WHERE order_id=74 AND sent_at IS NOT NULL").Scan(&sent); err != nil || sent != 0 {
		return errors.New("historical stock outcome")
	}
	report := map[string]any{"readOnly": !apply, "orderNumber": number, "nativeNomenclature": 1117, "quantity": 1, "unitPrice": "3827.00", "buyerID": buyer, "paymentsRequested": false, "receiptsRequested": false, "otherOrdersTouched": false}
	if !apply {
		return json.NewEncoder(os.Stdout).Encode(report)
	}
	event, err := ordersale.EnqueueCreated(ctx, tx, 74)
	if err != nil {
		return err
	}
	if event.IntentID <= 0 || event.OrderNumber != number || event.Trigger != "created" || len(event.Lines) != 1 || event.Lines[0].VariantID != 59 || event.Lines[0].SabyID != "1117" || event.Lines[0].Quantity != 1 || event.Lines[0].UnitPrice != "3827.00" {
		return errors.New("canonical event mismatch")
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	report["enqueued"] = true
	report["intentId"] = event.IntentID
	report["eventKey"] = event.Key
	report["fingerprint"] = event.Fingerprint
	return json.NewEncoder(os.Stdout).Encode(report)
}
