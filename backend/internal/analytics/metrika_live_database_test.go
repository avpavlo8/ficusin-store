package analytics

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMetrikaOutboxOnLiveDatabase(t *testing.T) {
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
	s := NewStore(pool)
	w := s.WithMetrika("test-secret", "https://ficusin.ru", func() string { return "111907858" })
	var ids []int64
	defer func() {
		for _, id := range ids {
			_, _ = pool.Exec(ctx, `DELETE FROM payment_refunds WHERE order_id=$1`, id)
			_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, id)
		}
	}()
	for _, tc := range []struct {
		name, status, paystatus, payee string
		paid                           int
		privacy, server, refund        bool
		want                           int
	}{
		{"paid", "confirmed", "paid", "shop", 1590, true, true, false, 1},
		{"carrier", "confirmed", "paid", "carrier", 1490, true, true, false, 1},
		{"partial label paid", "confirmed", "paid", "shop", 1000, true, true, false, 0},
		{"hold", "confirmed", "pending", "shop", 0, true, true, false, 0},
		{"cancelled", "cancelled", "paid", "shop", 1590, true, true, false, 0},
		{"privacy", "confirmed", "paid", "shop", 1590, false, true, false, 0},
		{"legacy browser", "confirmed", "paid", "shop", 1590, true, false, false, 0},
		{"refund", "confirmed", "paid", "shop", 1590, true, true, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var id, paymentID int64
			number := fmt.Sprintf("CI-MP-%d", time.Now().UnixNano())
			err := pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_fee,delivery_payee,subtotal,total,payment_method,payment_status,status,attribution)
 VALUES($1,'CI Metrika','+79990000001','test@example.invalid','pickup',100,$2,1490,1590,'online',$3,$4,jsonb_build_object('analyticsAllowed',$5::boolean,'metrikaServer',$6::boolean,'metrikaCounterId','111907858','metrikaClientId','123456')) RETURNING id`, number, tc.payee, tc.paystatus, tc.status, tc.privacy, tc.server).Scan(&id)
			if err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id)
			if tc.paid > 0 {
				if err = pool.QueryRow(ctx, `INSERT INTO payments(order_id,idempotence_key,amount,status,paid_at) VALUES($1,$2,$3,'paid',CURRENT_TIMESTAMP) RETURNING id`, id, number, tc.paid).Scan(&paymentID); err != nil {
					t.Fatal(err)
				}
			}
			if tc.refund {
				if _, err = pool.Exec(ctx, `INSERT INTO payment_refunds(order_id,payment_id,idempotence_key,amount) VALUES($1,$2,$3,100)`, id, paymentID, number); err != nil {
					t.Fatal(err)
				}
			}
			for range 2 {
				if err = w.enqueue(ctx); err != nil {
					t.Fatal(err)
				}
			}
			var count int
			if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM metrika_paid_outbox WHERE order_id=$1`, id).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != tc.want {
				t.Fatalf("got %d want %d", count, tc.want)
			}
			if count == 1 {
				var amount string
				if err = pool.QueryRow(ctx, `SELECT amount::text FROM metrika_paid_outbox WHERE order_id=$1`, id).Scan(&amount); err != nil {
					t.Fatal(err)
				}
				want := "1590.00"
				if tc.payee == "carrier" {
					want = "1490.00"
				}
				if amount != want {
					t.Fatalf("amount=%s want %s", amount, want)
				}
			}
		})
	}
	// Two claimed purchases cannot be claimed again by another worker/poll.
	for range 2 {
		d, err := w.claim(ctx)
		if err != nil || d == nil {
			t.Fatalf("claim=%+v err=%v", d, err)
		}
	}
	if d, err := w.claim(ctx); err != nil || d != nil {
		t.Fatalf("duplicate claim=%+v err=%v", d, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE metrika_paid_outbox SET claimed_at=CURRENT_TIMESTAMP-INTERVAL '3 minutes' WHERE order_id=ANY($1) AND status='sending'`, ids); err != nil {
		t.Fatal(err)
	}
	if d, err := w.claim(ctx); err != nil || d != nil {
		t.Fatalf("interrupted claim replayed: %+v %v", d, err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM metrika_paid_outbox WHERE order_id=ANY($1) AND status='uncertain'`, ids).Scan(&count); err != nil || count != 2 {
		t.Fatalf("uncertain=%d %v", count, err)
	}
}

func TestMetrikaPaymentWithoutBuyerReturnOnLiveDatabase(t *testing.T) {
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
	for _, tc := range []struct{ name, body, status string }{{"accepted", "<!-- OK -->", "sent"}, {"ambiguous", "unexpected body", "uncertain"}} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				payload, _ := io.ReadAll(r.Body)
				hits := strings.Split(string(payload), "\n")
				if len(hits) != 3 {
					t.Errorf("expected pageview, goal and SKU purchase, got %d hits", len(hits))
				} else {
					purchase, _ := url.ParseQuery(hits[2])
					if purchase.Get("pr1id") == "" || purchase.Get("pr1pr") != "100.00" || purchase.Get("pr1qt") != "1" {
						t.Error("missing canonical product snapshot", purchase)
					}
				}
				_, _ = res.Write([]byte(tc.body))
			}))
			defer server.Close()
			s := NewStore(pool)
			w := s.WithMetrika("test", "https://ficusin.ru", func() string { return "111907858" })
			w.endpoint = server.URL
			var id int64
			number := fmt.Sprintf("CI-MP-CYCLE-%d", time.Now().UnixNano())
			err = pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status) VALUES($1,'CI MP','+79990000001','test@example.invalid','pickup',0,100,100,'online','paid','confirmed') RETURNING id`, number).Scan(&id)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				_, _ = pool.Exec(ctx, `DELETE FROM analytics_events WHERE order_number=$1`, number)
				_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, id)
			}()
			if _, err = pool.Exec(ctx, `INSERT INTO order_items(order_id,product_id,variant_id,sku,product_name,unit_price,quantity) SELECT $1,p.id,v.id,v.sku,p.name,100,1 FROM product_variants v JOIN products p ON p.id=v.product_id ORDER BY v.id LIMIT 1`, id); err != nil {
				t.Fatal(err)
			}
			if err = s.RecordOrder(ctx, number, 100, Attribution{AnalyticsAllowed: true, MetrikaClientID: "123456"}); err != nil {
				t.Fatal(err)
			}
			if _, err = pool.Exec(ctx, `INSERT INTO payments(order_id,idempotence_key,amount,status,paid_at) VALUES($1,$2,100,'paid',CURRENT_TIMESTAMP)`, id, number); err != nil {
				t.Fatal(err)
			}
			// Only background worker cycles run; no buyer page/API read is involved.
			for range 2 {
				if err = w.cycle(ctx); err != nil {
					t.Fatal(err)
				}
			}
			var status string
			if err = pool.QueryRow(ctx, `SELECT status FROM metrika_paid_outbox WHERE order_id=$1`, id).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if status != tc.status || calls.Load() != 1 {
				t.Fatalf("status=%s calls=%d", status, calls.Load())
			}
		})
	}
}
