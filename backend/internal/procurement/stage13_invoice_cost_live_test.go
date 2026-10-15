package procurement

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStage13InvoiceCalculationDoesNotRevalueExistingStock(t *testing.T) {
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
	store := NewPostgresStore(pool)
	unique := time.Now().UnixNano()
	sabyID := fmt.Sprintf("stage13-invoice-cost-%d", unique)
	var productID, variantID, actorID, supplierID, orderID int64
	if _, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,balance,price_minor)
		VALUES($1,$1,'Stage 13 invoice cost',0,180000)`, sabyID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO products(name,slug,status,catalog_section)
		VALUES('Stage 13 invoice cost',$1,'active','plants') RETURNING id`, fmt.Sprintf("stage-13-invoice-cost-%d", unique)).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO product_variants(product_id,saby_id,sku,label,base_price_minor,is_active,
		current_unit_cost_rub,current_unit_cost_kind,current_unit_cost_effective_at)
		VALUES($1,$2,$3,'D12',180000,1,900,'estimated','2035-01-01') RETURNING id`, productID, sabyID, fmt.Sprintf("7%017d", unique%100000000000000000)).Scan(&variantID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO procurement_cost_history(canonical_variant_id,saby_id,unit_cost_rub,cost_kind,source,effective_at)
		VALUES($1,$2,900,'estimated','saby_retail_half_initial','2035-01-01')`, variantID, sabyID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at)
		VALUES($1,$2,'','Stage 13 owner',CURRENT_TIMESTAMP) RETURNING id`, fmt.Sprintf("stage13-invoice-%d@example.invalid", unique), fmt.Sprintf("+77%09d", unique%1000000000)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency)
		VALUES($1,'domestic','RUB') RETURNING id`, fmt.Sprintf("Stage 13 invoice supplier %d", unique)).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,order_number,source_kind,currency,status,created_by)
		VALUES($1,$2,'payment_invoice','RUB','review',$3) RETURNING id`, supplierID, fmt.Sprintf("STAGE-13-INVOICE-%d", unique), actorID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	var documentID int64
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_documents(supplier_id,procurement_order_id,file_name,content_type,
		size_bytes,sha256,content,parser_kind,parser_version,parse_status,arithmetic_status,document_number,
		currency,line_count,unit_count,product_subtotal,document_total,calculated_total,extracted_text,created_by,revision_no)
		VALUES($1,$2,'stage13-invoice.pdf','application/pdf',4,$3,decode('25504446','hex'),'domestic_payment_invoice',1,
		'parsed','ok',$4,'RUB',1,20,7000,7000,7000,'Stage 13 synthetic invoice',$5,1) RETURNING id`,
		supplierID, orderID, fmt.Sprintf("%064x", unique), fmt.Sprintf("INV-%d", unique), actorID).Scan(&documentID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,procurement_document_id,saby_id,
		canonical_variant_id,raw_name,ordered_qty,invoiced_qty,unit_price,line_total,match_status,reconciliation_status)
		VALUES($1,$2,$3,$4,'Stage 13 invoice cost',20,20,350,7000,'confirmed','matched')`, orderID, documentID, sabyID, variantID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.CalculateOrder(ctx, Actor{CustomerID: actorID, Role: "owner"}, orderID, CalculationInput{}); err != nil {
		t.Fatal(err)
	}
	var cost float64
	var kind, status string
	if err = pool.QueryRow(ctx, `SELECT current_unit_cost_rub::DOUBLE PRECISION,current_unit_cost_kind
		FROM product_variants WHERE id=$1`, variantID).Scan(&cost, &kind); err != nil || cost != 900 || kind != "estimated" {
		t.Fatalf("current cost=%.2f kind=%s err=%v", cost, kind, err)
	}
	var historyCount int
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM procurement_cost_history WHERE procurement_order_id=$1 AND canonical_variant_id=$2`, orderID, variantID).Scan(&historyCount); err != nil || historyCount != 0 {
		t.Fatalf("calculation wrote actual cost history: %d, err=%v", historyCount, err)
	}
	var incomingCost float64
	if err = pool.QueryRow(ctx, `SELECT unit_cost_rub::DOUBLE PRECISION FROM procurement_order_lines WHERE procurement_order_id=$1`, orderID).Scan(&incomingCost); err != nil || incomingCost != 350 {
		t.Fatalf("incoming batch cost=%v err=%v", incomingCost, err)
	}
	if err = pool.QueryRow(ctx, `SELECT status FROM procurement_orders WHERE id=$1`, orderID).Scan(&status); err != nil || status != "ready_to_receive" {
		t.Fatalf("order status=%q err=%v", status, err)
	}
	actor := Actor{CustomerID: actorID, Role: "owner"}
	if _, err = store.PrepareBatch(ctx, actor, orderID, "prices", []string{"wb"}); err == nil {
		t.Fatal("unknown tariffs allowed a marketplace price")
	}
	var lineID int64
	if err = pool.QueryRow(ctx, `SELECT id FROM procurement_order_lines WHERE procurement_order_id=$1`, orderID).Scan(&lineID); err != nil {
		t.Fatal(err)
	}
	profiles := map[string]ChannelCosts{"site": zeroCosts(), "wb": {amount(.36), amount(0), amount(150), amount(100), amount(30)}, "ozon": {amount(.52), amount(.01), amount(150), amount(200), amount(30)}}
	retail := int64(1650)
	if _, err = store.UpdateOrderLine(ctx, actor, lineID, OrderLineUpdate{ChannelCosts: profiles, RetailOverrideRUB: &retail}); err != nil {
		t.Fatal(err)
	}
	detail, err := store.CalculateOrder(ctx, actor, orderID, CalculationInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Lines) != 1 || detail.Lines[0].ProposedRetailRUB == nil || *detail.Lines[0].ProposedRetailRUB != 1650 {
		t.Fatalf("market retail override lost: %+v", detail.Lines)
	}
	settings, err := store.loadSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings.TaxRate != .08 {
		t.Fatalf("fixture expects 8 percent revenue tax, got %v", settings.TaxRate)
	}
	prepared := map[string]int64{}
	for channel, want := range map[string]float64{"site": 1650, "wb": 3211, "ozon": 4867} {
		batch, err := store.PrepareBatch(ctx, actor, orderID, "prices", []string{channel})
		if err != nil || len(batch.Items) != 1 || batch.Items[0].NewValue != want {
			t.Fatalf("%s actual prepared price: items=%+v err=%v want=%v", channel, batch.Items, err, want)
		}
		prepared[channel] = batch.ID
		if batch.Items[0].CompareAtValue != nil {
			t.Fatal("invented a strike-through price")
		}
	}
	if _, err = store.ApprovedSabyPriceExportLines(ctx, orderID); err == nil {
		t.Fatal("manual retail exported before site approval")
	}
	if _, err = store.ApproveBatch(ctx, actor, prepared["site"], nil); err != nil {
		t.Fatal(err)
	}
	exported, err := store.ApprovedSabyPriceExportLines(ctx, orderID)
	if err != nil || len(exported) != 1 || *exported[0].ProposedRetailRUB != 1650 {
		t.Fatalf("manual price below threshold missing from Saby XLSX: %+v err=%v", exported, err)
	}
	sabyDraft, err := store.PrepareBatch(ctx, actor, orderID, "prices", []string{"saby_price"})
	if err != nil || len(sabyDraft.Items) != 1 {
		t.Fatalf("manual price missing from Saby draft: %+v err=%v", sabyDraft, err)
	}
	// Editing costs invalidates the order and all draft prices. Preparing new
	// prices without recalculation must fail even if the old rows still exist.
	if _, err = store.UpdateOrderLine(ctx, actor, lineID, OrderLineUpdate{ChannelCosts: profiles}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.PrepareBatch(ctx, actor, orderID, "prices", []string{"wb"}); err == nil {
		t.Fatal("stale quote survived a cost edit")
	}

	// Keeping a profitable site price within the threshold must still allow
	// correcting marketplace prices. The old shared site threshold hid them.
	if _, err = pool.Exec(ctx, `UPDATE procurement_order_lines SET retail_override_rub=NULL,unit_price=786,line_total=15720 WHERE id=$1`, lineID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE procurement_documents SET product_subtotal=15720,document_total=15720,calculated_total=15720 WHERE id=$1`, documentID); err != nil {
		t.Fatal(err)
	}
	kept, err := store.CalculateOrder(ctx, actor, orderID, CalculationInput{})
	if err != nil || *kept.Lines[0].ProposedRetailRUB != 1650 {
		t.Fatalf("small retail change must retain actual profitable site price: %+v err=%v", kept.Lines, err)
	}
	siteDraft, err := store.PrepareBatch(ctx, actor, orderID, "prices", []string{"site"})
	if err != nil || len(siteDraft.Items) != 0 {
		t.Fatalf("unexpected site change: %+v err=%v", siteDraft, err)
	}
	wbDraft, err := store.PrepareBatch(ctx, actor, orderID, "prices", []string{"wb"})
	if err != nil || len(wbDraft.Items) != 1 {
		t.Fatalf("marketplace price incorrectly depends on site change: %+v err=%v", wbDraft, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE procurement_order_lines SET unit_price=350,line_total=7000 WHERE id=$1`, lineID); err != nil {
		t.Fatal(err)
	}
	// Nonreturnable supplier packaging/rent and bank/import costs land in the
	// batch; a refundable trolley deposit remains separate from expense.
	if _, err = pool.Exec(ctx, `UPDATE procurement_suppliers SET kind='international' WHERE id=$1`, supplierID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE procurement_orders SET currency='EUR' WHERE id=$1`, orderID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE procurement_documents SET currency='EUR',parser_kind='holland_packing_list',package_total=211,product_subtotal=7000,calculated_total=7000,document_total=7211,extracted_text='Single Package 10,00
Returnable Package 196,00
Package Rent 5,00' WHERE id=$1`, documentID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE procurement_order_lines SET retail_override_rub=NULL,pot_diameter_cm=12,height_cm=40,load_unit='1' WHERE id=$1`, lineID); err != nil {
		t.Fatal(err)
	}
	detail, err = store.CalculateOrder(ctx, actor, orderID, CalculationInput{ExchangeRate: 100, DeliveryToMoscowRUB: 1000, DeliveryToRyazanRUB: 500, AdditionalCostsRUB: 2000})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Costs.SupplierChargesRUB != 1500 || detail.Costs.ReturnableDepositRUB != 19600 || detail.Costs.AdditionalCostsRUB != 2000 || detail.Lines[0].UnitCostRUB == nil || *detail.Lines[0].UnitCostRUB != 35250 || detail.Lines[0].AdditionalPurchaseUnitRUB == nil || *detail.Lines[0].AdditionalPurchaseUnitRUB != 175 {
		t.Fatalf("supplier charges lost or deposit expensed: costs=%+v line=%+v", detail.Costs, detail.Lines[0])
	}
	// Read-only previews must not change formula version or product cost.
	versionBefore := settings.Version
	example, err := store.PricingExample(ctx, settings)
	if err != nil || example.After.ChannelQuotes == nil {
		t.Fatalf("pricing example=%+v err=%v", example, err)
	}
	currentSettings, err := store.loadSettings(ctx)
	if err != nil || currentSettings.Version != versionBefore {
		t.Fatalf("preview mutated settings: %+v %v", currentSettings, err)
	}
	updated := settings
	updated.Economics = PricingEconomics{MonthlyFixedCostsRUB: 300000, Channels: profiles}
	saved, err := store.UpdateSettings(ctx, actor, updated)
	if err != nil || saved.Economics.MonthlyFixedCostsRUB != 300000 || saved.Economics.Channels["ozon"].CommissionRate == nil || *saved.Economics.Channels["ozon"].CommissionRate != .52 {
		t.Fatalf("pricing profiles not persisted: %+v err=%v", saved, err)
	}
	if _, err = store.UpdateSettings(ctx, actor, updated); err == nil {
		t.Fatal("stale settings editor overwrote newer financial inputs")
	}
	settings.Version = saved.Version
	if _, err = store.UpdateSettings(ctx, actor, settings); err != nil {
		t.Fatalf("restore settings: %v", err)
	}

}
