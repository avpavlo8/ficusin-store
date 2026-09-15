package admin

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func TestStage13PnLUsesMoscowBusinessDay(t *testing.T) {
	if os.Getenv("CRM_TEST_DATABASE_URL") == "" {
		t.Skip("CRM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("CRM_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	prefix := fmt.Sprintf("stage13-day-%d", time.Now().UnixNano())
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM sales_events WHERE source_event_id LIKE $1`, prefix+"%") }()
	for i, date := range []string{"2051-09-01 00:30:00+03", "2051-09-02 00:30:00+03"} {
		if _, err = pool.Exec(ctx, `INSERT INTO sales_events(channel,source_event_id,source_document_id,source_line_id,event_type,event_status,event_at,external_product_id,units,gross_rub,effect,reconciliation_status,import_batch_id) VALUES('saby',$1,$1,'1','sale','confirmed',$2::TIMESTAMPTZ,'test',1,$3,1,'counted',gen_random_uuid())`, fmt.Sprintf("%s-%d", prefix, i), date, 100*(i+1)); err != nil {
			t.Fatal(err)
		}
	}
	report, err := NewPostgresRepository(pool).FinancePnL(ctx, "2051-09-01", "2051-09-01")
	if err != nil {
		t.Fatal(err)
	}
	if report.Revenue != 100 || report.UnknownCostUnits != 1 {
		t.Fatalf("wrong business day: %+v", report)
	}
}

func TestStage13MarketplacePeriodCompletenessAndIdempotentReimport(t *testing.T) {
	if os.Getenv("CRM_TEST_DATABASE_URL") == "" {
		t.Skip("CRM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("CRM_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	prefix := fmt.Sprintf("stage13-report-%d", time.Now().UnixNano())
	var ownerID, variantID int64
	if err = pool.QueryRow(ctx, `SELECT id FROM customers WHERE email='crm-owner@example.invalid'`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT id FROM product_variants WHERE saby_id='crm-stage07-a'`).Scan(&variantID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM finance_marketplace_adjustments WHERE source_document_id=$1`, prefix)
		_, _ = pool.Exec(ctx, `DELETE FROM finance_marketplace_report_rows WHERE period_id IN (SELECT id FROM finance_marketplace_periods WHERE source_document_id=$1)`, prefix)
		_, _ = pool.Exec(ctx, `DELETE FROM finance_marketplace_periods WHERE source_document_id=$1`, prefix)
		_, _ = pool.Exec(ctx, `DELETE FROM sales_events WHERE source_event_id=$1`, prefix)
	}()
	_, err = pool.Exec(ctx, `INSERT INTO sales_events(channel,source_event_id,source_document_id,source_line_id,event_type,event_status,event_at,external_product_id,canonical_variant_id,units,gross_rub,effect,reconciliation_status,import_batch_id,unit_cost_rub_snapshot,cost_quality) VALUES('ozon',$1,$1,'sale','sale','confirmed','2041-04-10 12:00:00+03','stage13-product',$2,1,1000,1,'counted',gen_random_uuid(),200,'actual')`, prefix, variantID)
	if err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresRepository(pool)
	actor := Actor{CustomerID: ownerID, Role: RoleOwner}
	input := MarketplaceAdjustmentInput{Channel: "ozon", SourceDocumentID: prefix, SourceLineID: "commission-1", OperationDate: "2041-04-10", PeriodStart: "2041-04-01", PeriodEnd: "2041-04-30", PeriodStatus: "incomplete", Kind: "commission", Amount: 100, Effect: -1, Status: "confirmed"}
	if err = repository.CreateMarketplaceAdjustment(ctx, actor, input); err != nil {
		t.Fatal(err)
	}
	report, err := repository.FinancePnL(ctx, "2041-04-01", "2041-04-30")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(report.Warnings, "|"), "Полнота комиссий") {
		t.Fatalf("incomplete period accepted: %+v", report.Warnings)
	}
	input.Amount = 120
	if err = repository.CreateMarketplaceAdjustment(ctx, actor, input); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM finance_marketplace_adjustments WHERE source_document_id=$1`, prefix).Scan(&count); err != nil || count != 1 {
		t.Fatalf("reimport duplicated rows: count=%d err=%v", count, err)
	}
	input.PeriodStatus = "closed"
	if err = repository.CreateMarketplaceAdjustment(ctx, actor, input); err != nil {
		t.Fatal(err)
	}
	report, err = repository.FinancePnL(ctx, "2041-04-01", "2041-04-30")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(report.Warnings, "|"), "Полнота комиссий") {
		t.Fatalf("closed complete period still preliminary for reports: %+v", report.Warnings)
	}
	if report.MarketplaceCosts != 120 {
		t.Fatalf("wrong reimported cost: %v", report.MarketplaceCosts)
	}
	var linked bool
	if err = pool.QueryRow(ctx, `SELECT a.report_row_id IS NOT NULL FROM finance_marketplace_adjustments a WHERE a.source_document_id=$1`, prefix).Scan(&linked); err != nil || !linked {
		t.Fatalf("adjustment is not linked to report row: %v", err)
	}
}

