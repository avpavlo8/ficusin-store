package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type guestOrderCallChecker struct{ confirmed bool }

func (checker *guestOrderCallChecker) RequestCallCheck(context.Context, string) (string, string, string, error) {
	return "", "", "", nil
}
func (checker *guestOrderCallChecker) CallCheckStatus(context.Context, string) (bool, bool, error) {
	return checker.confirmed, false, nil
}

func TestVerifiedGuestOrdersOnLiveDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, flow := range []string{"login", "register"} {
		t.Run(flow, func(t *testing.T) {
			suffix := fmt.Sprintf("%d", time.Now().UnixNano())
			phone := "+79" + suffix[len(suffix)-9:]
			otherPhone := "+73" + suffix[len(suffix)-9:]
			email := "guest-" + suffix + "@example.invalid"
			checkID := "guest-check-" + suffix
			var customerID, otherID int64
			seedCustomer := func(phone, email string) int64 {
				t.Helper()
				var id int64
				if err := pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at) VALUES($1,$2,'','CI guest orders',CURRENT_TIMESTAMP) RETURNING id`, email, phone).Scan(&id); err != nil {
					t.Fatal(err)
				}
				return id
			}
			otherID = seedCustomer(otherPhone, "other-"+email)
			if flow == "login" {
				customerID = seedCustomer(phone, email)
			}
			defer func() {
				_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE order_number LIKE $1`, "CI-GUEST-"+suffix+"-%")
				_, _ = pool.Exec(ctx, `DELETE FROM call_checks WHERE check_id=$1`, checkID)
				_, _ = pool.Exec(ctx, `DELETE FROM consent_events WHERE customer_id IN (SELECT id FROM customers WHERE phone IN ($1,$2))`, phone, otherPhone)
				_, _ = pool.Exec(ctx, `DELETE FROM customers WHERE phone IN ($1,$2)`, phone, otherPhone)
			}()
			seedOrder := func(label, orderPhone string, owner any) int64 {
				t.Helper()
				var id int64
				if err := pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_id,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total) VALUES($1,$2,'CI guest',$3,$4,'pickup',0,1490,1490) RETURNING id`, "CI-GUEST-"+suffix+"-"+label, owner, orderPhone, email).Scan(&id); err != nil {
					t.Fatal(err)
				}
				return id
			}
			guestID := seedOrder("matching-phone", phone, nil)
			emailOnlyID := seedOrder("email-only", otherPhone, nil)
			ownedID := seedOrder("already-owned", phone, otherID)
			if _, err := pool.Exec(ctx, `INSERT INTO call_checks(phone,check_id,call_phone,call_phone_pretty,expires_at) VALUES($1,$2,'+79990000000','test',CURRENT_TIMESTAMP+INTERVAL '5 minutes')`, phone, checkID); err != nil {
				t.Fatal(err)
			}
			checker := &guestOrderCallChecker{}
			service := NewService(pool, 1, checker)
			registration := Registration{Flow: flow, FullName: "CI guest orders", Email: email, AccountType: "retail", Consent: true}
			if token, _, pending, err := service.ConfirmCall(ctx, phone, checkID, registration, ClientMeta{}); err != nil || !pending || token != "" {
				t.Fatalf("unverified sign in: %q, %v, %v", token, pending, err)
			}
			var unlinked bool
			if err := pool.QueryRow(ctx, `SELECT customer_id IS NULL FROM orders WHERE id=$1`, guestID).Scan(&unlinked); err != nil || !unlinked {
				t.Fatalf("unverified guest linked: %v, %v", unlinked, err)
			}
			checker.confirmed = true
			token, _, pending, err := service.ConfirmCall(ctx, phone, checkID, registration, ClientMeta{UserAgent: "local-guest-check"})
			if err != nil || pending || token == "" {
				t.Fatalf("verified sign in: %v, %v", pending, err)
			}
			user, err := service.UserByToken(ctx, token)
			if err != nil || user == nil {
				t.Fatalf("session: %#v, %v", user, err)
			}
			if flow == "login" && user.ID != customerID {
				t.Fatal("changed existing owner")
			}
			var linkedID, retainedID int64
			if err := pool.QueryRow(ctx, `SELECT customer_id FROM orders WHERE id=$1`, guestID).Scan(&linkedID); err != nil || linkedID != user.ID {
				t.Fatalf("guest not linked to verified phone: %d, %v", linkedID, err)
			}
			if err := pool.QueryRow(ctx, `SELECT customer_id FROM orders WHERE id=$1`, ownedID).Scan(&retainedID); err != nil || retainedID != otherID {
				t.Fatalf("existing owner changed: %d, %v", retainedID, err)
			}
			if err := pool.QueryRow(ctx, `SELECT customer_id IS NULL FROM orders WHERE id=$1`, emailOnlyID).Scan(&unlinked); err != nil || !unlinked {
				t.Fatalf("email granted access: %v, %v", unlinked, err)
			}
			if _, _, _, err := service.ConfirmCall(ctx, phone, checkID, registration, ClientMeta{}); !errors.Is(err, ErrInvalidCode) {
				t.Fatalf("verification reused: %v", err)
			}
		})
	}
}
