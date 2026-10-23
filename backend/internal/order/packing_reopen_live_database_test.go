package order

import (
	"context"
	"errors"
	"fmt"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"testing"
	"time"
)

type reopenCarrier struct {
	*stage13CDEK
	status  string
	failure error
	deletes int
}

func (c *reopenCarrier) FetchOrder(context.Context, string) (integration.Shipment, error) {
	return integration.Shipment{Status: c.status}, nil
}
func (c *reopenCarrier) CancelOrder(context.Context, string) error { c.deletes++; return c.failure }

func TestPackingReopenWaitsForConfirmedCarrierRemovalOnLiveDatabase(t *testing.T) {
	ctx, pool := cdekReviewPool(t)
	for _, scenario := range []string{"confirmed", "pending", "failure", "handed_off", "already_removed", "closed"} {
		t.Run(scenario, func(t *testing.T) {
			key := fmt.Sprintf("REOPEN-CARRIER-%d", time.Now().UnixNano())
			var id, w int64
			must := func(e error) {
				t.Helper()
				if e != nil {
					t.Fatal(e)
				}
			}
			must(pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_name,phone,email,address,delivery_method,delivery_payee,subtotal,delivery_fee,total,status,cdek_uuid,cdek_track_number,cdek_create_state,cdek_status,delivery_fee_pending,cdek_checkout_requested)VALUES($1,'Reopen','+70000000000','','','cdek','shop',1000,680,1680,'new','old-uuid','old-track','registered','CREATED',1,0)RETURNING id`, key).Scan(&id))
			defer func() { _, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, id) }()
			must(pool.QueryRow(ctx, `INSERT INTO order_packing_workflows(order_id,order_revision,idempotency_key,fingerprint,boxes,tariff_code,delivery_fee,state)VALUES($1,1,$2,'fixture','[]',136,680,'reopening')RETURNING id`, id, key).Scan(&w))
			carrier := &reopenCarrier{stage13CDEK: &stage13CDEK{}, status: "CREATED"}
			switch scenario {
			case "pending":
				carrier.failure = integration.ErrCDEKCancellationPending
			case "failure":
				carrier.failure = errors.New("carrier unavailable")
			case "handed_off":
				carrier.status = "RECEIVED_AT_SHIPMENT_WAREHOUSE"
			case "already_removed":
				carrier.status = "REMOVED"
			case "closed":
				_, err := pool.Exec(ctx, `UPDATE orders SET status='shipped' WHERE id=$1`, id)
				must(err)
			}
			worker := NewShippingWorker(pool, carrier, nil, nil, quietLogger())
			e := worker.reopenPacking(ctx, w)
			safe := scenario == "confirmed" || scenario == "already_removed"
			if !safe {
				if e == nil {
					t.Fatal("unconfirmed deletion unlocked order")
				}
				return
			}
			must(e)
			var uuid, track, providerNumber, workflowState string
			var revision, pending int
			must(pool.QueryRow(ctx, `SELECT cdek_uuid,cdek_track_number,cdek_order_number,shipment_revision,delivery_fee_pending FROM orders WHERE id=$1`, id).Scan(&uuid, &track, &providerNumber, &revision, &pending))
			must(pool.QueryRow(ctx, `SELECT state FROM order_packing_workflows WHERE id=$1`, w).Scan(&workflowState))
			if uuid != "" || track != "" || revision != 2 || pending != 1 || providerNumber != key+"-r2" || workflowState != "stale" {
				t.Fatalf("bad reset uuid=%s track=%s revision=%d pending=%d provider=%s state=%s", uuid, track, revision, pending, providerNumber, workflowState)
			}
			must(worker.reopenPacking(ctx, w))
			if carrier.deletes > 1 {
				t.Fatal("retry deleted carrier again")
			}
		})
	}
}
