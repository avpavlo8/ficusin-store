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
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM marketplace_returns WHERE source_return_id=$1`, sourceID) }()
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
}
