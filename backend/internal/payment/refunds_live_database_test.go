package payment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func (p *livePaymentProvider) PrepareRefund(id string, amount float64, key string, _ integration.PaymentRequest) (integration.RefundRequest, error) {
	return integration.RefundRequest{PaymentID: id, Amount: amount, IdempotenceKey: key}, nil
}
func (p *livePaymentProvider) CreateRefund(ctx context.Context, r integration.RefundRequest) (integration.Refund, error) {
	if err := p.Refund(ctx, r.PaymentID, r.Amount, r.IdempotenceKey); err != nil {
		return integration.Refund{}, err
	}
	return integration.Refund{ID: "ci-refund-" + r.IdempotenceKey, PaymentID: r.PaymentID, Amount: r.Amount, Status: "succeeded"}, nil
}
func (*livePaymentProvider) FetchRefund(context.Context, string) (integration.Refund, error) {
	return integration.Refund{}, errors.New("unexpected fetch")
}

type pendingRefundProvider struct {
	livePaymentProvider
	result  integration.Refund
	timeout bool
	keys    []string
	fetches int
}

func (p *pendingRefundProvider) CreateRefund(_ context.Context, r integration.RefundRequest) (integration.Refund, error) {
	p.keys = append(p.keys, r.IdempotenceKey)
	p.result = integration.Refund{ID: "pending-" + r.IdempotenceKey, PaymentID: r.PaymentID, Amount: r.Amount, Status: "pending"}
	if p.timeout {
		return integration.Refund{}, integration.ErrPaymentOutcomeUnknown
	}
	return p.result, nil
}
func (p *pendingRefundProvider) FetchRefund(_ context.Context, _ string) (integration.Refund, error) {
	p.fetches++
	return p.result, nil
}
func TestRefundRecoveryOnLiveDatabase(t *testing.T) {
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
	var orderID int64
	number := fmt.Sprintf("AUDIT-REFUND-%d", time.Now().UnixNano())
	err = pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status) VALUES($1,'fixture','+70000000000','fixture@example.invalid','pickup',0,100,100,'online','paid','confirmed') RETURNING id`, number).Scan(&orderID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM payment_refunds WHERE order_id=$1`, orderID)
		_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, orderID)
	}()
	_, err = pool.Exec(ctx, `INSERT INTO payments(order_id,idempotence_key,amount,status,provider_payment_id) VALUES($1,$2,100,'paid',$2)`, orderID, number)
	if err != nil {
		t.Fatal(err)
	}
	provider := &pendingRefundProvider{timeout: true}
	service := NewService(pool, provider, "https://example.invalid", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err = service.RefundAmount(ctx, orderID, 40, "fixture"); err == nil {
		t.Fatal("timeout disappeared")
	}
	provider.timeout = false
	balance, err := service.RefundAmount(ctx, orderID, 40, "retry")
	if err != nil {
		t.Fatal(err)
	}
	if balance.Refunded != 0 || len(provider.keys) != 2 || provider.keys[0] != provider.keys[1] {
		t.Fatalf("pending settled or key changed: %+v %+v", balance, provider.keys)
	}
	provider.result.Status = "succeeded"
	if err = service.reconcileRefunds(ctx); err != nil {
		t.Fatal(err)
	}
	balance, err = service.BalanceForOrder(ctx, orderID)
	if err != nil || balance.Refunded != 40 || len(provider.keys) != 2 || provider.fetches != 1 {
		t.Fatalf("not reconciled %+v %v", balance, err)
	}
	if len(balance.Refunds) != 1 || balance.Refunds[0].ReceiptStatus != "legacy_unverified" {
		t.Fatalf("receipt task missing: %+v", balance.Refunds)
	}
	_, err = pool.Exec(ctx, `UPDATE payment_refunds SET status='pending',created_at=CURRENT_TIMESTAMP-INTERVAL '2 days' WHERE order_id=$1`, orderID)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.reconcileRefunds(ctx); err != nil {
		t.Fatal(err)
	}
	if provider.fetches != 2 || len(provider.keys) != 2 {
		t.Fatal("old refund posted again")
	}
	_, err = pool.Exec(ctx, `UPDATE payment_refunds SET status='pending',provider_refund_id='' WHERE order_id=$1`, orderID)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.reconcileRefunds(ctx); err != nil {
		t.Fatal(err)
	}
	balance, err = service.BalanceForOrder(ctx, orderID)
	if err != nil || !balance.Refunds[0].NeedsReview || len(provider.keys) != 2 {
		t.Fatalf("unsafe recovery %+v %v", balance, err)
	}
}
func TestPaymentTokenOwnershipOnLiveDatabase(t *testing.T) {
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
	token := strings.Repeat("a", 64)
	sum := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(sum[:])
	number := fmt.Sprintf("ACCESS-%d", time.Now().UnixNano())
	var id int64
	err = pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,guest_payment_token_hash) VALUES($1,'fixture','+70000000000','fixture@example.invalid','pickup',0,100,100,$2) RETURNING id`, number, hash).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, id) }()
	service := NewService(pool, &livePaymentProvider{}, "https://example.invalid", slog.Default())
	for _, tc := range []struct {
		token string
		want  bool
	}{{"", false}, {strings.Repeat("b", 64), false}, {token, true}} {
		ok, err := service.AuthorizeOrderPayment(ctx, number, tc.token, 0)
		if err != nil || ok != tc.want {
			t.Fatalf("got %v %v", ok, err)
		}
	}
}

func TestConcurrentRefundsOnLiveDatabase(t *testing.T) {
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
	key := fmt.Sprintf("CONCURRENT-REFUND-%d", time.Now().UnixNano())
	var orderID, actorID int64
	if err = pool.QueryRow(ctx, `INSERT INTO customers(email,phone,full_name,consent_at) VALUES($1,$2,'Refund actor',CURRENT_TIMESTAMP) RETURNING id`, key+"@example.invalid", key).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(context.Background(), `DELETE FROM customers WHERE id=$1`, actorID) }()
	if err = pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status) VALUES($1,'fixture','+70000000000','fixture@example.invalid','pickup',0,100,100,'online','paid','confirmed') RETURNING id`, key).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM payment_refunds WHERE order_id=$1`, orderID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM orders WHERE id=$1`, orderID)
	}()
	if _, err = pool.Exec(ctx, `INSERT INTO payments(order_id,idempotence_key,amount,status,provider_payment_id) VALUES($1,$2,100,'paid',$2)`, orderID, key); err != nil {
		t.Fatal(err)
	}
	provider := &pendingRefundProvider{}
	service := NewService(pool, provider, "https://example.invalid", slog.Default())
	ctx = WithRefundRequest(WithRefundActor(ctx, actorID), key)
	errs := make(chan error, 6)
	for range 6 {
		go func() { _, err := service.RefundAmount(ctx, orderID, 100, "concurrent fixture"); errs <- err }()
	}
	for range 6 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if len(provider.keys) != 1 {
		t.Fatalf("created %d refunds for one concurrent request", len(provider.keys))
	}
	balance, err := service.BalanceForOrder(ctx, orderID)
	if err != nil || len(balance.Refunds) != 1 || balance.Refunds[0].ActorID == nil || *balance.Refunds[0].ActorID != actorID {
		t.Fatalf("actor lost: %+v %v", balance, err)
	}
	provider.result.Status = "succeeded"
	if err = service.reconcileRefunds(ctx); err != nil {
		t.Fatal(err)
	}
	balance, err = service.BalanceForOrder(ctx, orderID)
	if err != nil || balance.Refunded != 100 {
		t.Fatalf("wrong refunded balance %+v %v", balance, err)
	}
	if _, err = service.RefundAmount(ctx, orderID, 100, "concurrent fixture"); err != nil {
		t.Fatal(err)
	}
	if len(provider.keys) != 1 {
		t.Fatal("same request sent another refund after success")
	}
	if _, err = service.RefundAmount(ctx, orderID, 10, "concurrent fixture"); err == nil {
		t.Fatal("request ID reused with a different amount")
	}

}
