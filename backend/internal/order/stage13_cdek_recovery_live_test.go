package order

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/jackc/pgx/v5/pgxpool"
)

type stage13CDEK struct {
	mu        sync.Mutex
	creates   int
	ambiguous bool
	byNumber  map[string]integration.Shipment
	byUUID    map[string]integration.Shipment
}

func (client *stage13CDEK) Configured() bool { return true }

func (client *stage13CDEK) CreateOrder(_ context.Context, request integration.ShipmentRequest) (integration.Shipment, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.creates++
	shipment := integration.Shipment{UUID: fmt.Sprintf("stage13-cdek-%d", client.creates), Status: "CREATED"}
	client.byNumber[request.OrderNumber] = shipment
	client.byUUID[shipment.UUID] = shipment
	if client.ambiguous {
		return integration.Shipment{}, fmt.Errorf("%w: connection closed after POST", integration.ErrCDEKOutcomeUnknown)
	}
	return shipment, nil
}

func (client *stage13CDEK) FindOrderByNumber(_ context.Context, number string) (integration.Shipment, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	shipment, ok := client.byNumber[number]
	if !ok {
		return integration.Shipment{}, integration.ErrCDEKOrderNotFound
	}
	return shipment, nil
}

func (client *stage13CDEK) FetchOrder(_ context.Context, uuid string) (integration.Shipment, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	shipment, ok := client.byUUID[uuid]
	if !ok {
		return integration.Shipment{}, integration.ErrCDEKOrderNotFound
	}
	return shipment, nil
}

func (client *stage13CDEK) CancelOrder(context.Context, string) error { return nil }

type stage13ShippingSettings struct{}

func (stage13ShippingSettings) Enabled(string) bool { return true }
func (stage13ShippingSettings) Value(string) string  { return "stage13 test" }

func TestStage13CDEKCreateRecoveryDoesNotDuplicateOrPretendShipment(t *testing.T) {
	databaseURL := os.Getenv("CRM_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CRM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := &stage13CDEK{byNumber: map[string]integration.Shipment{}, byUUID: map[string]integration.Shipment{}}
	worker := NewShippingWorker(pool, client, stage13ShippingSettings{}, nil, logger)

	registeredOrder, registeredOffer := seedStage13CDEKOffer(t, ctx, pool, "registered")
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, registeredOrder) }()
	worker.createOfferShipments(ctx)

	var status, createState, uuid string
	if err := pool.QueryRow(ctx, `SELECT status,cdek_create_state,cdek_uuid FROM shipment_offers WHERE id=$1`, registeredOffer).Scan(&status, &createState, &uuid); err != nil {
		t.Fatal(err)
	}
	if status != "shipping" || createState != "registered" || uuid == "" {
		t.Fatalf("created application became a shipped parcel: status=%s state=%s uuid=%s", status, createState, uuid)
	}

	client.mu.Lock()
	client.byUUID[uuid] = integration.Shipment{
		UUID: uuid, TrackNumber: "TRACK-13", Status: "RECEIVED_AT_SHIPMENT_WAREHOUSE", StatusReason: "Accepted",
	}
	client.mu.Unlock()
	worker.refreshOfferStatuses(ctx)
	if err := pool.QueryRow(ctx, `SELECT status,cdek_track_number FROM shipment_offers WHERE id=$1`, registeredOffer).Scan(&status, &createState); err != nil {
		t.Fatal(err)
	}
	if status != "shipped" || createState != "TRACK-13" {
		t.Fatalf("physical acceptance was not reflected: status=%s track=%s", status, createState)
	}

	client.ambiguous = true
	unknownOrder, unknownOffer := seedStage13CDEKOffer(t, ctx, pool, "unknown")
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, unknownOrder) }()
	worker.createOfferShipments(ctx)
	if err := pool.QueryRow(ctx, `SELECT status,cdek_create_state,cdek_uuid FROM shipment_offers WHERE id=$1`, unknownOffer).Scan(&status, &createState, &uuid); err != nil {
		t.Fatal(err)
	}
	if status != "shipping" || createState != "unknown" || uuid != "" {
		t.Fatalf("ambiguous POST was treated as a retryable refusal: status=%s state=%s uuid=%s", status, createState, uuid)
	}
	if _, err := pool.Exec(ctx, `UPDATE shipment_offers SET cdek_next_attempt_at=CURRENT_TIMESTAMP WHERE id=$1`, unknownOffer); err != nil {
		t.Fatal(err)
	}
	worker.reconcileUnknownShipments(ctx)
	if err := pool.QueryRow(ctx, `SELECT status,cdek_create_state,cdek_uuid FROM shipment_offers WHERE id=$1`, unknownOffer).Scan(&status, &createState, &uuid); err != nil {
		t.Fatal(err)
	}
	if status != "shipping" || createState != "registered" || uuid == "" {
		t.Fatalf("read-only reconciliation failed: status=%s state=%s uuid=%s", status, createState, uuid)
	}
	client.mu.Lock()
	creates := client.creates
	client.mu.Unlock()
	if creates != 2 {
		t.Fatalf("ambiguous shipment was created more than once: creates=%d", creates)
	}
}

