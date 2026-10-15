package procurement

import (
	"context"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReviewLegacyPriceRepairRequiresExactAuditAndIsIdempotent(t *testing.T) {
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
	unique := time.Now().UnixNano()
	var customer, supplier int64
	if err := pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at) VALUES($1,$2,'','Legacy repair test',now()) RETURNING id`, fmt.Sprintf("legacy-review-%d@example.invalid", unique), fmt.Sprintf("+78%09d", unique%1000000000)).Scan(&customer); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency) VALUES($1,'international','EUR') RETURNING id`, fmt.Sprintf("Legacy review %d", unique)).Scan(&supplier); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresStore(pool)
	actor := Actor{CustomerID: customer, Role: "owner"}
	pot, height := 14.0, 40.0
	type fixture struct {
		line, order int64
		price, cost float64
		repair      bool
	}
	cases := map[string]fixture{}
	for _, name := range []string{"fraction", "below-one", "cancelled", "closed", "invoice", "identity-changed", "cost-changed", "no-audit"} {
		price := 4.85
		if name == "below-one" {
			price = .85
		}
		order, err := store.CreatePlan(ctx, actor, PlanCreate{SupplierID: supplier, Costs: &CalculationInput{ExchangeRate: 120, DeliveryToMoscowRUB: 1200}, Items: []PlanItem{{RawName: fmt.Sprintf("Legacy %s %d", name, unique), Quantity: 1, ExpectedUnitPrice: price, PotDiameterCM: &pot, HeightCM: &height, LoadUnit: "shelf"}}})
		if err != nil {
			t.Fatal(err)
		}
		v := fixture{order: order.ID, price: price, repair: name == "fraction" || name == "below-one" || name == "cancelled"}
		if err := pool.QueryRow(ctx, `UPDATE procurement_order_lines SET expected_unit_price=NULLIF(TRUNC(expected_unit_price),0) WHERE procurement_order_id=$1 RETURNING id,unit_cost_rub::DOUBLE PRECISION`, order.ID).Scan(&v.line, &v.cost); err != nil {
			t.Fatal(err)
		}
		switch name {
		case "cancelled":
			_, err = pool.Exec(ctx, `UPDATE procurement_orders SET status='cancelled' WHERE id=$1`, order.ID)
		case "closed":
			_, err = pool.Exec(ctx, `UPDATE procurement_orders SET status='received' WHERE id=$1`, order.ID)
		case "invoice":
			_, err = pool.Exec(ctx, `INSERT INTO procurement_documents(supplier_id,procurement_order_id,file_name,content_type,size_bytes,sha256,content,parser_kind,parser_version,parse_status,arithmetic_status,document_number,currency,line_count,unit_count,created_by) VALUES($1,$2,'legacy.pdf','application/pdf',8,$3,'12345678','holland_packing_list',1,'review','ok','LEGACY-TEST','EUR',1,1,$4)`, supplier, order.ID, fmt.Sprintf("%064x", unique), customer)
		case "identity-changed":
			_, err = pool.Exec(ctx, `UPDATE procurement_order_lines SET raw_name='Edited after creation' WHERE id=$1`, v.line)
		case "cost-changed":
			_, err = pool.Exec(ctx, `UPDATE procurement_order_lines SET purchase_unit_rub=999 WHERE id=$1`, v.line)
		case "no-audit":
			_, err = pool.Exec(ctx, `UPDATE admin_audit_log SET action='procurement.plan.create.test-untrusted' WHERE entity_type='procurement_order' AND entity_id=$1 AND action='procurement.plan.create'`, fmt.Sprint(order.ID))
		}
		if err != nil {
			t.Fatal(err)
		}
		cases[name] = v
	}
	migration, err := os.ReadFile("../../../timeweb/migrations/20261015_zz_procurement_plan_price_repair.sql")
	if err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 2; pass++ {
		if _, err := pool.Exec(ctx, string(migration)); err != nil {
			t.Fatal(err)
		}
		for name, v := range cases {
			var price, cost float64
			var audits int
			if err := pool.QueryRow(ctx, `SELECT COALESCE(expected_unit_price,0)::DOUBLE PRECISION,unit_cost_rub::DOUBLE PRECISION FROM procurement_order_lines WHERE id=$1`, v.line).Scan(&price, &cost); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_audit_log WHERE action='procurement.plan.price.repair' AND entity_type='procurement_order_line' AND entity_id=$1`, fmt.Sprint(v.line)).Scan(&audits); err != nil {
				t.Fatal(err)
			}
			want := math.Trunc(v.price)
			wantAudits := 0
			if v.repair {
				want = v.price
				wantAudits = 1
			}
			if price != want || cost != v.cost || audits != wantAudits {
				t.Fatalf("%s pass %d: price=%v want=%v cost=%v want=%v audit=%d want=%d", name, pass, price, want, cost, v.cost, audits, wantAudits)
			}
		}
	}
}

func TestReviewFractionalPlanPricesSurviveDatabaseRoundTrip(t *testing.T) {
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
	unique := time.Now().UnixNano()
	var customer, supplier int64
	if err := pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at) VALUES($1,$2,'','Review regression',now()) RETURNING id`, fmt.Sprintf("review-%d@example.invalid", unique), fmt.Sprintf("+79%09d", unique%1000000000)).Scan(&customer); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,default_currency) VALUES($1,'international','EUR') RETURNING id`, fmt.Sprintf("Review %d", unique)).Scan(&supplier); err != nil {
		t.Fatal(err)
	}
	saby := fmt.Sprintf("review-decimal-%d", unique)
	if _, err := pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,balance) VALUES($1,$1,'Review fractional plant',0)`, saby); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresStore(pool)
	actor := Actor{CustomerID: customer, Role: "owner"}
	pot, height := 12.5, 40.5
	for _, price := range []float64{4.85, 4.25, 0.0001} {
		input := PlanCreate{SupplierID: supplier, Title: "Review exact price", SourceKind: SourceManual, Costs: &CalculationInput{ExchangeRate: 120, DeliveryToMoscowRUB: 1200}, Items: []PlanItem{
			{SabyID: saby, RawName: "Review fractional plant", Category: "Ficus", Quantity: 12, PackageCount: 2, UnitsPerPackage: 6, ExpectedUnitPrice: price, PotDiameterCM: &pot, HeightCM: &height, LoadUnit: "shelf"},
			{RawName: "Review manual plant", Category: "Olea", Quantity: 6, PackageCount: 1, UnitsPerPackage: 6, ExpectedUnitPrice: price, PotDiameterCM: &pot, HeightCM: &height, LoadUnit: "shelf"},
		}}
		order, err := store.CreatePlan(ctx, actor, input)
		if err != nil {
			t.Fatal(err)
		}
		if order.Title != input.Title || order.SourceKind != SourceManual || order.Units != 18 {
			t.Fatalf("metadata lost: %+v", order)
		}
		detail, err := store.OrderDetail(ctx, order.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(detail.Lines) != 2 {
			t.Fatalf("lost lines: %+v", detail.Lines)
		}
		for _, line := range detail.Lines {
			if line.ExpectedUnitPrice == nil || math.Abs(*line.ExpectedUnitPrice-price) > 0.0000001 || line.PotDiameterCM == nil || *line.PotDiameterCM != pot || line.HeightCM == nil || *line.HeightCM != height {
				t.Fatalf("database truncated price/dimensions: %+v; expected %f", line, price)
			}
		}
		var aliasPrice float64
		if err := pool.QueryRow(ctx, `SELECT expected_unit_price::DOUBLE PRECISION FROM procurement_supplier_aliases WHERE supplier_id=$1 AND raw_name='Review fractional plant'`, supplier).Scan(&aliasPrice); err != nil {
			t.Fatal(err)
		}
		if math.Abs(aliasPrice-price) > 0.0000001 {
			t.Fatalf("alias price %f != %f", aliasPrice, price)
		}
	}
	// Completed business status and verification state must remain independent.
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,source_kind,currency,status,created_by) VALUES($1,'invoice','EUR','received',$2) RETURNING id`, supplier, customer).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,raw_name,invoiced_qty,unit_price,reconciliation_status,comparison_accepted) VALUES($1,'Pending invoice row',65,4.85,'added',false)`, id); err != nil {
		t.Fatal(err)
	}
	summary, err := loadOrderSummary(ctx, pool, id)
	if err != nil {
		t.Fatal(err)
	}
	if summary.PendingReview != 1 || summary.Status != "received" || summary.Units != 0 {
		t.Fatalf("misleading verification summary: %+v", summary)
	}
	request, err := store.CreateRequest(ctx, actor, RequestCreate{Kind: "staff_recommendation", SabyID: saby, RequestedName: "Review request", Quantity: 10, Source: "manual", DueDate: "2030-02-15", Assignee: "Test buyer"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.CreatePlan(ctx, actor, PlanCreate{SupplierID: supplier, Title: "Request allocation", SourceKind: SourceManual, Costs: &CalculationInput{ExchangeRate: 120}, Items: []PlanItem{{SabyID: saby, RawName: "Review fractional plant", Quantity: 6, ExpectedUnitPrice: 4.85, PotDiameterCM: &pot, HeightCM: &height, LoadUnit: "shelf"}}})
	if err != nil {
		t.Fatal(err)
	}
	due, assignee := "2030-02-16", "Second buyer"
	edit := RequestUpdate{SabyID: saby, RequestedName: request.RequestedName, Quantity: 1, Status: "open", DueDate: &due, Assignee: &assignee}
	if _, err := store.UpdateRequest(ctx, actor, request.ID, edit); err == nil {
		t.Fatal("allocated quantity was silently reduced")
	}
	edit.Quantity = 10
	edit.Status = "cancelled"
	if _, err := store.UpdateRequest(ctx, actor, request.ID, edit); err != nil {
		t.Fatal(err)
	}
	requests, err := store.listRequests(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range requests {
		if item.ID == request.ID {
			found = true
			if item.Status != "cancelled" || item.AllocatedQuantity != 0 || item.DueDate != *edit.DueDate || item.Assignee != *edit.Assignee || len(item.History) < 2 || item.History[0].DueDate != due || item.History[0].Assignee != assignee {
				t.Fatalf("request cancellation/history lost: %+v", item)
			}
		}
	}
	if !found {
		t.Fatal("cancelled request disappeared from history")
	}
	edit.DueDate = nil
	edit.Assignee = nil // An older client changing only status must preserve the new metadata.
	edit.Status = "open"
	if _, err := store.UpdateRequest(ctx, actor, request.ID, edit); err != nil {
		t.Fatal(err)
	}
	requests, err = store.listRequests(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range requests {
		if item.ID == request.ID && (item.DueDate != due || item.Assignee != assignee) {
			t.Fatalf("partial update erased metadata: %+v", item)
		}
	}
	inputSupplier := SupplierUpdate{SupplierCreate: SupplierCreate{Name: fmt.Sprintf("Review edited %d", unique), Kind: KindInternational, CountryCode: "NL", DefaultCurrency: "EUR"}, Active: false}
	archived, err := store.UpdateSupplier(ctx, actor, supplier, inputSupplier)
	if err != nil {
		t.Fatal(err)
	}
	if archived.Active {
		t.Fatal("supplier did not archive")
	}
	history, err := store.OrderDetail(ctx, id)
	if err != nil || history.Order.ID != id {
		t.Fatalf("archiving removed history: %v", err)
	}
	inputSupplier.Kind = KindDomestic
	if _, err := store.UpdateSupplier(ctx, actor, supplier, inputSupplier); err == nil {
		t.Fatal("changed pricing kind of historical supplier")
	}
	inputSupplier.Kind = KindInternational
	inputSupplier.Active = true
	if _, err := store.UpdateSupplier(ctx, actor, supplier, inputSupplier); err != nil {
		t.Fatal(err)
	}
	query := fmt.Sprintf("qa-pagination-%d", unique)
	if _, err := pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,balance) SELECT $1||'-'||n::TEXT,$1||'-'||n::TEXT,$1||'-'||n::TEXT,0 FROM generate_series(1,301) n`, query); err != nil {
		t.Fatal(err)
	}
	page, err := store.ProductPage(ctx, supplier, query, "", 100, 300)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Total != 301 || page.FolderCounts[""] != 301 {
		t.Fatalf("catalogue still truncated: %+v", page)
	}
	example, err := store.PricingExample(ctx, (func() PricingSettings {
		v, e := store.loadSettings(ctx)
		if e != nil {
			t.Fatal(e)
		}
		return v
	})())
	if err != nil {
		t.Fatal(err)
	}
	if example.Before.Retail != example.After.Retail {
		t.Fatal("unchanged formula has different preview")
	}

}
