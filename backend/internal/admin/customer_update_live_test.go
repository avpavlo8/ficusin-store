package admin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestUpdateOlderCustomerReadsExactIdentityBeyondFirstPageOnLiveDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	bootstrap, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Close()
	schema := fmt.Sprintf("customer_update_%d", time.Now().UnixNano())
	if _, err = bootstrap.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE") // Disposable private fixture only.
	for _, table := range []string{"customers", "admin_users", "orders", "admin_audit_log"} {
		if _, err = bootstrap.Exec(ctx, "CREATE TABLE "+schema+"."+table+"(LIKE public."+table+" INCLUDING DEFAULTS INCLUDING CONSTRAINTS INCLUDING INDEXES)"); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO customers(id,email,phone,full_name,consent_at,created_at)
  SELECT n,'customer-'||n||'@example.invalid','+7972'||lpad(n::text,7,'0'),'Customer '||n,CURRENT_TIMESTAMP,TIMESTAMPTZ '2020-01-01'+n*INTERVAL '1 day' FROM generate_series(1,201)n;
  UPDATE customers SET lifetime_spend_minor=234567 WHERE id=1;
  INSERT INTO orders(id,order_number,customer_id,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total)
  VALUES(1,'OLD-CUSTOMER-1',1,'Customer 1','+79720000001','customer-1@example.invalid','pickup',0,100,100),
  (2,'OLD-CUSTOMER-2',1,'Customer 1','+79720000001','customer-1@example.invalid','pickup',0,200,200);`)
	repo := NewPostgresRepository(pool)
	page, err := repo.ListCustomersPage(ctx, "", false, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Customers) != 100 || page.Total != 201 || !page.HasMore {
		t.Fatalf("fixture/page wrong: %+v", page)
	}
	for _, c := range page.Customers {
		if c.ID == 1 {
			t.Fatal("fixture's oldest target is on first page")
		}
	}
	name, role, discount, active := "Updated oldest customer", RoleManager, 1250, true
	updated, err := repo.UpdateCustomer(ctx, Actor{CustomerID: 2, Role: RoleOwner}, 1, CustomerUpdate{FullName: &name, AdminRole: &role, RetailDiscountBPS: &discount, Active: &active})
	if err != nil {
		t.Fatalf("committed update of old customer failed to return target: %v", err)
	}
	if updated.ID != 1 || updated.FullName != name || updated.Email != "customer-1@example.invalid" || updated.AdminRole != RoleManager || updated.RetailDiscountBPS != 1250 || updated.LifetimeSpend != 2345.67 || updated.OrdersCount != 2 || !updated.Active || updated.CreatedAt.Year() != 2020 {
		t.Fatalf("scoped projection lost target/role/order aggregates: %+v", updated)
	}
	var audits int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM admin_audit_log WHERE action='customer.update' AND entity_id='1'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("audit missing/duplicated: %d %v", audits, err)
	}
	other, err := repo.customerByID(ctx, 2)
	if err != nil || other.FullName != "Customer 2" {
		t.Fatalf("updated a different identity: %+v %v", other, err)
	}
	for _, tc := range []struct {
		actor  Actor
		update CustomerUpdate
	}{
		{Actor{CustomerID: 3, Role: RoleManager}, CustomerUpdate{FullName: &name}},
		{Actor{CustomerID: 3, Role: RoleAdministrator}, CustomerUpdate{AdminRole: &role}},
		{Actor{CustomerID: 3, Role: RoleAdministrator}, CustomerUpdate{RetailDiscountBPS: &discount}},
		{Actor{CustomerID: 3, Role: RoleAdministrator}, CustomerUpdate{Active: &active}},
	} {
		if _, err = repo.UpdateCustomer(ctx, tc.actor, 1, tc.update); !errors.Is(err, ErrForbidden) {
			t.Fatalf("permissions changed for %+v: %v", tc.actor, err)
		}
	}
	disabled := false
	if _, err = repo.UpdateCustomer(ctx, Actor{CustomerID: 1, Role: RoleOwner}, 1, CustomerUpdate{Active: &disabled}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("owner could disable self: %v", err)
	}
	contact := "Administrator-edited contact"
	updated, err = repo.UpdateCustomer(ctx, Actor{CustomerID: 3, Role: RoleAdministrator}, 1, CustomerUpdate{FullName: &contact})
	if err != nil || updated.ID != 1 || updated.FullName != contact || updated.AdminRole != RoleManager || updated.RetailDiscountBPS != 1250 {
		t.Fatalf("allowed contact update changed financial/role fields: %+v %v", updated, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM admin_audit_log WHERE action='customer.update' AND entity_id='1'`).Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("forbidden updates changed audit/state: %d %v", audits, err)
	}
}
