// Package orderworkflow computes fulfilment readiness from the same current
// catalogue and payment facts for lists, details and automatic assembly.
package orderworkflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Blocker struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	View    string `json:"view,omitempty"`
}
type State struct {
	Status             string    `json:"status"`
	Queue              string    `json:"queue"`
	ReadyForAssembly   bool      `json:"readyForAssembly"`
	StockConfirmed     bool      `json:"stockConfirmed"`
	PackagingConfirmed bool      `json:"packagingConfirmed"`
	Blockers           []Blocker `json:"blockers"`
	ManualStatuses     []string  `json:"manualStatuses"`
}
type Line struct {
	Name       string `json:"name"`
	Wanted     int    `json:"wanted"`
	Available  int    `json:"available"`
	StockKnown bool   `json:"stockKnown"`
	Dimensions bool   `json:"dimensions"`
}
type Facts struct {
	Status, PaymentStatus, PaymentMethod, DeliveryMethod, CDEKState string
	FeePending, RepackRequested, AddressConfirmed, PaymentUncertain bool
	Lines                                                           []Line
	BoxesConfirmed                                                  bool
}

func Evaluate(f Facts) State {
	s := State{Status: f.Status, Queue: "new", Blockers: []Blocker{}, ManualStatuses: []string{}, StockConfirmed: len(f.Lines) > 0, PackagingConfirmed: true}
	switch f.Status {
	case "cancelled":
		s.Queue = "cancelled"
		return s
	case "completed":
		s.Queue = "completed"
		return s
	case "shipped", "shipping":
		s.Queue = "in_delivery"
		return s
	case "assembling", "ready":
		s.Queue = "in_progress"
	}
	add := func(code, message, view string) { s.Blockers = append(s.Blockers, Blocker{code, message, view}) }
	if f.PaymentStatus != "paid" {
		message := "Заказ не оплачен"
		if f.PaymentStatus == "partially_paid" {
			message = "Заказ оплачен не полностью"
		}
		if f.PaymentMethod == "on_delivery" && f.DeliveryMethod == "pickup" {
			message = "Оплата при получении"
			if f.Status == "new" || f.Status == "confirmed" {
				message = "Оплата при получении — начните сборку вручную"
			}
		}
		add("payment", message, "payment")
	}
	if f.PaymentUncertain {
		add("payment_check", "Нужно уточнить результат оплаты", "payment")
	}
	if len(f.Lines) == 0 {
		add("composition", "В заказе нет товаров", "items")
	}
	for _, line := range f.Lines {
		if !line.StockKnown {
			s.StockConfirmed = false
			add("stock_unknown", "Нет свежего остатка СБИС: "+line.Name, "items")
		} else if line.Available < line.Wanted {
			s.StockConfirmed = false
			add("stock_short", "Не хватает в наличии: "+line.Name, "items")
		}
		if f.DeliveryMethod == "cdek" && !line.Dimensions && !f.BoxesConfirmed {
			s.PackagingConfirmed = false
			add("dimensions", "Не заполнены габариты и вес: "+line.Name, "delivery")
		}
	}
	if f.DeliveryMethod == "cdek" {
		if f.RepackRequested {
			add("repack", "Нужно подтвердить состав коробки и пересчитать доставку", "delivery")
		} else if f.FeePending {
			add("delivery_fee", "Нужно рассчитать и подтвердить стоимость доставки", "delivery")
		}
		if !f.AddressConfirmed {
			add("delivery_address", "Не выбраны тариф или пункт СДЭК", "delivery")
		}
		if f.CDEKState == "manual_review" {
			add("cdek_review", "Отправление СДЭК требует проверки", "delivery")
		}
	}
	s.ReadyForAssembly = len(s.Blockers) == 0
	if f.DeliveryMethod == "pickup" {
		cashReady := f.PaymentMethod == "on_delivery" && s.StockConfirmed && len(s.Blockers) == 1 && s.Blockers[0].Code == "payment"
		if f.Status == "new" || f.Status == "confirmed" {
			if cashReady {
				s.ManualStatuses = append(s.ManualStatuses, "assembling")
			}
			// Keep the existing pickup shortcut available to older clients;
			// both paths still require fresh stock and the same payment rules.
			if s.ReadyForAssembly || cashReady {
				s.ManualStatuses = append(s.ManualStatuses, "ready")
			}
		}
		if f.Status == "assembling" && (s.ReadyForAssembly || cashReady) {
			s.ManualStatuses = append(s.ManualStatuses, "ready")
		}
		if f.Status == "ready" && (f.PaymentStatus == "paid" || f.PaymentMethod == "on_delivery") {
			s.ManualStatuses = append(s.ManualStatuses, "completed")
		}
	}
	return s
}

type Querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