func TestStage13FinanceBalanceGateAndPersistentRule(t *testing.T) {
	if os.Getenv("CRM_TEST_DATABASE_URL") == "" {
		t.Skip("CRM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("CRM_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	prefix := fmt.Sprintf("stage13-bank-%d", time.Now().UnixNano())
	var ownerID, importID, transactionID int64
	if err = pool.QueryRow(ctx, `SELECT id FROM customers WHERE email='crm-owner@example.invalid'`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM finance_classification_rules WHERE created_from_transaction_id IN (SELECT id FROM finance_transactions WHERE import_id IN (SELECT id FROM finance_imports WHERE account_number=$1))`, prefix)
		_, _ = pool.Exec(ctx, `DELETE FROM finance_classification_versions WHERE transaction_id IN (SELECT id FROM finance_transactions WHERE import_id IN (SELECT id FROM finance_imports WHERE account_number=$1))`, prefix)
		_, _ = pool.Exec(ctx, `DELETE FROM finance_transactions WHERE import_id IN (SELECT id FROM finance_imports WHERE account_number=$1)`, prefix)
		_, _ = pool.Exec(ctx, `DELETE FROM finance_imports WHERE account_number=$1`, prefix)
	}()
	err = pool.QueryRow(ctx, `INSERT INTO finance_imports(bank,account_number,file_name,file_sha256,source_file,source_format,status,balance_valid,created_by) VALUES('sber',$1,'bad.xlsx',$1,'x','xlsx','preview',FALSE,$2) RETURNING id`, prefix, ownerID).Scan(&importID)
	if err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresRepository(pool)
	actor := Actor{CustomerID: ownerID, Role: RoleOwner}
	if _, err = repository.ConfirmFinanceImport(ctx, actor, importID); err == nil || !strings.Contains(err.Error(), "остатки выписки не сходятся") {
		t.Fatalf("mismatched statement confirmed: %v", err)
	}
	err = pool.QueryRow(ctx, `INSERT INTO finance_transactions(import_id,bank,account_number,source_row,operation_date,counterparty,purpose,dedupe_key,classification,pnl_effect) VALUES($1,'sber',$2,1,'2042-01-01','А','Первая',$2,'review','review') RETURNING id`, importID, prefix).Scan(&transactionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.ClassifyFinanceTransaction(ctx, actor, transactionID, FinanceClassification{Classification: "commission", PnlEffect: "expense", Reason: "Правило владельца"}); err != nil {
		t.Fatal(err)
	}
	var rules int
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM finance_classification_rules WHERE created_from_transaction_id=$1`, transactionID).Scan(&rules); err != nil || rules != 1 {
		t.Fatalf("classification rule not persisted: count=%d err=%v", rules, err)
	}
}

func TestStage13PackagingIsRecognizedInShipmentMonthAndImmutable(t *testing.T) {
	if os.Getenv("CRM_TEST_DATABASE_URL") == "" {
		t.Skip("CRM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("CRM_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	prefix := fmt.Sprintf("stage13-package-%d", time.Now().UnixNano())
	var variantID, eventID int64
	if err = pool.QueryRow(ctx, `SELECT id FROM product_variants WHERE saby_id='crm-stage07-a'`).Scan(&variantID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM sales_events WHERE source_event_id=$1`, prefix)
		_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE order_number=$1`, prefix)
	}()
	_, err = pool.Exec(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,status) VALUES($1,'Тест','','','cdek',0,1000,1000,'new')`, prefix)
	if err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `INSERT INTO sales_events(channel,source_event_id,source_document_id,source_line_id,event_type,event_status,event_at,external_product_id,canonical_variant_id,units,gross_rub,effect,reconciliation_status,import_batch_id,unit_cost_rub_snapshot,cost_quality) VALUES('site',$1,$1,'1','sale','confirmed','2037-01-31 12:00:00+03','stage13-product',$2,2,1000,1,'counted',gen_random_uuid(),200,'actual') RETURNING id`, prefix, variantID).Scan(&eventID)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM finance_packaging_snapshots WHERE sales_event_id=$1`, eventID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("packaging recognized before shipment: count=%d err=%v", count, err)
	}
	_, err = pool.Exec(ctx, `UPDATE orders SET status='shipped',shipped_at='2037-02-01 09:00:00+03' WHERE order_number=$1`, prefix)
	if err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresRepository(pool)
	january, err := repository.FinancePnL(ctx, "2037-01-01", "2037-01-31")
	if err != nil {
		t.Fatal(err)
	}
	february, err := repository.FinancePnL(ctx, "2037-02-01", "2037-02-28")
	if err != nil {
		t.Fatal(err)
	}
	if january.Packaging != 0 || february.Packaging != 300 {
		t.Fatalf("wrong recognition month: january=%v february=%v", january.Packaging, february.Packaging)
	}
	_, err = pool.Exec(ctx, `UPDATE sales_events SET event_at='2037-03-01 12:00:00+03',units=3 WHERE id=$1`, eventID)
	if err != nil {
		t.Fatal(err)
	}
	var recognized string
	var amount float64
	if err = pool.QueryRow(ctx, `SELECT recognition_date::TEXT,amount_rub::DOUBLE PRECISION FROM finance_packaging_snapshots WHERE sales_event_id=$1`, eventID).Scan(&recognized, &amount); err != nil {
		t.Fatal(err)
	}
	if recognized != "2037-02-01" || amount != 300 {
		t.Fatalf("snapshot changed after recognition: date=%s amount=%v", recognized, amount)
	}
}
