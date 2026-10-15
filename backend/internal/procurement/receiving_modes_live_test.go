package procurement

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReceivingAssortmentAndCustomerHandoffPreserveInvoiceAndLogistics(t *testing.T) {
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
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	unique := time.Now().UnixNano()
	store := NewPostgresStore(pool)
	var actorID, supplierID int64
	must(pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at) VALUES($1,$2,'','Receiving modes test',now()) RETURNING id`, fmt.Sprintf("receiving-%d@example.invalid", unique), fmt.Sprintf("+74%09d", unique%1000000000)).Scan(&actorID))
	must(pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency) VALUES($1,'international','EUR') RETURNING id`, fmt.Sprintf("Receiving %d", unique)).Scan(&supplierID))
	cards := []string{}
	for _, name := range []string{"Лимон на штамбе", "Кумкват на штамбе", "Обычная позиция"} {
		id := fmt.Sprintf("receiving-%d-%d", unique, len(cards))
		cards = append(cards, id)
		_, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,balance,section_path,seen_at) VALUES($1,$1,$2,0,ARRAY['Цветы'],now())`, id, name)
		must(err)
	}
	actor := Actor{CustomerID: actorID, Role: "owner"}
	pot, height := 15.0, 45.0
	parsed := ParsedDocument{ParserKind: "holland_packing_list", DocumentNumber: fmt.Sprintf("RECEIVING-%d", unique), Currency: "EUR", ArithmeticOK: true, ProductSubtotal: 340, DocumentTotal: 340, CalculatedTotal: 340}
	for i, qty := range []int{6, 8, 10} {
		price := 10.0
		if i == 2 {
			price = 20
		}
		parsed.Lines = append(parsed.Lines, ParsedLine{SourcePage: 1, SourceLine: i + 1, RawName: []string{"Citr Mix", "Nepent Mix", "Regular plant"}[i], Quantity: qty, UnitPrice: price, LineTotal: float64(qty) * price, LoadUnit: "1", PotDiameterCM: &pot, HeightCM: &height})
	}
	imported, err := store.ImportDocument(ctx, actor, DocumentUpload{SupplierID: supplierID, FileName: "mix.pdf", ContentType: "application/pdf", Content: []byte(fmt.Sprintf("%%PDF-receiving-%d", unique))}, parsed)
	must(err)
	orderID := imported.Order.ID
	detail, err := store.OrderDetail(ctx, orderID)
	must(err)
	mixID, handoffID, regularID := detail.Lines[0].ID, detail.Lines[1].ID, detail.Lines[2].ID
	regularAlias := detail.Lines[2].MatchAlias.ID
	yes := true
	note := "Позиция поставщика подтверждена"
	for _, line := range detail.Lines {
		_, err = store.UpdateOrderLine(ctx, actor, line.ID, OrderLineUpdate{AcceptComparison: &yes, ComparisonNote: &note})
		must(err)
	}
	_, err = store.ResolveAlias(ctx, actor, regularAlias, AliasResolution{MatchStatus: "confirmed", SabyID: cards[0]})
	must(err)
	_, err = store.ResolveAlias(ctx, actor, detail.Lines[0].MatchAlias.ID, AliasResolution{MatchStatus: "confirmed", SabyID: cards[0]})
	must(err)
	mixMode, handoffMode := "assortment", "customer_handoff"
	_, err = store.UpdateOrderLine(ctx, actor, mixID, OrderLineUpdate{ReceivingMode: &mixMode})
	must(err)
	_, err = store.UpdateOrderLine(ctx, actor, handoffID, OrderLineUpdate{ReceivingMode: &handoffMode})
	must(err)
	detail, err = store.OrderDetail(ctx, orderID)
	must(err)
	if detail.Validation.Unmatched != 0 || !detail.Validation.CanCalculate || detail.Validation.PendingAssortments != 1 || detail.Validation.PendingHandoffs != 1 {
		t.Fatalf("deferred lines blocked invoice calculation %+v", detail.Validation)
	}
	costs := CalculationInput{ExchangeRate: 120, DeliveryToMoscowRUB: 120000, DeliveryToRyazanRUB: 17000}
	detail, err = store.CalculateOrder(ctx, actor, orderID, costs)
	must(err)
	if detail.Validation.CanPrepareReceipt {
		t.Fatal("unallocated mix can be sent")
	}
	if _, err = store.PrepareBatch(ctx, actor, orderID, "receipt", nil); err == nil {
		t.Fatal("receipt accepted unallocated assortment")
	}
	var moscow, ryazan float64
	for _, line := range detail.Lines {
		if line.UnitCostRUB == nil || line.TrolleyDeliveryUnitRUB == nil || line.RyazanDeliveryUnitRUB == nil {
			t.Fatalf("missing costs %+v", line)
		}
		moscow += *line.TrolleyDeliveryUnitRUB * float64(line.Quantity)
		ryazan += *line.RyazanDeliveryUnitRUB * float64(line.Quantity)
	}
	if math.Abs(moscow-120000) > .01 || math.Abs(ryazan-17000) > .01 {
		t.Fatalf("deferred logistics were dropped %.2f %.2f", moscow, ryazan)
	}
	bad := []ReceivingAllocation{{SabyID: cards[0], Quantity: 2}, {SabyID: cards[1], Quantity: 3}}
	if _, err = store.UpdateOrderLine(ctx, actor, mixID, OrderLineUpdate{ReceivingAllocations: &bad}); err == nil {
		t.Fatal("short distribution accepted")
	}
	parts := []ReceivingAllocation{{SabyID: cards[0], Quantity: 2}, {SabyID: cards[1], Quantity: 4}}
	detail, err = store.UpdateOrderLine(ctx, actor, mixID, OrderLineUpdate{ReceivingAllocations: &parts})
	must(err)
	if len(detail.Lines) != 3 || detail.Order.Lines != 3 || detail.Order.Units != 24 || math.Abs(detail.Order.Total-340) > .000001 {
		t.Fatalf("distribution doubled purchase %+v", detail.Order)
	}
	if len(detail.Lines[0].ReceivingAllocations) != 2 {
		t.Fatalf("distribution lost %+v", detail.Lines[0])
	}
	chosenRetail := int64(2000)
	_, err = store.UpdateOrderLine(ctx, actor, mixID, OrderLineUpdate{RetailOverrideRUB: &chosenRetail, ChannelPrices: map[string]int64{"wb": 2700}})
	must(err)
	detail, err = store.CalculateOrder(ctx, actor, orderID, costs)
	must(err)
	if !detail.Validation.CanPrepareReceipt || detail.Validation.PendingAssortments != 0 {
		t.Fatalf("complete distribution blocked %+v", detail.Validation)
	}
	pdfRows, err := receivingPDFRows(detail)
	must(err)
	if len(pdfRows) != 4 {
		t.Fatalf("PDF lost split cards or handoff: %+v", pdfRows)
	}
	// Preserve each card's previous label price even after live catalogue prices move.
	_, err = pool.Exec(ctx, `UPDATE procurement_order_lines SET baseline_site_price_minor=CASE WHEN saby_id=$2 THEN proposed_retail_rub*50 ELSE proposed_retail_rub*100 END WHERE receiving_parent_line_id=$1 AND reconciliation_status<>'superseded'`, mixID, cards[0])
	must(err)
	detail, err = store.OrderDetail(ctx, orderID)
	must(err)
	allocations := detail.Lines[0].ReceivingAllocations
	if len(allocations) != 2 || !allocations[0].PriceChangeNeeded || allocations[0].CurrentRetailRUB <= 0 || allocations[1].PriceChangeNeeded {
		t.Fatalf("mix card label prices lost: %+v", allocations)
	}
	pdfRows, err = receivingPDFRows(detail)
	must(err)
	if !pdfRows[0].ReprintLabel || pdfRows[1].ReprintLabel {
		t.Fatalf("mix label changes not reflected in receiving PDF: %+v", pdfRows)
	}
	var childCosts int
	must(pool.QueryRow(ctx, `SELECT COUNT(*) FROM procurement_order_lines c JOIN procurement_order_lines p ON p.id=c.receiving_parent_line_id WHERE p.id=$1 AND c.reconciliation_status<>'superseded' AND c.unit_cost_rub=p.unit_cost_rub AND c.unit_cost_rub>0 AND c.supplier_alias_id IS NULL AND c.retail_override_rub=p.retail_override_rub AND c.channel_prices=p.channel_prices`, mixID).Scan(&childCosts))
	if childCosts != 2 {
		t.Fatal("child costs or alias isolation failed")
	}
	batch, err := store.PrepareBatch(ctx, actor, orderID, "receipt", nil)
	must(err)
	if len(batch.Items) != 1 || batch.Items[0].Quantity == nil || *batch.Items[0].Quantity != 16 {
		t.Fatalf("receipt lost split units or included customer order %+v", batch)
	}
	var payload struct {
		Lines []struct {
			SabyID   string  `json:"sabyId"`
			Quantity int     `json:"quantity"`
			UnitCost float64 `json:"unitCost"`
		}
	}
	must(json.Unmarshal(batch.Items[0].Payload, &payload))
	quantities := map[string]int{}
	for _, line := range payload.Lines {
		quantities[line.SabyID] += line.Quantity
	}
	if quantities[cards[0]] != 12 || quantities[cards[1]] != 4 || len(quantities) != 2 {
		t.Fatalf("wrong receipt identities %v", quantities)
	}
	var expectedCost, receiptCost float64
	for _, line := range detail.Lines {
		if line.ID != handoffID {
			expectedCost += *line.UnitCostRUB * float64(line.Quantity)
		}
	}
	for _, line := range payload.Lines {
		receiptCost += line.UnitCost * float64(line.Quantity)
	}
	if math.Abs(expectedCost-receiptCost) > .0001 {
		t.Fatalf("receipt inflated cost: %.4f instead of %.4f", receiptCost, expectedCost)
	}
	if _, err = store.UpdateOrderStatus(ctx, actor, orderID, OrderStatusUpdate{Status: "received", Force: true, Note: "test"}); err == nil {
		t.Fatal("force completion hid pending customer handoff")
	}
	_, err = store.UpdateOrderLine(ctx, actor, handoffID, OrderLineUpdate{CustomerHandedOver: &yes})
	must(err)
	detail, err = store.OrderDetail(ctx, orderID)
	must(err)
	if detail.Order.Status != "ready_to_receive" || detail.Validation.PendingHandoffs != 0 || detail.Lines[0].UnitCostRUB == nil {
		t.Fatal("handoff invalidated the calculation")
	}
	// Once a receipt has been queued, allocation changes are forbidden.
	_, err = pool.Exec(ctx, `UPDATE procurement_action_items SET status='queued' WHERE batch_id=$1`, batch.ID)
	must(err)
	if _, err = store.UpdateOrderLine(ctx, actor, mixID, OrderLineUpdate{ReceivingAllocations: &parts}); err == nil {
		t.Fatal("sent receipt distribution was editable")
	}
	// A purchase consisting only of a customer handoff needs no fictitious
	// Saby stock document, but cannot close until the handoff is confirmed.
	parsed.DocumentNumber += "-HANDOFF"
	parsed.Lines = parsed.Lines[1:2]
	parsed.ProductSubtotal = 80
	parsed.DocumentTotal = 80
	parsed.CalculatedTotal = 80
	customerOrder, err := store.ImportDocument(ctx, actor, DocumentUpload{SupplierID: supplierID, FileName: "customer.pdf", ContentType: "application/pdf", Content: []byte(fmt.Sprintf("%%PDF-customer-%d", unique))}, parsed)
	must(err)
	customerDetail, err := store.OrderDetail(ctx, customerOrder.Order.ID)
	must(err)
	customerLine := customerDetail.Lines[0].ID
	_, err = store.UpdateOrderLine(ctx, actor, customerLine, OrderLineUpdate{AcceptComparison: &yes, ComparisonNote: &note})
	must(err)
	_, err = store.UpdateOrderLine(ctx, actor, customerLine, OrderLineUpdate{ReceivingMode: &handoffMode})
	must(err)
	customerDetail, err = store.CalculateOrder(ctx, actor, customerOrder.Order.ID, costs)
	must(err)
	if customerDetail.Validation.CanPrepareReceipt {
		t.Fatal("all-customer purchase offers an empty stock receipt")
	}
	if _, err = store.UpdateOrderStatus(ctx, actor, customerOrder.Order.ID, OrderStatusUpdate{Status: "received"}); err == nil {
		t.Fatal("customer purchase closed before handoff")
	}
	_, err = store.UpdateOrderLine(ctx, actor, customerLine, OrderLineUpdate{CustomerHandedOver: &yes})
	must(err)
	customerDetail, err = store.UpdateOrderStatus(ctx, actor, customerOrder.Order.ID, OrderStatusUpdate{Status: "received"})
	must(err)
	if customerDetail.Order.Status != "received" {
		t.Fatal("confirmed customer handoff did not finish")
	}
	// Detach an accidental card without dropping the plant from logistics.
	parsed.DocumentNumber += "-NEW-CARD"
	parsed.Lines[0].RawName = "New citrus card"
	detached, err := store.ImportDocument(ctx, actor, DocumentUpload{SupplierID: supplierID, FileName: "new-card.pdf", ContentType: "application/pdf", Content: []byte(fmt.Sprintf("%%PDF-new-card-%d", unique))}, parsed)
	must(err)
	pendingDetail, err := store.OrderDetail(ctx, detached.Order.ID)
	must(err)
	pendingLine := pendingDetail.Lines[0].ID
	pendingAlias := pendingDetail.Lines[0].MatchAlias.ID
	_, err = store.UpdateOrderLine(ctx, actor, pendingLine, OrderLineUpdate{AcceptComparison: &yes, ComparisonNote: &note})
	must(err)
	_, err = store.ResolveAlias(ctx, actor, pendingAlias, AliasResolution{MatchStatus: "confirmed", SabyID: cards[0]})
	must(err)
	awaiting := "awaiting_catalog"
	pendingDetail, err = store.UpdateOrderLine(ctx, actor, pendingLine, OrderLineUpdate{ReceivingMode: &awaiting})
	must(err)
	if pendingDetail.Lines[0].SabyID != "" || !pendingDetail.Validation.CanCalculate || pendingDetail.Validation.PendingCatalog != 1 {
		t.Fatalf("detach lost the billable line: %+v", pendingDetail)
	}
	var oldAliasCard string
	must(pool.QueryRow(ctx, `SELECT COALESCE(matched_saby_id,'') FROM procurement_supplier_aliases WHERE id=$1`, pendingAlias).Scan(&oldAliasCard))
	if oldAliasCard != "" {
		t.Fatal("detached alias would remap the wrong card on next import")
	}
	pendingDetail, err = store.CalculateOrder(ctx, actor, detached.Order.ID, costs)
	must(err)
	if pendingDetail.Validation.CanPrepareReceipt {
		t.Fatal("missing card did not block stock receipt")
	}
	if _, err = store.PrepareBatch(ctx, actor, detached.Order.ID, "receipt", nil); err == nil {
		t.Fatal("receipt accepted a missing card")
	}
	newCard := fmt.Sprintf("new-citrus-%d", unique)
	_, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,balance,section_path,seen_at) VALUES($1,$1,'New citrus',0,ARRAY['Цветы'],now())`, newCard)
	must(err)
	_, err = store.ResolveAlias(ctx, actor, pendingAlias, AliasResolution{MatchStatus: "confirmed", SabyID: newCard})
	must(err)
	pendingDetail, err = store.OrderDetail(ctx, detached.Order.ID)
	must(err)
	if pendingDetail.Lines[0].ReceivingMode != "catalog" || pendingDetail.Lines[0].SabyID != newCard || pendingDetail.Validation.PendingCatalog != 0 {
		t.Fatalf("new card not applied %+v", pendingDetail)
	}
	var regularQty int
	must(pool.QueryRow(ctx, `SELECT invoiced_qty FROM procurement_order_lines WHERE id=$1`, regularID).Scan(&regularQty))
	if regularQty != 10 {
		t.Fatal("unrelated line changed")
	}
}
