package admin

import (
	"context"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStage11PnLAndSupplierAccountOnLiveDatabase(t *testing.T) {
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
	var ownerID, variantID int64
	if err = pool.QueryRow(ctx, `SELECT id FROM customers WHERE email='crm-owner@example.invalid'`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT id FROM product_variants WHERE saby_id='crm-stage07-a'`).Scan(&variantID); err != nil {
		t.Fatal(err)
	}
	prefix := fmt.Sprintf("stage11-%d", time.Now().UnixNano())
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM supplier_account_reconciliations WHERE source_reference LIKE $1`, prefix+"%")
		_, _ = pool.Exec(ctx, `DELETE FROM supplier_account_operations WHERE source_reference LIKE $1`, prefix+"%")
		_, _ = pool.Exec(ctx, `DELETE FROM finance_tax_periods WHERE period_start='2036-09-01' AND period_end='2036-09-30'`)
		_, _ = pool.Exec(ctx, `DELETE FROM finance_marketplace_adjustments WHERE source_document_id LIKE $1`, prefix+"%")
		_, _ = pool.Exec(ctx, `DELETE FROM marketplace_returns WHERE source_return_id LIKE $1`, prefix+"%")
		_, _ = pool.Exec(ctx, `DELETE FROM finance_cash_entries WHERE linked_transaction_id IN (SELECT id FROM finance_transactions WHERE import_id IN (SELECT id FROM finance_imports WHERE file_sha256=$1))`, prefix)
		_, _ = pool.Exec(ctx, `DELETE FROM finance_transactions WHERE import_id IN (SELECT id FROM finance_imports WHERE file_sha256=$1)`, prefix)
		_, _ = pool.Exec(ctx, `DELETE FROM finance_imports WHERE file_sha256=$1`, prefix)
		_, _ = pool.Exec(ctx, `DELETE FROM sales_events WHERE source_event_id LIKE $1`, prefix+"%")
	}()
	insertSale := func(channel, id, eventType string, units int, gross float64, effect int, cost *float64, quality *string) int64 {
		t.Helper()
		var eventID int64
		err = pool.QueryRow(ctx, `INSERT INTO sales_events(channel,source_event_id,source_document_id,source_line_id,event_type,event_status,event_at,external_product_id,canonical_variant_id,units,gross_rub,effect,reconciliation_status,import_batch_id,unit_cost_rub_snapshot,cost_quality) VALUES($1,$2,$2,'1',$3,'confirmed','2036-09-10 12:00:00+03','stage11-product',$4,$5,$6,$7,'counted',gen_random_uuid(),$8,$9) RETURNING id`, channel, id, eventType, variantID, units, gross, effect, cost, quality).Scan(&eventID)
		if err != nil {
			t.Fatal(err)
		}
		return eventID
	}
	actual, estimated := 300.0, 250.0
	actualKind, estimatedKind := "actual", "estimated"
	saleID := insertSale("ozon", prefix+"-sale", "sale", 2, 2000, 1, &actual, &actualKind)
	insertSale("ozon", prefix+"-return", "return", 1, 1000, -1, &actual, &actualKind)
	insertSale("site", prefix+"-estimated", "sale", 1, 500, 1, &estimated, &estimatedKind)
	insertSale("saby", prefix+"-unknown", "sale", 1, 300, 1, nil, nil)
	_, err = pool.Exec(ctx, `INSERT INTO marketplace_returns(channel,source_return_id,source_unit_index,sales_event_id,canonical_variant_id,returned_at,condition,unit_cost_rub_snapshot,cost_outcome) VALUES('ozon',$1,1,$3,$2,'2036-09-10','ready',300,'restored'),('ozon',$1||'-dead',1,$3,$2,'2036-09-10','dead',100,'lost')`, prefix+"-return", variantID, saleID)
	if err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresRepository(pool)
	actor := Actor{CustomerID: ownerID, Role: RoleOwner}
	if err = repository.CreateMarketplaceAdjustment(ctx, actor, MarketplaceAdjustmentInput{Channel: "ozon", SourceDocumentID: prefix + "-report", SourceLineID: "1", OperationDate: "2036-09-10", Kind: "commission", Amount: 100, Effect: -1, Status: "confirmed"}); err != nil {
		t.Fatal(err)
	}
	var importID int64
	err = pool.QueryRow(ctx, `INSERT INTO finance_imports(bank,account_number,file_name,file_sha256,source_file,source_format,status,rows_total,rows_new,created_by) VALUES('tochka','stage11','stage11.xlsx',$1,'x','xlsx','confirmed',3,3,$2) RETURNING id`, prefix, ownerID).Scan(&importID)
	if err != nil {
		t.Fatal(err)
	}
	var transferID int64
	err = pool.QueryRow(ctx, `INSERT INTO finance_transactions(import_id,bank,account_number,source_row,operation_date,dedupe_key,debit_rub,credit_rub,classification,pnl_effect,confirmed) VALUES($1,'tochka','stage11',1,'2036-09-10',$2,50,0,'other','expense',TRUE),($1,'tochka','stage11',2,'2036-09-10',$2||'-pack',200,0,'packaging_material','none',TRUE),($1,'tochka','stage11',3,'2036-09-10',$2||'-payout',0,1000,'marketplace_payout','income',TRUE),($1,'tochka','stage11',4,'2036-09-10',$2||'-transfer',0,500,'own_transfer','none',TRUE) RETURNING id`, importID, prefix).Scan(&transferID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO finance_cash_entries(operation_date,kind,direction,amount_rub,purpose,linked_transaction_id,created_by) VALUES('2036-09-10','bank_transfer','out',500,'Внесение в банк',$1,$2)`, transferID, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO marketplace_returns(channel,source_return_id,source_unit_index,canonical_variant_id,returned_at,condition,unit_cost_rub_snapshot,cost_outcome) VALUES('ozon',$1,1,$2,'2036-09-10','ready',900,'restored')`, prefix+"-unlinked", variantID); err != nil { t.Fatal(err) }
	if _, err = pool.Exec(ctx, `INSERT INTO finance_transactions(import_id,bank,account_number,source_row,operation_date,dedupe_key,debit_rub,credit_rub,classification,pnl_effect,confirmed) VALUES($1,'tochka','stage11',5,'2036-09-10',$2,900,0,'supplier','expense',TRUE)`, importID, prefix+"-supplier"); err != nil { t.Fatal(err) }
	report, err := repository.FinancePnL(ctx, "2036-09-01", "2036-09-30")
	if err != nil {
		t.Fatal(err)
	}
	assertNear := func(name string, got, want float64) {
		t.Helper()
		if math.Abs(got-want) > .001 {
			t.Fatalf("%s=%v want %v", name, got, want)
		}
	}
	assertNear("revenue", report.Revenue, 1800)
	assertNear("cogs", report.COGS, 550)
	assertNear("packaging", report.Packaging, 300)
	assertNear("marketplace costs", report.MarketplaceCosts, 100)
	assertNear("operating expenses", report.OperatingExpenses, 50)
	assertNear("profit before tax", report.ProfitBeforeTax, 800)
	assertNear("return loss", report.ReturnLoss, 100)
	assertNear("cash flow", report.CashFlow, -150)
	if report.NetProfit != nil || !report.Preliminary || report.ActualCostUnits != 2 || report.EstimatedCostUnits != 1 || report.UnknownCostUnits != 1 {
		t.Fatalf("unexpected completeness: %+v", report)
	}
	if err = repository.SaveFinanceTax(ctx, actor, TaxPeriodInput{From: "2036-09-01", To: "2036-09-30", ActualTax: 80, Source: prefix + "-tax"}); err != nil {
		t.Fatal(err)
	}
	report, err = repository.FinancePnL(ctx, "2036-09-01", "2036-09-30")
	if err != nil || report.NetProfit == nil {
		t.Fatalf("tax was not applied: %+v err=%v", report, err)
	}
	assertNear("net profit", *report.NetProfit, 720)
	opening, err := repository.CreateSupplierAccountOperation(ctx, actor, SupplierAccountInput{OperationDate: "2036-09-01", Kind: "opening", OriginalAmountEUR: -240.84, SourceReference: prefix + "-opening"})
	if err != nil || opening.NormalizedAmountEUR != 240.84 {
		t.Fatalf("opening sign: %+v %v", opening, err)
	}
	topup, err := repository.CreateSupplierAccountOperation(ctx, actor, SupplierAccountInput{OperationDate: "2036-09-09", Kind: "topup", OriginalAmountEUR: -3460, SourceReference: prefix + "-topup"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repository.CreateSupplierAccountOperation(ctx, actor, SupplierAccountInput{OperationDate: "2036-09-11", Kind: "correction", OriginalAmountEUR: 40, LinkedTopupID: &topup.ID, SourceReference: prefix + "-correction"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repository.CreateSupplierAccountOperation(ctx, actor, SupplierAccountInput{OperationDate: "2036-09-11", Kind: "correction", OriginalAmountEUR: -10, LinkedTopupID: &topup.ID, SourceReference: prefix + "-correction-plus"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repository.CreateSupplierAccountOperation(ctx, actor, SupplierAccountInput{OperationDate: "2036-09-10", Kind: "trolley_return", OriginalAmountEUR: -150, SourceReference: prefix + "-trolley"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repository.CreateSupplierAccountOperation(ctx, actor, SupplierAccountInput{OperationDate: "2036-09-10", Kind: "invoice", OriginalAmountEUR: 1000, SourceReference: prefix + "-invoice"})
	if err != nil {
		t.Fatal(err)
	}
	if err = repository.ReconcileSupplierAccount(ctx, actor, SupplierReconciliationInput{Date: "2036-09-11", OriginalBalanceEUR: -2820.84, SourceReference: prefix + "-reconcile"}); err != nil {
		t.Fatal(err)
	}
	account, err := repository.SupplierAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertNear("supplier balance", account.BalanceEUR, 2820.84)
	if account.OriginalBalanceEUR == nil || *account.OriginalBalanceEUR != -2820.84 || account.AvailableEUR != nil {
		t.Fatalf("supplier reconciliation lost sign or invented reserve: %+v", account)
	}
}