const factsSQL = `
 SELECT o.id,o.status,o.payment_status,o.payment_method,o.delivery_method,o.delivery_fee_pending=1,o.delivery_repack_requested=1,
  o.cdek_create_state,(COALESCE(o.cdek_tariff_code,0)>0 AND COALESCE(o.cdek_office_code,'')<>''),
  EXISTS(SELECT 1 FROM payments pay WHERE pay.order_id=o.id AND (pay.status='waiting_for_capture' OR (pay.status='pending' AND pay.provider_payment_id=''))),
  COALESCE((SELECT jsonb_agg(jsonb_build_object('name',lines.name,'wanted',lines.wanted,'available',COALESCE(stock.available,0),'stockKnown',COALESCE(stock.known,false),'dimensions',lines.dimensions))
   FROM (SELECT oi.variant_id,MIN(oi.product_name) name,SUM(oi.quantity)::int wanted,
    BOOL_AND(COALESCE(NULLIF((oi.variant_snapshot->>'packageLengthCm')::numeric,0),shipping_package_dimension(oi.variant_id,'package_length_cm'),0)>0 AND
     COALESCE(NULLIF((oi.variant_snapshot->>'packageWidthCm')::numeric,0),shipping_package_dimension(oi.variant_id,'package_width_cm'),0)>0 AND
     COALESCE(NULLIF((oi.variant_snapshot->>'packageHeightCm')::numeric,0),shipping_package_dimension(oi.variant_id,'package_height_cm'),0)>0 AND
     COALESCE(NULLIF((oi.variant_snapshot->>'packageWeightGrams')::numeric,0),variant_numeric_attribute(oi.variant_id,'package_weight_grams'),0)>0) dimensions
    FROM order_items oi WHERE oi.order_id=o.id GROUP BY oi.variant_id) lines
   LEFT JOIN LATERAL (SELECT SUM(GREATEST(i.available_qty-i.reserved_qty,0))::int available,COUNT(*)>0 known
    FROM inventory i JOIN warehouses w ON w.id=i.warehouse_id AND w.saby_id='saby-ryazan-main' AND w.is_active=1
    JOIN product_variants v ON v.id=i.variant_id JOIN products p ON p.id=v.product_id AND 'stock'=ANY(p.saby_fields)
    JOIN saby_nomenclature n ON n.saby_id=v.saby_id AND n.missing_since IS NULL
    WHERE i.variant_id=lines.variant_id AND i.synced_at>=CURRENT_TIMESTAMP-INTERVAL '2 hours') stock ON TRUE),'[]'::jsonb),
  COALESCE((SELECT boxes FROM order_packing_workflows w WHERE w.order_id=o.id AND w.order_revision=o.shipment_revision AND w.state NOT IN ('draft','cancelled','stale') LIMIT 1),'[]'::jsonb),
  COALESCE((SELECT jsonb_object_agg(oi.id::text,oi.quantity) FROM order_items oi WHERE oi.order_id=o.id),'{}'::jsonb)
 FROM orders o WHERE o.id=ANY($1::bigint[]) ORDER BY o.id`

func Read(ctx context.Context, db Querier, ids []int64) (map[int64]State, error) {
	result := map[int64]State{}
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := db.Query(ctx, factsSQL, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var f Facts
		var lines, boxes, quantities []byte
		if err = rows.Scan(&id, &f.Status, &f.PaymentStatus, &f.PaymentMethod, &f.DeliveryMethod, &f.FeePending, &f.RepackRequested, &f.CDEKState, &f.AddressConfirmed, &f.PaymentUncertain, &lines, &boxes, &quantities); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(lines, &f.Lines); err != nil {
			return nil, err
		}
		f.BoxesConfirmed = validBoxes(boxes, quantities)
		result[id] = Evaluate(f)
	}
	return result, rows.Err()
}
func validBoxes(raw, quantities []byte) bool {
	// Box JSON is shared with the established packing workflow; validate the
	// complete saved allocation, never treat an arbitrary nonempty array as proof.
	var entries []struct {
		Length   int `json:"lengthCm"`
		Width    int `json:"widthCm"`
		Height   int `json:"heightCm"`
		Weight   int `json:"weightGrams"`
		Contents []struct {
			OrderItemID int64 `json:"orderItemId"`
			Quantity    int   `json:"quantity"`
		} `json:"contents"`
	}
	var expected map[string]int
	if json.Unmarshal(raw, &entries) != nil || json.Unmarshal(quantities, &expected) != nil || len(entries) == 0 || len(expected) == 0 {
		return false
	}
	actual := map[string]int{}
	for _, box := range entries {
		if box.Length <= 0 || box.Width <= 0 || box.Height <= 0 || box.Weight <= 0 || len(box.Contents) == 0 {
			return false
		}
		for _, line := range box.Contents {
			if line.Quantity <= 0 {
				return false
			}
			actual[fmt.Sprint(line.OrderItemID)] += line.Quantity
		}
	}
	if len(actual) != len(expected) {
		return false
	}
	for id, count := range expected {
		if actual[id] != count {
			return false
		}
	}
	return true
}

// Refresh only advances new/confirmed, fully paid orders. Stock ageing or a
// refund never silently rewinds physical work or changes a carrier milestone.
func Refresh(ctx context.Context, pool *pgxpool.Pool, ids []int64) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id FROM orders WHERE status IN ('new','confirmed') AND payment_status='paid' AND ($1::bigint[] IS NULL OR id=ANY($1)) ORDER BY id FOR UPDATE SKIP LOCKED`, ids)
	if err != nil {
		return err
	}
	selected := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		selected = append(selected, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	states, err := Read(ctx, tx, selected)
	if err != nil {
		return err
	}
	for id, state := range states {
		if state.ReadyForAssembly {
			if _, err = tx.Exec(ctx, `UPDATE orders SET status='assembling',has_preorder=0 WHERE id=$1 AND status IN ('new','confirmed') AND payment_status='paid'`, id); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}
func Run(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		if err := Refresh(ctx, pool, nil); err != nil && ctx.Err() == nil {
			logger.Error("refresh order fulfilment readiness", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
