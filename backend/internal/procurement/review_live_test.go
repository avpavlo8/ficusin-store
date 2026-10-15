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
