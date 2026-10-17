package procurement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestFailedReceiptPriceRefreshPreservesPurchaseAndExternalState(t *testing.T) {
	dsn := os.Getenv("CRM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CRM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, tc := range []struct {
		name, batchStatus, itemStatus, kind, channel, otherStatus string
		changedCosts, missingCost, otherBatch, wantError          bool
	}{
		{name: "failed_receipt", batchStatus: "partially_completed", itemStatus: "failed"},
		{name: "failed_batch", batchStatus: "failed", itemStatus: "failed"},
		{name: "running_receipt", batchStatus: "processing", itemStatus: "processing", wantError: true},
		{name: "queued_receipt", batchStatus: "processing", itemStatus: "queued", wantError: true},
		{name: "changed_delivery", batchStatus: "partially_completed", itemStatus: "failed", changedCosts: true, wantError: true},
		{name: "missing_cost", batchStatus: "partially_completed", itemStatus: "failed", missingCost: true, wantError: true},
		{name: "terminal_prices", batchStatus: "partially_completed", itemStatus: "failed", kind: "prices", channel: "wb"},
		{name: "failed_ozon_prices", batchStatus: "failed", itemStatus: "failed", kind: "prices", channel: "ozon"},
		{name: "receipt_and_terminal_prices", batchStatus: "partially_completed", itemStatus: "failed", otherBatch: true, otherStatus: "failed"},
		{name: "receipt_and_running_prices", batchStatus: "partially_completed", itemStatus: "failed", otherBatch: true, otherStatus: "processing", wantError: true},
		{name: "running_prices", batchStatus: "processing", itemStatus: "queued", kind: "prices", channel: "wb", wantError: true},
		{name: "unfinished_prices", batchStatus: "partially_completed", itemStatus: "failed", otherBatch: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx) //nolint:errcheck
			unique := time.Now().UnixNano()
			kind, channel := tc.kind, tc.channel
			if kind == "" {
				kind = "receipt"
			}
			if channel == "" {
				channel = "saby_receipt"
			}
			var actor, supplier, order, line, batch, product, variant int64
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			must(tx.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at) VALUES($1,$2,'','Price refresh',CURRENT_TIMESTAMP) RETURNING id`, fmt.Sprintf("refresh-%d@example.invalid", unique), fmt.Sprintf("+79%09d", unique%1000000000)).Scan(&actor))
			must(tx.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency) VALUES($1,'domestic','RUB') RETURNING id`, fmt.Sprintf("Refresh %d", unique)).Scan(&supplier))
			must(tx.QueryRow(ctx, `INSERT INTO products(name,slug,status,catalog_section) VALUES('Refresh plant',$1,'draft','plants') RETURNING id`, fmt.Sprintf("refresh-%d", unique)).Scan(&product))
			must(tx.QueryRow(ctx, `INSERT INTO product_variants(product_id,label,base_price_minor,is_active) VALUES($1,'D12',50000,1) RETURNING id`, product).Scan(&variant))
			must(tx.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,order_number,source_kind,currency,status,exchange_rate,delivery_to_moscow_rub,delivery_to_ryazan_rub,additional_costs_rub,calculated_at,calculation_version) VALUES($1,$2,'payment_invoice','RUB','ready_to_receive',1,20,10,5,CURRENT_TIMESTAMP,1) RETURNING id`, supplier, fmt.Sprintf("REFRESH-%d", unique)).Scan(&order))
			must(tx.QueryRow(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,canonical_variant_id,raw_name,ordered_qty,invoiced_qty,unit_price,match_status,reconciliation_status,purchase_unit_rub,trolley_delivery_unit_rub,ryazan_delivery_unit_rub,additional_purchase_unit_rub,unit_cost_rub,proposed_retail_rub,channel_costs) VALUES($1,$2,'Refresh plant',1,1,100,'confirmed','matched',100,20,10,5,135,270,'{}') RETURNING id`, order, variant).Scan(&line))
			must(tx.QueryRow(ctx, `INSERT INTO procurement_action_batches(procurement_order_id,kind,status,calculation_version) VALUES($1,$3,$2,1) RETURNING id`, order, tc.batchStatus, kind).Scan(&batch))
			_, err = tx.Exec(ctx, `INSERT INTO procurement_action_items(batch_id,procurement_order_line_id,channel,external_article,new_value,quantity,status,payload,external_operation_id,error_message) VALUES($1,$2,$4,'plant',135,1,$3,'{"lines":[{"sabyId":"plant","quantity":1}]}','external-468','Original receipt failure')`, batch, line, tc.itemStatus, channel)
			must(err)
			if tc.missingCost {
				_, err = tx.Exec(ctx, `UPDATE procurement_order_lines SET unit_cost_rub=NULL WHERE id=$1`, line)
				must(err)
			}
			if tc.otherBatch {
				_, err = tx.Exec(ctx, `INSERT INTO procurement_action_batches(procurement_order_id,kind,status) VALUES($1,'prices','partially_completed')`, order)
				must(err)
				if tc.otherStatus != "" {
					var extraBatch int64
					must(tx.QueryRow(ctx, `SELECT MAX(id) FROM procurement_action_batches WHERE procurement_order_id=$1 AND kind='prices'`, order).Scan(&extraBatch))
					_, err = tx.Exec(ctx, `UPDATE procurement_action_batches SET status=$2 WHERE id=$1`, extraBatch, tc.otherStatus)
					must(err)
					_, err = tx.Exec(ctx, `INSERT INTO procurement_action_items(batch_id,procurement_order_line_id,channel,new_value,status) VALUES($1,$2,'ozon',400,$3)`, extraBatch, line, tc.otherStatus)
					must(err)
				}
			}
			input := CalculationInput{ExchangeRate: 1, DeliveryToMoscowRUB: 20, DeliveryToRyazanRUB: 10, AdditionalCostsRUB: 5}
			if tc.changedCosts {
				input.DeliveryToMoscowRUB++
			}
			zero := 0.0
			profile := ChannelCosts{CommissionRate: &zero, AcquiringRate: &zero, PackagingRUB: &zero, LogisticsRUB: &zero, LossRUB: &zero}
			settings := PricingSettings{Version: 42, RetailMarkupMultiplier: 2, TaxRate: 0.08, PriceChangeThreshold: 0.1, Economics: PricingEconomics{Channels: map[string]ChannelCosts{"site": profile, "wb": profile, "ozon": profile}}}
			err = refreshPricesAfterTerminalActions(ctx, tx, Actor{CustomerID: actor, Role: "owner"}, order, input, settings)
			if tc.wantError {
				var userFacing *UserFacingError
				if !errors.As(err, &userFacing) {
					t.Fatalf("error=%v, want actionable error", err)
				}
				return
			}
			must(err)
			var purchase, moscow, ryazan, extra, cost float64
			var retail, sitePrice, version, receiptVersion int64
			var quotes map[string]ChannelQuote
			must(tx.QueryRow(ctx, `SELECT purchase_unit_rub::DOUBLE PRECISION,trolley_delivery_unit_rub::DOUBLE PRECISION,ryazan_delivery_unit_rub::DOUBLE PRECISION,additional_purchase_unit_rub::DOUBLE PRECISION,unit_cost_rub::DOUBLE PRECISION,proposed_retail_rub,channel_quotes FROM procurement_order_lines WHERE id=$1`, line).Scan(&purchase, &moscow, &ryazan, &extra, &cost, &retail, &quotes))
			if purchase != 100 || moscow != 20 || ryazan != 10 || extra != 5 || cost != 135 || retail != 270 {
				t.Fatalf("cost snapshot changed: %v %v %v %v %v retail=%d", purchase, moscow, ryazan, extra, cost, retail)
			}
			if quotes["site"].ContributionRUB == nil || *quotes["site"].ContributionRUB <= 0 || quotes["ozon"].PriceRUB == nil {
				t.Fatalf("quotes not refreshed: %+v", quotes)
			}
			must(tx.QueryRow(ctx, `SELECT base_price_minor FROM product_variants WHERE id=$1`, variant).Scan(&sitePrice))
			if sitePrice != 50000 {
				t.Fatal("storefront price changed")
			}
			var receiptStatus, message, operation, batchStatus, orderStatus string
			var payload json.RawMessage
			must(tx.QueryRow(ctx, `SELECT i.status,i.error_message,i.external_operation_id,i.payload,b.status,b.calculation_version FROM procurement_action_items i JOIN procurement_action_batches b ON b.id=i.batch_id WHERE b.id=$1`, batch).Scan(&receiptStatus, &message, &operation, &payload, &batchStatus, &receiptVersion))
			if receiptStatus != "failed" || message != "Original receipt failure" || operation != "external-468" || batchStatus != tc.batchStatus || receiptVersion != 1 {
				t.Fatalf("receipt changed: %s %s %s %s %d", receiptStatus, message, operation, batchStatus, receiptVersion)
			}
			var receiptPayload struct {
				Lines []struct {
					Quantity int `json:"quantity"`
				} `json:"lines"`
			}
			must(json.Unmarshal(payload, &receiptPayload))
			if len(receiptPayload.Lines) != 1 || receiptPayload.Lines[0].Quantity != 1 {
				t.Fatal("receipt payload changed")
			}
			must(tx.QueryRow(ctx, `SELECT calculation_version,status FROM procurement_orders WHERE id=$1`, order).Scan(&version, &orderStatus))
			if version != 42 || orderStatus != "ready_to_receive" {
				t.Fatalf("version/status: %d %s", version, orderStatus)
			}
			var audits int
			must(tx.QueryRow(ctx, `SELECT COUNT(*) FROM admin_audit_log WHERE action='procurement.order.refresh_prices' AND entity_id=$1`, fmt.Sprint(order)).Scan(&audits))
			if audits != 1 {
				t.Fatalf("missing audit: %d", audits)
			}
		})
	}
}