func TestStage13CDEKUnknownMovesToManualReviewWithoutAnotherPOST(t *testing.T) {
	state, next := cdekReconciliationFailure(integration.ErrCDEKOrderNotFound, 4)
	if state != "manual_review" || next != nil {
		t.Fatalf("state=%s next=%v", state, next)
	}
	state, next = cdekReconciliationFailure(fmt.Errorf("%w: timeout", integration.ErrCDEKOutcomeUnknown), 10)
	if state != "unknown" || next == nil {
		t.Fatalf("transient lookup must remain recoverable: state=%s next=%v", state, next)
	}
	if !errors.Is(fmt.Errorf("wrapped: %w", integration.ErrCDEKOutcomeUnknown), integration.ErrCDEKOutcomeUnknown) {
		t.Fatal("unknown CDEK outcome must remain classifiable through wrapping")
	}
}

func seedStage13CDEKOffer(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label string) (int64, int64) {
	t.Helper()
	var customerID, productID, variantID, orderID, itemID, offerID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM customers WHERE email='crm-owner@example.invalid'`).Scan(&customerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT p.id,pv.id FROM products p JOIN product_variants pv ON pv.product_id=p.id WHERE p.slug='crm-stage07-a'`).Scan(&productID, &variantID); err != nil {
		t.Fatal(err)
	}
	number := fmt.Sprintf("CRM-S13-CDEK-%s-%d", label, time.Now().UnixNano())
	if err := pool.QueryRow(ctx, `
		INSERT INTO orders(order_number,customer_id,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,
			payment_method,payment_status,status,has_preorder,cdek_city_code,cdek_office_code)
		VALUES($1,$2,'Stage 13 CDEK','+70000000000','stage13-cdek@example.invalid','cdek',777,2290,3067,
			'online','paid','confirmed',1,44,'PVZ-13') RETURNING id`, number, customerID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO order_items(order_id,product_id,variant_id,sku,product_name,variant_label,variant_snapshot,unit_price,quantity,is_preorder,reserved_qty)
		SELECT $1,$2,$3,sku,'Stage 13 CDEK plant',label,'{}',2290,1,1,0 FROM product_variants WHERE id=$3 RETURNING id`, orderID, productID, variantID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO shipment_offers(order_id,public_token,order_revision,status,delivery_method,delivery_fee,subtotal,total,
			cdek_tariff_code,cdek_tariff_name,created_by)
		VALUES($1,$2,1,'paid','cdek',777,2290,3067,136,'Stage 13',$3) RETURNING id`, orderID, fmt.Sprintf("stage13-cdek-%s-%d", label, time.Now().UnixNano()), customerID).Scan(&offerID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO shipment_offer_items(shipment_offer_id,order_item_id,variant_id,sku,product_name,unit_price,original_unit_price,quantity)
		SELECT $1,$2,$3,sku,'Stage 13 CDEK plant',2290,2290,1 FROM product_variants WHERE id=$3`, offerID, itemID, variantID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO shipment_offer_boxes(shipment_offer_id,box_no,length_cm,width_cm,height_cm,weight_grams,contents)
		VALUES($1,1,55,25,30,1800,$2::jsonb)`, offerID, fmt.Sprintf(`[{"orderItemId":%d,"quantity":1}]`, itemID)); err != nil {
		t.Fatal(err)
	}
	return orderID, offerID
}
