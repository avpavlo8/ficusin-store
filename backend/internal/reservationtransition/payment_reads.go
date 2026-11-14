package reservationtransition

import (
	"context"
	"errors"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/avpavlo8/ficusin-store/backend/internal/money"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PaymentReader intentionally exposes no creation, capture, cancellation,
// refund or fiscal-receipt capabilities. Observations do not mutate DB state.
type PaymentReader interface {
	FetchPayment(context.Context, string) (integration.Payment, error)
}
type PaymentReads struct {
	Checked      int            `json:"checked"`
	NoProviderID int            `json:"noProviderId"`
	Complete     bool           `json:"complete"`
	Outcomes     map[string]int `json:"outcomes"`
}

func ReadUnfinishedPayments(ctx context.Context, pool *pgxpool.Pool, reader PaymentReader, limit int) (PaymentReads, error) {
	out := PaymentReads{Complete: true, Outcomes: map[string]int{}}
	if reader == nil || limit < 1 || limit > 1000 {
		return out, errors.New("read-only payment capability and limit required")
	}
	rows, err := pool.Query(ctx, `SELECT provider_payment_id,amount::text FROM payments WHERE status NOT IN('paid','cancelled','canceled','refunded') ORDER BY id LIMIT $1`, limit+1)
	if err != nil {
		return out, err
	}
	type attempt struct{ id, amount string }
	attempts := []attempt{}
	for rows.Next() {
		var a attempt
		if err = rows.Scan(&a.id, &a.amount); err != nil {
			rows.Close()
			return out, err
		}
		attempts = append(attempts, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(attempts) > limit {
		out.Complete = false
		attempts = attempts[:limit]
	}
	for _, a := range attempts {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		if a.id == "" {
			out.NoProviderID++
			out.Outcomes["unknown_creation_manual_lookup"]++
			continue
		}
		if out.Checked > 0 {
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return out, ctx.Err()
			case <-timer.C:
			}
		}
		out.Checked++
		remote, e := reader.FetchPayment(ctx, a.id)
		if e != nil {
			out.Outcomes["lookup_unavailable"]++
			continue
		}
		if remote.ID != a.id {
			out.Outcomes["identity_mismatch"]++
			continue
		}
		expected, e := money.Parse(a.amount)
		actual, e2 := money.Minor(remote.Amount)
		if e != nil || e2 != nil || expected != actual {
			out.Outcomes["amount_mismatch"]++
			continue
		}
		switch remote.Status {
		case "canceled", "cancelled":
			if remote.Paid {
				out.Outcomes["contradictory_provider_state"]++
			} else {
				out.Outcomes["provider_cancelled_manual_reconciliation"]++
			}
		case "succeeded":
			if remote.Paid {
				out.Outcomes["paid_requires_financial_review"]++
			} else {
				out.Outcomes["contradictory_provider_state"]++
			}
		case "waiting_for_capture":
			out.Outcomes["authorization_requires_decision_no_capture_sent"]++
		case "pending":
			out.Outcomes["pending_no_retry_sent"]++
		default:
			out.Outcomes["unsupported_provider_state"]++
		}
	}
	return out, nil
}
