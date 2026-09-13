package admin

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStage09PlantReturnsAreIdempotentAndDoNotChangeStock(t *testing.T) {
	databaseURL := os.Getenv("CRM_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CRM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var managerID, variantID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM customers WHERE email='crm-manager@example.invalid'`).Scan(&managerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM product_variants WHERE saby_id='crm-stage07-a'`).Scan(&variantID); err != nil {
		t.Fatal(err)
	}
	actor := Actor{CustomerID: managerID, Role: RoleManager}
	if Can(RoleManager, PermissionProcurementEdit) {
		t.Fatal("manager unexpectedly received procurement price permission")
	}
	var stockBefore int
	if err := pool.QueryRow(ctx, `SELECT COALESCE((SELECT available_qty FROM inventory WHERE variant_id=$1),0)`, variantID).Scan(&stockBefore); err != nil {
		t.Fatal(err)
	}
	sourceID := fmt.Sprintf("crm-stage09-%d", time.Now().UnixNano())
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM marketplace_returns WHERE source_return_id LIKE $1`, sourceID+"%") }()
	repository := NewPostgresRepository(pool)
	created, err := repository.CreateMarketplaceReturns(ctx, actor, ReturnCreate{
		Channel: "ozon", SourceReturnID: sourceID, SourceShipmentID: "CRM-STAGE-09-SHIPMENT",
		VariantID: variantID, Quantity: 3, ReturnedAt: time.Now().Format("2006-01-02"),
		Conditions: []string{"ready", "restoring", "dead"}, Comment: "Три растения из одной отправки",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 3 || created[0].ID == created[1].ID || created[1].ID == created[2].ID {
		t.Fatalf("expected three distinct plant records, got %+v", created)
	}
	if created[0].FinancialStatus != "incomplete" || created[0].CostOutcome != "restored" || created[2].CostOutcome != "lost" {
		t.Fatalf("unexpected financial states: %+v", created)
	}
	repeated, err := repository.CreateMarketplaceReturns(ctx, actor, ReturnCreate{
		Channel: "ozon", SourceReturnID: sourceID, SourceShipmentID: "CRM-STAGE-09-SHIPMENT",
		VariantID: variantID, Quantity: 3, ReturnedAt: time.Now().Format("2006-01-02"),
		Conditions: []string{"ready", "restoring", "dead"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := range created {
		if repeated[index].ID != created[index].ID {
			t.Fatalf("duplicate import created a new plant: %d != %d", repeated[index].ID, created[index].ID)
		}
	}
	restored, err := repository.UpdateMarketplaceReturn(ctx, actor, created[1].ID, ReturnUpdate{Condition: "ready", Comment: "Восстановилось"})
	if err != nil {
		t.Fatal(err)
	}
	if restored.Condition != "ready" || len(restored.History) != 2 {
		t.Fatalf("restoring transition was not retained: %+v", restored)
	}
	firstReceipt, err := repository.CreateReturnReceipt(ctx, actor, created[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	secondReceipt, err := repository.CreateReturnReceipt(ctx, actor, created[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstReceipt.ReceiptStatus != "queued" || secondReceipt.ReceiptStatus != "queued" {
		t.Fatalf("unexpected receipt status: %s / %s", firstReceipt.ReceiptStatus, secondReceipt.ReceiptStatus)
	}
	var actions int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM procurement_action_items WHERE marketplace_return_id=$1`, created[0].ID).Scan(&actions); err != nil || actions != 1 {
		t.Fatalf("expected one durable receipt action, count=%d err=%v", actions, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE procurement_action_items SET status='failed',attempts=5,error_message='test failure' WHERE marketplace_return_id=$1`, created[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE marketplace_returns SET receipt_status='failed' WHERE id=$1`, created[0].ID); err != nil {
		t.Fatal(err)
	}
	retried, err := repository.CreateReturnReceipt(ctx, actor, created[0].ID)
	if err != nil || retried.ReceiptStatus != "queued" {
		t.Fatalf("failed receipt was not requeued: status=%s err=%v", retried.ReceiptStatus, err)
	}
	var actionStatus string
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT status,attempts FROM procurement_action_items WHERE marketplace_return_id=$1`, created[0].ID).Scan(&actionStatus, &attempts); err != nil || actionStatus != "queued" || attempts != 0 {
		t.Fatalf("failed action was not reset: status=%s attempts=%d err=%v", actionStatus, attempts, err)
	}
	var stockAfter int
	if err := pool.QueryRow(ctx, `SELECT COALESCE((SELECT available_qty FROM inventory WHERE variant_id=$1),0)`, variantID).Scan(&stockAfter); err != nil {
		t.Fatal(err)
	}
	if stockAfter != stockBefore {
		t.Fatalf("local stock changed before Saby posting: %d -> %d", stockBefore, stockAfter)
	}
	if _, err := pool.Exec(ctx, `UPDATE marketplace_returns SET receipt_status='posted' WHERE id=$1`, created[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.UpdateMarketplaceReturn(ctx, actor, created[0].ID, ReturnUpdate{Condition: "dead"}); err == nil {
		t.Fatal("posted receipt allowed a direct condition reversal")
	}

	// A return linked to a historical sale uses the sale's cost snapshot,
	// even if a newer receipt has changed the product's current cost.
	saleSourceID := sourceID + "-sale"
	var saleID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO sales_events(channel,source_event_id,source_document_id,source_line_id,event_type,event_status,
			event_at,external_product_id,canonical_variant_id,units,gross_rub,effect,reconciliation_status,import_batch_id,unit_cost_rub_snapshot)
		VALUES('ozon',$1,$1,'1','sale','confirmed',CURRENT_TIMESTAMP,'stage09-product',$2,1,2000,1,'counted',gen_random_uuid(),321.45)
		RETURNING id`, saleSourceID, variantID).Scan(&saleID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM sales_events WHERE id=$1`, saleID) }()
	linked, err := repository.CreateMarketplaceReturns(ctx, actor, ReturnCreate{
		Channel: "ozon", SourceReturnID: sourceID + "-linked", SourceShipmentID: "CRM-STAGE-09-LINKED",
		SalesEventID: &saleID, VariantID: variantID, Quantity: 1, ReturnedAt: time.Now().Format("2006-01-02"),
		Conditions: []string{"dead"},
	})
	if err != nil || len(linked) != 1 || linked[0].UnitCost == nil || *linked[0].UnitCost != 321.45 {
		t.Fatalf("linked return did not retain sale cost: %+v err=%v", linked, err)
	}
}
