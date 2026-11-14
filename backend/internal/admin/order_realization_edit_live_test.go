package admin

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/ordersale"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type orderEditSQLTrace struct{ t *testing.T }
type orderEditSQLTraceKey struct{}

func (tr orderEditSQLTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, orderEditSQLTraceKey{}, data.SQL)
}
func (tr orderEditSQLTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if data.Err != nil {
		tr.t.Logf("failed SQL (arguments omitted): %s", ctx.Value(orderEditSQLTraceKey{}))
	}
}

// Real admin EditOrder + durable realization, with no external clients. Every
// table belongs to this test's private schema in a dedicated loopback DB.
func TestOrderRealizationEditsPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.Path != "/ficusin_order_sale_test" || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost") {
		t.Fatal("requires loopback dedicated ficusin_order_sale_test")
	}
	ctx := context.Background()
	control, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close(ctx)
	schema := fmt.Sprintf("sale_admin_edit_%d", time.Now().UnixNano())
	ident := pgx.Identifier{schema}.Sanitize()
	if _, err = control.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		t.Fatal(err)
	}
	defer control.Exec(ctx, "DROP SCHEMA "+ident+" CASCADE")
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.ConnConfig.Tracer = orderEditSQLTrace{t}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE customers(id BIGINT PRIMARY KEY,retail_discount_bps INTEGER);INSERT INTO customers VALUES(1,0);
 CREATE TABLE warehouses(id BIGINT PRIMARY KEY,name TEXT,is_active INTEGER,saby_id TEXT);INSERT INTO warehouses VALUES(1,'Test',1,'saby-ryazan-main');
 CREATE TABLE products(id BIGINT PRIMARY KEY,product_code BIGINT,name TEXT,status TEXT,saby_fields TEXT[]);INSERT INTO products VALUES(1,1,'Original plant','published',ARRAY['stock']),(2,2,'New plant','published',ARRAY['stock']);
 CREATE TABLE product_variants(id BIGINT PRIMARY KEY,product_id BIGINT,saby_id TEXT,sku TEXT,label TEXT,base_price_minor BIGINT,is_active INTEGER,archived_at TIMESTAMPTZ);INSERT INTO product_variants VALUES(1,1,'3980','13','D13',99900,1,NULL),(2,2,'4000','24','D24',20000,1,NULL);
 CREATE TABLE product_external_ids(variant_id BIGINT,provider TEXT,id_type TEXT,status TEXT,external_id TEXT);
 CREATE TABLE saby_nomenclature(saby_id TEXT PRIMARY KEY,missing_since TIMESTAMPTZ);INSERT INTO saby_nomenclature VALUES('3980',NULL),('4000',NULL);
 CREATE TABLE inventory(id BIGSERIAL PRIMARY KEY,warehouse_id BIGINT,variant_id BIGINT,available_qty INTEGER,reserved_qty INTEGER,synced_at TIMESTAMPTZ);INSERT INTO inventory(warehouse_id,variant_id,available_qty,reserved_qty,synced_at)VALUES(1,1,100,0,now()),(1,2,100,0,now());
 CREATE FUNCTION variant_numeric_attribute(BIGINT,TEXT)RETURNS NUMERIC LANGUAGE SQL IMMUTABLE AS 'SELECT 13::NUMERIC';
 CREATE FUNCTION shipping_package_dimension(BIGINT,TEXT)RETURNS NUMERIC LANGUAGE SQL IMMUTABLE AS 'SELECT 20::NUMERIC';
 CREATE TABLE orders(id BIGSERIAL PRIMARY KEY,order_number TEXT UNIQUE,customer_id BIGINT,customer_name TEXT DEFAULT 'Synthetic',phone TEXT DEFAULT '',email TEXT DEFAULT '',address TEXT DEFAULT '',comment TEXT DEFAULT '',
 delivery_method TEXT DEFAULT 'pickup',delivery_fee NUMERIC DEFAULT 0,delivery_fee_pending INTEGER DEFAULT 0,delivery_repack_requested INTEGER DEFAULT 0,cdek_city_code INTEGER,cdek_office_code TEXT,cdek_tariff_code INTEGER,subtotal NUMERIC(12,2) DEFAULT 200,total NUMERIC(12,2) DEFAULT 200,
 payment_method TEXT DEFAULT 'online',payment_status TEXT DEFAULT 'pending',has_preorder INTEGER DEFAULT 0,status TEXT DEFAULT 'new',delivery_payee TEXT DEFAULT 'shop',cdek_status TEXT DEFAULT '',cdek_create_state TEXT DEFAULT '',cdek_cancel_state TEXT DEFAULT '',cdek_uuid TEXT DEFAULT '',cdek_track_number TEXT DEFAULT '',created_at TIMESTAMPTZ DEFAULT now(),stock_released_at TIMESTAMPTZ,
 manager_discount_type TEXT DEFAULT 'fixed',manager_discount_value NUMERIC DEFAULT 0,manager_discount_amount NUMERIC DEFAULT 0,shipment_revision INTEGER DEFAULT 1);
 CREATE TABLE order_items(id BIGSERIAL PRIMARY KEY,order_id BIGINT REFERENCES orders(id),product_id BIGINT,variant_id BIGINT,sku TEXT,product_name TEXT,variant_label TEXT,variant_snapshot JSONB,unit_price NUMERIC(12,2),quantity INTEGER,is_preorder INTEGER DEFAULT 0,reserved_qty INTEGER DEFAULT 0);
 CREATE TABLE shipment_offers(id BIGINT PRIMARY KEY,order_id BIGINT,status TEXT,delivery_method TEXT,cdek_status TEXT DEFAULT '',created_at TIMESTAMPTZ DEFAULT now(),checkout_locked_until TIMESTAMPTZ,updated_at TIMESTAMPTZ,cdek_uuid TEXT DEFAULT '',cdek_create_state TEXT DEFAULT '',cdek_cancel_state TEXT DEFAULT '');
 CREATE TABLE shipment_offer_items(order_item_id BIGINT,shipment_offer_id BIGINT,variant_id BIGINT,product_name TEXT,sku TEXT,quantity INTEGER,unit_price NUMERIC(12,2));
 CREATE TABLE procurement_requests(kind TEXT,saby_id TEXT,requested_name TEXT,quantity INTEGER,customer_order_id BIGINT,status TEXT,notes TEXT,updated_at TIMESTAMPTZ);
 CREATE TABLE order_packing_workflows(id BIGINT,order_id BIGINT,state TEXT,order_revision INTEGER,boxes JSONB);
 CREATE TABLE payments(id BIGINT,order_id BIGINT,status TEXT,provider_payment_id TEXT DEFAULT '',request_payload JSONB);
 CREATE TABLE stock_movements(order_id BIGINT,variant_id BIGINT,saby_id TEXT,kind TEXT,quantity INTEGER,status TEXT);
 CREATE TABLE admin_audit_log(actor_customer_id BIGINT,actor_role TEXT,action TEXT,entity_type TEXT,entity_id TEXT,before_data JSONB,after_data JSONB);
 CREATE TABLE product_media(id BIGINT,product_id BIGINT,variant_id BIGINT,object_key TEXT,is_primary INTEGER,sort_order INTEGER);
 CREATE TABLE media_mirror(source_url TEXT,card_url TEXT,large_url TEXT);
 CREATE TABLE catalog_supplementary_media(product_id BIGINT,object_key TEXT);`)
	paths, err := filepath.Glob("../../../timeweb/migrations/*order_sale*.sql")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		exec(string(raw))
	}
	if err = ordersale.RefreshDispatchControl(ctx, pool, ordersale.DispatchControl{Mode: "mock", Enabled: true, SiteWarehouseID: 1}); err != nil {
		t.Fatal(err)
	}
	repository := &PostgresRepository{pool: pool}
	actor := Actor{CustomerID: 1, Role: "owner"}
	create := func(t *testing.T) (int64, int64, int64) {
		t.Helper()
		var id, itemID int64
		if err = pool.QueryRow(ctx, `INSERT INTO orders(order_number)VALUES('EDIT-'||nextval('orders_id_seq')::TEXT) RETURNING id`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if err = pool.QueryRow(ctx, `INSERT INTO order_items(order_id,product_id,variant_id,sku,product_name,variant_label,variant_snapshot,unit_price,quantity)VALUES($1,1,1,'13','Agreed original name','D13','{"packageLengthCm":35}',100,2)RETURNING id`, id).Scan(&itemID); err != nil {
			t.Fatal(err)
		}
		tx, e := pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		event, e := ordersale.EnqueueCreated(ctx, tx, id)
		if e != nil {
			tx.Rollback(ctx)
			t.Fatal(e)
		}
		if e = tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
		return id, itemID, event.IntentID
	}
	drain := func(t *testing.T) {
		t.Helper()
		for range 100 {
			did, e := ordersale.PublicationStep(ctx, pool, ordersale.MockProvider{}, "mock")
			if e != nil {
				t.Fatal(e)
			}
			if !did {
				return
			}
		}
		t.Fatal("mock realization did not settle")
	}
	edit := func(t *testing.T, id int64, lines []OrderEditLine, discount *OrderDiscount) Order {
		t.Helper()
		input := OrderEdit{Discount: discount}
		if lines != nil {
			input.Items = &lines
		}
		value, e := repository.EditOrder(ctx, actor, id, input)
		if e != nil {
			t.Fatal(e)
		}
		if value.ID != id {
			t.Fatal("edit created a different order", value.ID, id)
		}
		return value
	}
	for _, phase := range []string{"new", "pending", "posted"} {
		t.Run(phase+" composition revision preserves document and item identity", func(t *testing.T) {
			id, itemID, intent := create(t)
			if phase == "pending" {
				for range 2 {
					if _, e := ordersale.PublicationStep(ctx, pool, ordersale.MockProvider{}, "mock"); e != nil {
						t.Fatal(e)
					}
				}
			} else if phase == "posted" {
				drain(t)
			}
			var requestHash, beforeDoc string
			if e := pool.QueryRow(ctx, `SELECT request_hash,document_id FROM order_sale_publications WHERE intent_id=$1`, intent).Scan(&requestHash, &beforeDoc); e != nil {
				t.Fatal(e)
			}
			value := edit(t, id, []OrderEditLine{{SKU: "13", Quantity: 3}, {SKU: "24", Quantity: 1}}, nil)
			if len(value.Items) != 2 {
				t.Fatal("edited composition missing", value.Items)
			}
			if _, e := repository.UpdateOrderStatus(ctx, actor, id, "assembling", ""); !errors.Is(e, ErrInvalidInput) {
				t.Fatal("assembly must wait for the changed realization", e)
			}
			var preservedID, quantity int64
			var price, name string
			var packing int
			if e := pool.QueryRow(ctx, `SELECT id,quantity,unit_price::TEXT,product_name,(variant_snapshot->>'packageLengthCm')::INTEGER FROM order_items WHERE order_id=$1 AND sku='13'`, id).Scan(&preservedID, &quantity, &price, &name, &packing); e != nil || preservedID != itemID || quantity != 3 || price != "100.00" || name != "Agreed original name" || packing != 35 {
				t.Fatal("retained item identity/terms changed", preservedID, quantity, price, name, packing, e)
			}
			drain(t)
			var afterDoc, afterHash, status, message string
			if e := pool.QueryRow(ctx, `SELECT document_id,request_hash,status,message FROM order_sale_publications WHERE intent_id=$1`, intent).Scan(&afterDoc, &afterHash, &status, &message); e != nil || afterDoc != fmt.Sprintf("STG-SALE-%d", intent) || afterHash != requestHash || status != "posted" || (beforeDoc != "" && beforeDoc != afterDoc) {
				t.Fatal("revision replaced identity or original immutable receipt", beforeDoc, afterDoc, afterHash, requestHash, status, message, e)
			}
			edit(t, id, []OrderEditLine{{SKU: "24", Quantity: 2}}, nil)
			drain(t)
			if e := pool.QueryRow(ctx, `SELECT status,message FROM order_sale_publications WHERE intent_id=$1`, intent).Scan(&status, &message); e != nil || status != "posted" {
				t.Fatal("second edit did not settle on the existing document", status, message, e)
			}
			var live, history int
			if e := pool.QueryRow(ctx, `SELECT COUNT(*) FROM order_items WHERE id=$1`, itemID).Scan(&live); e != nil {
				t.Fatal(e)
			}
			if e := pool.QueryRow(ctx, `SELECT COUNT(*) FROM order_sale_intent_lines WHERE order_item_id=$1`, itemID).Scan(&history); e != nil || live != 0 || history != 1 {
				t.Fatal("removed live row lost frozen audit identity", live, history, e)
			}
			var auditQty, auditPacking int
			var auditPrice, auditName string
			if e := pool.QueryRow(ctx, `SELECT (row->>'quantity')::INTEGER,row->>'unit_price',row->>'product_name',(row->'variant_snapshot'->>'packageLengthCm')::INTEGER FROM order_sale_revisions r CROSS JOIN LATERAL jsonb_array_elements(r.order_items_snapshot) AS row WHERE r.intent_id=$1 AND r.revision_number=1 AND (row->>'id')::BIGINT=$2`, intent, itemID).Scan(&auditQty, &auditPrice, &auditName, &auditPacking); e != nil || auditQty != 2 || auditPrice != "100.00" || auditName != "Agreed original name" || auditPacking != 35 {
				t.Fatal("full original removed row audit was not retained", auditQty, auditPrice, auditName, auditPacking, e)
			}
		})
	}
	t.Run("paid partial shipment preserves its immutable composition", func(t *testing.T) {
		id, itemID, _ := create(t)
		drain(t)
		// Actual partial-offer creation does not insert a whole-order packing
		// workflow; its rows are independently frozen and protected.
		exec(`INSERT INTO shipment_offers(id,order_id,status,delivery_method)VALUES($1,$1,'paid','cdek')`, id)
		exec(`INSERT INTO shipment_offer_items(order_item_id,shipment_offer_id,variant_id,product_name,sku,quantity,unit_price)VALUES($1,$2,1,'Frozen partial','13',1,100)`, itemID, id)
		changed := []OrderEditLine{{SKU: "24", Quantity: 1}}
		if _, e := repository.EditOrder(ctx, actor, id, OrderEdit{Items: &changed}); !errors.Is(e, ErrInvalidInput) {
			t.Fatal("paid partial allowed its source SKU to disappear", e)
		}
		var live, frozen int
		if e := pool.QueryRow(ctx, `SELECT COUNT(*) FROM order_items WHERE id=$1 AND quantity=2`, itemID).Scan(&live); e != nil {
			t.Fatal(e)
		}
		if e := pool.QueryRow(ctx, `SELECT COUNT(*) FROM shipment_offer_items WHERE order_item_id=$1 AND quantity=1`, itemID).Scan(&frozen); e != nil || live != 1 || frozen != 1 {
			t.Fatal("partial order history was mutated", live, frozen, e)
		}
		// Re-submitting unchanged fields is a no-op even with this active offer.
		edit(t, id, []OrderEditLine{{SKU: "13", Quantity: 2}}, &OrderDiscount{Type: "fixed", Value: 0})
		if did, e := ordersale.PublicationStep(ctx, pool, ordersale.MockProvider{}, "mock"); e != nil || did {
			t.Fatal("unchanged partial form queued a revision", did, e)
		}
	})
	t.Run("discount and unchanged form stay in same order", func(t *testing.T) {
		id, itemID, intent := create(t)
		drain(t)
		edit(t, id, []OrderEditLine{{SKU: "13", Quantity: 3}}, &OrderDiscount{Type: "fixed", Value: 1})
		drain(t)
		var subtotal string
		var actualID int64
		if e := pool.QueryRow(ctx, `SELECT subtotal::TEXT FROM orders WHERE id=$1`, id).Scan(&subtotal); e != nil || subtotal != "299.00" {
			t.Fatal("net total incorrect", subtotal, e)
		}
		edit(t, id, []OrderEditLine{{SKU: "13", Quantity: 3}}, &OrderDiscount{Type: "fixed", Value: 1})
		if e := pool.QueryRow(ctx, `SELECT id FROM order_items WHERE order_id=$1`, id).Scan(&actualID); e != nil || actualID != itemID {
			t.Fatal("unchanged form recreated line", actualID, e)
		}
		if did, e := ordersale.PublicationStep(ctx, pool, ordersale.MockProvider{}, "mock"); e != nil || did {
			t.Fatal("unchanged form queued another stock operation", did, e)
		}
		fee := 25.0
		if value, e := repository.EditOrder(ctx, actor, id, OrderEdit{DeliveryFee: &fee}); e != nil || value.Total != 324 {
			t.Fatal("fee-only edit changed goods or total", value.Total, e)
		}
		if did, e := ordersale.PublicationStep(ctx, pool, ordersale.MockProvider{}, "mock"); e != nil || did {
			t.Fatal("delivery fee queued stock revision", did, e)
		}
		bad := []OrderEditLine{{SKU: "13", Quantity: 4}, {SKU: "missing", Quantity: 1}}
		if _, e := repository.EditOrder(ctx, actor, id, OrderEdit{Items: &bad}); e == nil {
			t.Fatal("invalid product should reject whole edit")
		}
		var keptQty int
		if e := pool.QueryRow(ctx, `SELECT quantity FROM order_items WHERE id=$1`, itemID).Scan(&keptQty); e != nil || keptQty != 3 {
			t.Fatal("failed edit partially mutated existing row", keptQty, e)
		}
		if did, e := ordersale.PublicationStep(ctx, pool, ordersale.MockProvider{}, "mock"); e != nil || did {
			t.Fatal("failed edit left a dispatchable revision", did, e)
		}
		exec(`INSERT INTO payments(id,order_id,status)VALUES($1,$1,'paid')`, id)
		changedDiscount := OrderDiscount{Type: "fixed", Value: 2}
		if _, e := repository.EditOrder(ctx, actor, id, OrderEdit{Discount: &changedDiscount}); e == nil {
			t.Fatal("existing paid-discount safety gate was bypassed")
		}
		var doc string
		if e := pool.QueryRow(ctx, `SELECT document_id FROM order_sale_publications WHERE intent_id=$1`, intent).Scan(&doc); e != nil || doc != fmt.Sprintf("STG-SALE-%d", intent) {
			t.Fatal(doc, e)
		}
	})
}
