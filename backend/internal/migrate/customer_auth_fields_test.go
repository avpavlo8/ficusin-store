package migrate

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type retiredCustomerFieldsCallChecker struct{}

func (retiredCustomerFieldsCallChecker) RequestCallCheck(_ context.Context, phone string) (string, string, string, error) {
	return "retired-fields-" + phone, "+79990000000", "+7 999 000-00-00", nil
}
func (retiredCustomerFieldsCallChecker) CallCheckStatus(context.Context, string) (bool, bool, error) {
	return true, false, nil
}

func TestRetiredCustomerFieldsPreservePhoneAuthenticationOnDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	base, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer base.Close()
	raw, e := os.ReadFile("../../../timeweb/migrations/20261113_zzzzzzzzzzz_retire_customer_password_fields.sql")
	if e != nil {
		t.Fatal(e)
	}
	schema := fmt.Sprintf("retired_customer_fields_%d", time.Now().UnixNano())
	archive := schema + "_archive"
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, e = base.Exec(ctx, "CREATE SCHEMA "+quoted); e != nil {
		t.Fatal(e)
	}
	defer base.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{archive}.Sanitize()+" CASCADE")
	defer base.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE")
	config, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, e := pgxpool.NewWithConfig(ctx, config)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	exec := func(sql string, args ...any) { t.Helper(); _, e := pool.Exec(ctx, sql, args...); must(e) }
	for _, table := range []string{"customers", "auth_sessions", "call_checks", "orders", "admin_users", "consent_events"} {
		id := pgx.Identifier{table}.Sanitize()
		exec("CREATE TABLE " + id + " (LIKE public." + id + " INCLUDING DEFAULTS INCLUDING GENERATED INCLUDING IDENTITY INCLUDING CONSTRAINTS INCLUDING INDEXES)")
	}
	// The fixture works both before and after this migration is applied to the
	// surrounding test database. No production customer or session is read.
	exec(`ALTER TABLE customers ADD COLUMN IF NOT EXISTS password_hash TEXT;
ALTER TABLE customers ADD COLUMN IF NOT EXISTS email_verified BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE auth_sessions ADD FOREIGN KEY(customer_id) REFERENCES customers(id);
ALTER TABLE orders ADD FOREIGN KEY(customer_id) REFERENCES customers(id);`)
	var customer int64
	must(pool.QueryRow(ctx, `INSERT INTO customers(phone,email,password_hash,email_verified,full_name,last_name,patronymic,delivery_address,retail_discount_bps,lifetime_spend_minor,consent_at,created_at,updated_at) VALUES('+79990000001','existing@example.invalid','historical-hash-exact',TRUE,'Existing customer','Last name','Patronymic','Saved address',500,123456,NOW(),TIMESTAMPTZ '2020-01-02 03:04:05+00',TIMESTAMPTZ '2021-02-03 04:05:06+00') RETURNING id`).Scan(&customer))
	exec(`INSERT INTO customers(phone,password_hash,email_verified,full_name,consent_at) VALUES('+79990000002',NULL,FALSE,'Null historical hash',NOW())`)
	exec(`INSERT INTO auth_sessions(token_hash,customer_id,expires_at,user_agent) VALUES(encode(sha256(convert_to('existing-session','UTF8')),'hex'),$1,NOW()+INTERVAL '1 day','Existing browser')`, customer)
	exec(`INSERT INTO orders(order_number,customer_id,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,status) VALUES('RETIRED-AUTH-OWNED',$1,'Existing customer','+79990000001','existing@example.invalid','pickup',0,1490,1490,'completed'),('RETIRED-AUTH-GUEST',NULL,'Existing customer','+79990000001','other@example.invalid','pickup',0,990,990,'new')`, customer)
	var beforeCustomer, beforeSessions, beforeOrders []byte
	must(pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(c)-'password_hash'-'email_verified' ORDER BY id) FROM customers c`).Scan(&beforeCustomer))
	must(pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(s) ORDER BY token_hash) FROM auth_sessions s`).Scan(&beforeSessions))
	must(pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM orders o`).Scan(&beforeOrders))
	exec(strings.ReplaceAll(string(raw), "legacy_archive", archive))
	var afterCustomer, afterSessions, afterOrders []byte
	must(pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM customers c`).Scan(&afterCustomer))
	must(pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(s) ORDER BY token_hash) FROM auth_sessions s`).Scan(&afterSessions))
	must(pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM orders o`).Scan(&afterOrders))
	if string(beforeCustomer) != string(afterCustomer) || string(beforeSessions) != string(afterSessions) || string(beforeOrders) != string(afterOrders) {
		t.Fatal("retirement changed current customer, session or order data")
	}
	var archivedHash string
	var verified bool
	var created, updated time.Time
	must(pool.QueryRow(ctx, "SELECT password_hash,email_verified,created_at,updated_at FROM "+archive+".customer_password_fields WHERE customer_id=$1", customer).Scan(&archivedHash, &verified, &created, &updated))
	if archivedHash != "historical-hash-exact" || !verified || created.UTC().Format(time.RFC3339) != "2020-01-02T03:04:05Z" || updated.UTC().Format(time.RFC3339) != "2021-02-03T04:05:06Z" {
		t.Fatal("historical values or timestamps were not preserved exactly")
	}
	var n int
	must(pool.QueryRow(ctx, "SELECT count(*) FROM "+archive+".customer_password_fields WHERE password_hash IS NULL AND NOT email_verified").Scan(&n))
	if n != 1 {
		t.Fatal("nullable historical password was changed")
	}
	service := auth.NewService(pool, 1, retiredCustomerFieldsCallChecker{})
	old, e := service.UserByToken(ctx, "existing-session")
	must(e)
	if old == nil || old.ID != customer || old.LifetimeSpendMinor != 123456 || old.RetailDiscountBPS != 500 {
		t.Fatal("existing session or customer pricing changed")
	}
	proof := strings.Repeat("a", 64)
	check, _, _, e := service.RequestCall(ctx, "+79990000001", proof)
	must(e)
	token, expires, pending, e := service.ConfirmCall(ctx, "+79990000001", check, auth.Registration{Flow: "login"}, auth.ClientMeta{CallProofHash: proof, UserAgent: "Current browser"})
	must(e)
	if token == "" || pending || !expires.After(time.Now()) {
		t.Fatal("existing customer phone login failed")
	}
	current, e := service.UserByToken(ctx, token)
	must(e)
	if current == nil || current.ID != customer || current.FullName != "Existing customer" || current.DeliveryAddress != "Saved address" {
		t.Fatal("phone login changed existing profile")
	}
	var owner int64
	must(pool.QueryRow(ctx, `SELECT customer_id FROM orders WHERE order_number='RETIRED-AUTH-GUEST'`).Scan(&owner))
	if owner != customer {
		t.Fatal("verified phone stopped linking guest orders")
	}
	check, _, _, e = service.RequestCall(ctx, "+79990000003", proof)
	must(e)
	newToken, _, pending, e := service.ConfirmCall(ctx, "+79990000003", check, auth.Registration{Flow: "register", FullName: "New phone customer", Email: "new@example.invalid", AccountType: "retail", Consent: true}, auth.ClientMeta{CallProofHash: proof, UserAgent: "New browser"})
	must(e)
	newUser, e := service.UserByToken(ctx, newToken)
	must(e)
	if pending || newUser == nil || newUser.ID == customer || newUser.Phone != "+79990000003" {
		t.Fatal("new customer registration failed after removing password fields")
	}
	must(service.Logout(ctx, newToken))
	newUser, e = service.UserByToken(ctx, newToken)
	must(e)
	if newUser != nil {
		t.Fatal("logout did not invalidate the new session")
	}
	old, e = service.UserByToken(ctx, "existing-session")
	must(e)
	if old == nil || old.ID != customer {
		t.Fatal("new customer logout removed another session")
	}
}
