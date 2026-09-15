package admin

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/jackc/pgx/v5/pgxpool"
)

type stage13ShipmentQuoter struct {
	boxes []integration.Parcel
}

func (quoter *stage13ShipmentQuoter) Configured() bool { return true }

func (quoter *stage13ShipmentQuoter) CalculatePVZPackages(_ context.Context, cityCode int, boxes []integration.Parcel) ([]integration.CDEKQuote, error) {
	if cityCode != 44 {
		return nil, fmt.Errorf("unexpected city %d", cityCode)
	}
	quoter.boxes = append([]integration.Parcel(nil), boxes...)
	return []integration.CDEKQuote{{TariffCode: 136, TariffName: "Посылка склад-склад", Price: 777, DaysMin: 2, DaysMax: 4}}, nil
}

func TestStage13ShipmentOfferUsesServerCDEKQuoteForExactBoxes(t *testing.T) {
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

	var customerID, productID, variantID, orderID, itemID int64
	if err = pool.QueryRow(ctx, `SELECT id FROM customers WHERE email='crm-owner@example.invalid'`).Scan(&customerID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT p.id,pv.id FROM products p JOIN product_variants pv ON pv.product_id=p.id WHERE p.slug='crm-stage07-a'`).Scan(&productID, &variantID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO orders(order_number,customer_id,customer_name,phone,email,delivery_method,delivery_fee,subtotal,total,payment_method,payment_status,status,has_preorder,cdek_city_code) VALUES($1,$2,'Stage 13 CDEK','+70000000000','stage13-cdek@example.invalid','cdek',1,2290,2291,'online','pending','confirmed',1,44) RETURNING id`, fmt.Sprintf("CRM-S13-CDEK-%d", time.Now().UnixNano()), customerID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, orderID) }()
	if err = pool.QueryRow(ctx, `INSERT INTO order_items(order_id,product_id,variant_id,sku,product_name,variant_label,variant_snapshot,unit_price,quantity,is_preorder,reserved_qty) SELECT $1,$2,$3,sku,'Stage 13 CDEK plant',label,'{}',2290,1,1,0 FROM product_variants WHERE id=$3 RETURNING id`, orderID, productID, variantID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}

	quoter := &stage13ShipmentQuoter{}
	repository := NewPostgresRepository(pool).WithShipmentQuotes(quoter)
	input := ShipmentOfferInput{
		Items: []ShipmentOfferLineInput{{OrderItemID: itemID, Quantity: 1}},
		Boxes: []ShipmentBoxInput{{LengthCM: 55, WidthCM: 25, HeightCM: 30, WeightGrams: 1800, Contents: []ShipmentBoxContent{{OrderItemID: itemID, Quantity: 1}}}},
		DeliveryFee: 1,
		CDEKTariffCode: func() *int { value := 136; return &value }(),
	}
	preview, err := repository.QuoteShipmentOffer(ctx, Actor{CustomerID: customerID, Role: RoleManager}, orderID, input)
	if err != nil || len(preview.Quotes) != 1 || preview.Quotes[0].Price != 777 {
		t.Fatalf("unexpected quote: %+v err=%v", preview, err)
	}
	offer, err := repository.CreateShipmentOffer(ctx, Actor{CustomerID: customerID, Role: RoleManager}, orderID, input)
	if err != nil {
		t.Fatal(err)
	}
	if offer.DeliveryFee != 777 || offer.CDEKTariffCode == nil || *offer.CDEKTariffCode != 136 || offer.CDEKTariffName != "Посылка склад-склад" {
		t.Fatalf("client fee was trusted or tariff was not saved: %+v", offer)
	}
	if len(quoter.boxes) != 1 || quoter.boxes[0] != (integration.Parcel{LengthCM: 55, WidthCM: 25, HeightCM: 30, WeightGrams: 1800}) {
		t.Fatalf("CDEK received wrong boxes: %+v", quoter.boxes)
	}
}
