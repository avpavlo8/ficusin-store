package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/order"
	"github.com/jackc/pgx/v5"
)

type OrderEditLine struct {
	// SKU опознаёт строку заказа. Продаётся конкретный размер, поэтому одна
	// карточка товара ещё не говорит, что именно уходит покупателю.
	SKU string `json:"sku"`
	// ProductID — имя поля до «Каталога v2». Оставлено, чтобы вкладка
	// админки, открытая до релиза, доработала до перезагрузки.
	ProductID string `json:"productId"`
	Quantity  int    `json:"quantity"`
}

func (line OrderEditLine) identity() string {
	if sku := strings.TrimSpace(line.SKU); sku != "" {
		return sku
	}
	return strings.TrimSpace(line.ProductID)
}

type OrderEdit struct {
	// Items is the complete desired composition. nil means "do not change
	// composition"; an explicitly empty array is rejected because an empty
	// order must be cancelled instead of becoming a zero-ruble ghost.
	Items       *[]OrderEditLine `json:"items"`
	DeliveryFee *float64         `json:"deliveryFee"`
}

type editableOrderLine struct {
	sku           string
	variantID     int64
	productID     int64
	name          string
	variantLabel  string
	heightCM      *int
	potDiameterCM *int
	price         float64
	quantity      int
	reserved      int
	preorder      bool
	sabyID        string
}

func normalizeOrderEditLines(lines []OrderEditLine) ([]OrderEditLine, error) {
	result := make([]OrderEditLine, 0, len(lines))
	positions := map[string]int{}
	for _, line := range lines {
		sku := line.identity()
		if sku == "" || line.Quantity <= 0 || line.Quantity > 100 {
			return nil, fmt.Errorf("некорректный товар или количество")
		}
		if index, ok := positions[sku]; ok {
			result[index].Quantity += line.Quantity
			if result[index].Quantity > 100 { return nil, fmt.Errorf("слишком большое количество товара") }
			continue
		}
		positions[sku] = len(result)
		result = append(result, OrderEditLine{SKU: sku, Quantity: line.Quantity})
	}
	if len(result) == 0 { return nil, fmt.Errorf("заказ не может быть пустым — отмените его") }
	return result, nil
}

func releaseOrderReservationsForEdit(ctx context.Context, tx pgx.Tx, orderID int64) error {
	if _, err := tx.Exec(ctx, `
		SELECT i.id FROM inventory i
		WHERE i.variant_id IN (SELECT variant_id FROM order_items WHERE order_id=$1 AND variant_id IS NOT NULL)
		ORDER BY i.id FOR UPDATE
	`, orderID); err != nil { return fmt.Errorf("lock inventory for order edit: %w", err) }
	if _, err := tx.Exec(ctx, `
		WITH wanted AS (
			SELECT variant_id,SUM(reserved_qty)::INTEGER AS quantity
			FROM order_items WHERE order_id=$1 AND variant_id IS NOT NULL GROUP BY variant_id
		), allocation AS (
			SELECT i.id,LEAST(i.reserved_qty,GREATEST(w.quantity-COALESCE(SUM(i.reserved_qty) OVER(
				PARTITION BY i.variant_id ORDER BY i.id ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING
			),0),0)) AS give_back
			FROM inventory i JOIN wanted w ON w.variant_id=i.variant_id
		)
		UPDATE inventory i SET reserved_qty=i.reserved_qty-a.give_back
		FROM allocation a WHERE a.id=i.id AND a.give_back>0
	`, orderID); err != nil { return fmt.Errorf("release inventory for order edit: %w", err) }
	return nil
}

func reserveVariantForEdit(ctx context.Context, tx pgx.Tx, variantID int64, quantity int) (int, bool, error) {
	rows, err := tx.Query(ctx, `
		SELECT id,GREATEST(available_qty-reserved_qty,0) FROM inventory
		WHERE variant_id=$1 ORDER BY id FOR UPDATE
	`, variantID)
	if err != nil { return 0,false,err }
	type slot struct{ id int64; free int }
	slots:=[]slot{}; available:=0
	for rows.Next(){var current slot;if err:=rows.Scan(&current.id,&current.free);err!=nil{rows.Close();return 0,false,err};slots=append(slots,current);available+=current.free}
	rows.Close();if err:=rows.Err();err!=nil{return 0,false,err}
	reserved:=min(quantity,available);remaining:=reserved
	for _,slot:=range slots{if remaining==0{break};take:=min(slot.free,remaining);if take>0{if _,err:=tx.Exec(ctx,`UPDATE inventory SET reserved_qty=reserved_qty+$2 WHERE id=$1`,slot.id,take);err!=nil{return 0,false,err};remaining-=take}}
	return reserved,reserved<quantity,nil
}

func currentAdminOrder(ctx context.Context, repository *PostgresRepository, id int64) (Order,error){
	orders,err:=repository.ListOrders(ctx);if err!=nil{return Order{},err};for _,item:=range orders{if item.ID==id{return item,nil}};return Order{},pgx.ErrNoRows
}

// EditOrder replaces the requested composition transactionally. Existing
// lines keep the price agreed when the order was placed; newly added lines
// use today's storefront price and the customer's current retail discount.
// Stock reservations are released and rebuilt in the same transaction, so an
// edit can never silently sell more than the shelf contains.
func (repository *PostgresRepository) EditOrder(ctx context.Context, actor Actor, id int64, edit OrderEdit) (Order,error){
	if !Can(actor.Role,PermissionOrdersEdit){return Order{},ErrForbidden}
	if edit.DeliveryFee!=nil && (*edit.DeliveryFee<0 || *edit.DeliveryFee>100000){return Order{},fmt.Errorf("стоимость доставки вне разумных пределов")}
	tx,err:=repository.pool.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return Order{},err};defer func(){_ = tx.Rollback(ctx)}()
	var status,deliveryMethod string;var customerID *int64;var oldDeliveryFee float64;var oldFeePending bool
	if err:=tx.QueryRow(ctx,`SELECT status,delivery_method,customer_id,delivery_fee::DOUBLE PRECISION,delivery_fee_pending=1 FROM orders WHERE id=$1 FOR UPDATE`,id).Scan(&status,&deliveryMethod,&customerID,&oldDeliveryFee,&oldFeePending);err!=nil{return Order{},err}
	if status=="cancelled" || status=="completed" || status=="shipped"{return Order{},fmt.Errorf("состав уже закрытого или отправленного заказа менять нельзя")}

	var before map[string]any
	if err:=tx.QueryRow(ctx,`SELECT jsonb_build_object('subtotal',subtotal,'deliveryFee',delivery_fee,'total',total,'hasPreorder',has_preorder,'feePending',delivery_fee_pending) FROM orders WHERE id=$1`,id).Scan(&before);err!=nil{return Order{},err}

	if edit.Items!=nil{
		lines,err:=normalizeOrderEditLines(*edit.Items);if err!=nil{return Order{},err}
		oldPrices:=map[string]float64{}
		rows,err:=tx.Query(ctx,`SELECT COALESCE(sku,''),unit_price::DOUBLE PRECISION FROM order_items WHERE order_id=$1`,id);if err!=nil{return Order{},err}
		for rows.Next(){var sku string;var price float64;if err:=rows.Scan(&sku,&price);err!=nil{rows.Close();return Order{},err};if sku!=""{oldPrices[sku]=price}};rows.Close();if err:=rows.Err();err!=nil{return Order{},err}

		_ = order.RecordMovement(ctx,tx,id,order.MovementRelease)
		if err:=releaseOrderReservationsForEdit(ctx,tx,id);err!=nil{return Order{},err}
		if _,err:=tx.Exec(ctx,`UPDATE procurement_requests SET status='cancelled',updated_at=CURRENT_TIMESTAMP WHERE customer_order_id=$1 AND kind='customer_order' AND status='open'`,id);err!=nil{return Order{},err}
		if _,err:=tx.Exec(ctx,`DELETE FROM order_items WHERE order_id=$1`,id);err!=nil{return Order{},err}

		discountBPS:=0
		if customerID!=nil{_ = tx.QueryRow(ctx,`SELECT COALESCE(retail_discount_bps,0) FROM customers WHERE id=$1`,*customerID).Scan(&discountBPS)}
		hasPreorder:=false
		for _,requested:=range lines{
			var line editableOrderLine;var priceMinor int64
			// Строка ищется по SKU. Прежний запрос искал товар по products.slug —
			// колонке, которую миграция 056 вывела из оборота, — и всегда брал
			// первый вариант карточки, то есть мог поставить в заказ не тот размер
			// и не ту цену, которые выбрал покупатель.
			if err:=tx.QueryRow(ctx,`
				SELECT pv.sku,pv.id,p.id,p.name,pv.label,pv.base_price_minor,COALESCE(p.saby_id,''),
					variant_numeric_attribute(pv.id,'height_cm')::INTEGER,
					variant_numeric_attribute(pv.id,'pot_diameter_cm')::INTEGER
				FROM product_variants pv JOIN products p ON p.id=pv.product_id
				WHERE pv.sku=$1 AND pv.is_active=1 AND pv.archived_at IS NULL AND p.status='published'
				LIMIT 1
			`,requested.identity()).Scan(&line.sku,&line.variantID,&line.productID,&line.name,&line.variantLabel,&priceMinor,&line.sabyID,&line.heightCM,&line.potDiameterCM);err!=nil{
				if errors.Is(err,pgx.ErrNoRows){return Order{},fmt.Errorf("товар %s больше не доступен",requested.identity())};return Order{},err
			}
			line.quantity=requested.Quantity
			if price,ok:=oldPrices[line.sku];ok{line.price=price}else{minor:=priceMinor;if discountBPS>0{if discountBPS>9000{discountBPS=9000};minor=(priceMinor*int64(10000-discountBPS)+5000)/10000};line.price=float64(minor)/100}
			line.reserved,line.preorder,err=reserveVariantForEdit(ctx,tx,line.variantID,line.quantity);if err!=nil{return Order{},err};hasPreorder=hasPreorder||line.preorder
			// Строка обязана нести полный кортеж PRODUCT + variant + SKU:
			// order_items_catalog_identity_check отвергает всё остальное, а без
			// снимка размеров историю заказа потом нечем объяснить.
			if _,err:=tx.Exec(ctx,`
				INSERT INTO order_items(order_id,product_id,variant_id,sku,product_name,variant_label,variant_snapshot,unit_price,quantity,is_preorder,reserved_qty)
				VALUES($1,$2,$3,$4,$5,$6,jsonb_strip_nulls(jsonb_build_object('heightCm',$7::INTEGER,'potDiameterCm',$8::INTEGER)),$9,$10,$11,$12)
			`,id,line.productID,line.variantID,line.sku,line.name,line.variantLabel,line.heightCM,line.potDiameterCM,line.price,line.quantity,boolToSmallInt(line.preorder),line.reserved);err!=nil{return Order{},err}
			if line.preorder{
				if _,err:=tx.Exec(ctx,`INSERT INTO procurement_requests(kind,saby_id,requested_name,quantity,customer_order_id,status,notes) VALUES('customer_order',NULLIF($1,''),$2,$3,$4,'open','Предзаказ после изменения заказа')`,line.sabyID,line.name,line.quantity,id);err!=nil{return Order{},err}
			}
		}
		if err:=order.RecordMovement(ctx,tx,id,order.MovementReserve);err!=nil{return Order{},err}

		// Изменение состава выполняет только менеджер. Оно не должно само по
		// себе возвращать уже подтверждённую доставку в pending. Если доставка
		// была неизвестна, она останется pending до передачи DeliveryFee; если
		// уже была подтверждена — сохранит это состояние.
		if _,err:=tx.Exec(ctx,`UPDATE orders SET has_preorder=$2,delivery_fee_pending=$3,stock_released_at=NULL WHERE id=$1`,id,boolToSmallInt(hasPreorder),boolToSmallInt(oldFeePending));err!=nil{return Order{},err}
	}

	if edit.DeliveryFee!=nil{
		if _,err:=tx.Exec(ctx,`UPDATE orders SET delivery_fee=$2,delivery_fee_pending=0,delivery_repack_requested=0 WHERE id=$1`,id,*edit.DeliveryFee);err!=nil{return Order{},err}
	}
	if _,err:=tx.Exec(ctx,`
		UPDATE orders SET subtotal=COALESCE((SELECT SUM(unit_price*quantity) FROM order_items WHERE order_id=$1),0),
			total=COALESCE((SELECT SUM(unit_price*quantity) FROM order_items WHERE order_id=$1),0)+delivery_fee
		WHERE id=$1
	`,id);err!=nil{return Order{},err}
	var after map[string]any
	if err:=tx.QueryRow(ctx,`SELECT jsonb_build_object('subtotal',subtotal,'deliveryFee',delivery_fee,'total',total,'hasPreorder',has_preorder,'feePending',delivery_fee_pending) FROM orders WHERE id=$1`,id).Scan(&after);err!=nil{return Order{},err}
	if err:=insertAudit(ctx,tx,actor,"order.contents.update","order",fmt.Sprint(id),before,after);err!=nil{return Order{},err}
	if err:=tx.Commit(ctx);err!=nil{return Order{},err}
	return currentAdminOrder(ctx,repository,id)
}
