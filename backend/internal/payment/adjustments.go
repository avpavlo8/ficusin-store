package payment

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/jackc/pgx/v5"
)

// Balance is the money position of one mutable order. Paid and refunded are
// deliberately separate facts: an order may have several successful card
// charges, partial refunds and a later top-up.
type Balance struct {
	Total         float64 `json:"total"`
	Paid          float64 `json:"paid"`
	Refunded      float64 `json:"refunded"`
	NetPaid       float64 `json:"netPaid"`
	Due           float64 `json:"due"`
	Overpaid      float64 `json:"overpaid"`
	Ready         bool    `json:"ready"`
	PaymentStatus string  `json:"paymentStatus"`
	Issues        []Issue `json:"issues"`
}

type orderMoneyState struct {
	id          int64
	number      string
	total       float64
	paid        float64
	refunded    float64
	feePending  bool
	hasPreorder bool
	method      string
	status      string
	email       string
	phone       string
}

func cents(value float64) int64 { return int64(math.Round(value * 100)) }
func rublesFromCents(value int64) float64 { return float64(value) / 100 }

func balanceFromState(state orderMoneyState) Balance {
	total := cents(state.total)
	paid := cents(state.paid)
	refunded := cents(state.refunded)
	net := paid - refunded
	if net < 0 {
		net = 0
	}
	due := total - net
	overpaid := int64(0)
	if due < 0 {
		overpaid = -due
		due = 0
	}
	ready := !state.feePending && !state.hasPreorder && state.method == MethodOnline &&
		state.status != "cancelled" && state.status != "completed"

	status := StatusPending
	switch {
	case state.method == MethodOnDelivery:
		status = StatusOnDelivery
	case state.method == MethodInvoice:
		status = StatusInvoice
	case state.status == "cancelled" && net == 0 && refunded > 0:
		status = "refunded"
	case state.status == "cancelled":
		status = StatusCancelled
	case total > 0 && due == 0 && overpaid == 0:
		status = StatusPaid
	case net > 0:
		status = "partially_paid"
	case refunded > 0 && total == 0:
		status = "refunded"
	default:
		status = StatusPending
	}
	return Balance{
		Total: rublesFromCents(total), Paid: rublesFromCents(paid), Refunded: rublesFromCents(refunded),
		NetPaid: rublesFromCents(net), Due: rublesFromCents(due), Overpaid: rublesFromCents(overpaid),
		Ready: ready, PaymentStatus: status,
	}
}

func (service *Service) moneyStateByOrderID(ctx context.Context, orderID int64) (orderMoneyState, error) {
	var state orderMoneyState
	err := service.pool.QueryRow(ctx, `
		SELECT o.id, o.order_number, o.total::DOUBLE PRECISION,
			COALESCE((SELECT SUM(p.amount) FROM payments p
				WHERE p.order_id=o.id AND p.status='paid'),0)::DOUBLE PRECISION,
			COALESCE((SELECT SUM(r.amount) FROM payment_refunds r
				WHERE r.order_id=o.id AND r.status='succeeded'),0)::DOUBLE PRECISION,
			o.delivery_fee_pending=1, o.has_preorder=1, o.payment_method, o.status,
			COALESCE(o.email,''), COALESCE(o.phone,'')
		FROM orders o WHERE o.id=$1
	`, orderID).Scan(
		&state.id, &state.number, &state.total, &state.paid, &state.refunded,
		&state.feePending, &state.hasPreorder, &state.method, &state.status,
		&state.email, &state.phone,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, errors.New("заказ не найден")
	}
	if err != nil {
		return state, fmt.Errorf("load order payment balance: %w", err)
	}
	return state, nil
}

func (service *Service) moneyStateByOrderNumber(ctx context.Context, orderNumber string) (orderMoneyState, error) {
	var id int64
	if err := service.pool.QueryRow(ctx, `SELECT id FROM orders WHERE order_number=$1`, strings.TrimSpace(orderNumber)).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) { return orderMoneyState{}, errors.New("заказ не найден") }
		return orderMoneyState{}, err
	}
	return service.moneyStateByOrderID(ctx, id)
}

// Reconcile rebuilds the order payment status from immutable money facts.
// It is safe after a payment, refund, delivery change or composition edit.
func (service *Service) Reconcile(ctx context.Context, orderID int64) (Balance, error) {
	state, err := service.moneyStateByOrderID(ctx, orderID)
	if err != nil { return Balance{}, err }
	balance := balanceFromState(state)
	if _, err := service.pool.Exec(ctx, `UPDATE orders SET payment_status=$2 WHERE id=$1`, orderID, balance.PaymentStatus); err != nil {
		return Balance{}, fmt.Errorf("reconcile order payment status: %w", err)
	}
	balance.Issues, err = service.paymentIssues(ctx, orderID)
	if err != nil { return Balance{}, fmt.Errorf("load payment issues: %w", err) }
	return balance, nil
}

func (service *Service) BalanceForOrder(ctx context.Context, orderID int64) (Balance, error) {
	state, err := service.moneyStateByOrderID(ctx, orderID)
	if err != nil { return Balance{}, err }
	balance := balanceFromState(state)
	balance.Issues, err = service.paymentIssues(ctx, orderID)
	if err != nil { return Balance{}, fmt.Errorf("load payment issues: %w", err) }
	return balance, nil
}

// StartOutstanding starts exactly the amount still owed, never the whole
// order again. It also refuses to take money until stock and delivery are
// both fully known.
func (service *Service) StartOutstanding(ctx context.Context, orderNumber string) (string, Balance, error) {
	if !service.Configured() { return "", Balance{}, errors.New("оплата картой временно недоступна") }
	state, err := service.moneyStateByOrderNumber(ctx, orderNumber)
	if err != nil { return "", Balance{}, err }
	balance := balanceFromState(state)
	if state.method != MethodOnline { return "", balance, errors.New("для заказа выбран другой способ оплаты") }
	if state.status == "cancelled" || state.status == "completed" { return "", balance, errors.New("этот заказ уже закрыт") }
	if !balance.Ready {
		return "", balance, errors.New("оплата будет доступна после подтверждения наличия и доставки менеджером")
	}
	if cents(balance.Due) <= 0 { return "", balance, errors.New("заказ уже полностью оплачен") }

	// A changed order must never keep a live link for an obsolete amount.
	var pendingID int64
	var pendingAmount float64
	var pendingURL string
	err = service.pool.QueryRow(ctx, `
		SELECT id, amount::DOUBLE PRECISION, confirmation_url FROM payments
		WHERE order_id=$1 AND status='pending' ORDER BY id DESC LIMIT 1
	`, state.id).Scan(&pendingID, &pendingAmount, &pendingURL)
	if err == nil {
		if cents(pendingAmount) == cents(balance.Due) {
			if pendingURL != "" { return pendingURL, balance, nil }
			recoveredURL, recoverErr := service.RecoverUnknown(ctx, pendingID)
			return recoveredURL, balance, recoverErr
		}
		if cancelErr := service.CancelPending(ctx, state.id); cancelErr != nil {
			return "", balance, fmt.Errorf("не удалось закрыть прежнюю ссылку на оплату: %w", cancelErr)
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return "", balance, fmt.Errorf("load pending payment: %w", err)
	}

	key, err := idempotenceKey()
	if err != nil { return "", balance, err }
	var paymentID int64
	if err := service.pool.QueryRow(ctx, `
		INSERT INTO payments(order_id,idempotence_key,amount,status)
		VALUES($1,$2,$3,'pending') RETURNING id
	`, state.id, key, balance.Due).Scan(&paymentID); err != nil {
		return "", balance, fmt.Errorf("create outstanding payment: %w", err)
	}
	created, err := service.attemptPaymentCreation(ctx, paymentID, integration.PaymentRequest{
		IdempotenceKey: key,
		Amount: balance.Due,
		Description: "Оплата заказа " + state.number + " — Фикусин",
		ReturnURL: service.returnURL + "/?paid=" + state.number,
		Email: state.email, Phone: state.phone,
		// A top-up may represent a mixture of an added product and changed
		// delivery. The receipt must still equal the payment amount exactly.
		Items: []integration.PaymentItem{{Name: "Доплата по заказу " + state.number, Price: balance.Due, Quantity: 1}},
	})
	if err != nil {
		return "", balance, err
	}
	return created.ConfirmationURL, balance, nil
}

func (service *Service) StartOutstandingForOrderID(ctx context.Context, orderID int64) (string, Balance, error) {
	state, err := service.moneyStateByOrderID(ctx, orderID)
	if err != nil { return "", Balance{}, err }
	return service.StartOutstanding(ctx, state.number)
}

// StartShipmentOffer charges one immutable shipment, not the mutable balance
// of its parent order. The receipt therefore contains exactly these products
// and this delivery charge.
func (service *Service) StartShipmentOffer(ctx context.Context,token string,customerID int64)(string,error){
	if !service.Configured(){return "",errors.New("оплата картой временно недоступна")}
	token=strings.TrimSpace(token);var offerID,orderID int64;var number,email,phone,status,addressSnapshot,address string;var amount float64;var expires time.Time;var offerRevision,revision int
	err:=service.pool.QueryRow(ctx,`SELECT so.id,o.id,o.order_number,COALESCE(o.email,''),o.phone,so.status,so.total::DOUBLE PRECISION,so.expires_at,so.address_snapshot,o.address,so.order_revision,o.shipment_revision FROM shipment_offers so JOIN orders o ON o.id=so.order_id WHERE so.public_token=$1 AND o.customer_id=$2`,token,customerID).Scan(&offerID,&orderID,&number,&email,&phone,&status,&amount,&expires,&addressSnapshot,&address,&offerRevision,&revision)
	if errors.Is(err,pgx.ErrNoRows){return "",errors.New("предложение отправки не найдено")};if err!=nil{return "",err}
	if status=="paid"{return "",errors.New("эта отправка уже оплачена")};if status!="offered"&&status!="payment_pending"{return "",errors.New("ссылка на эту отправку недействительна")}
	if !expires.After(time.Now()){_,_=service.pool.Exec(ctx,`UPDATE shipment_offers SET status='expired',updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND status='offered'`,offerID);return "",errors.New("срок предложения истёк")}
	if offerRevision!=revision||addressSnapshot!=address{_,_=service.pool.Exec(ctx,`UPDATE shipment_offers SET status='stale',updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND status='offered'`,offerID);return "",errors.New("заказ изменился — менеджер подготовит новое предложение")}
	var paymentID int64;var key,existingURL string;var existingAmount float64
	err=service.pool.QueryRow(ctx,`SELECT id,idempotence_key,confirmation_url,amount::DOUBLE PRECISION FROM payments WHERE shipment_offer_id=$1 AND status='pending' ORDER BY id DESC LIMIT 1`,offerID).Scan(&paymentID,&key,&existingURL,&existingAmount)
	if err==nil{if cents(existingAmount)!=cents(amount){return "",errors.New("сумма предложения изменилась при открытой оплате")};if existingURL!=""{return existingURL,nil}}else if !errors.Is(err,pgx.ErrNoRows){return "",err}
	items:=[]integration.PaymentItem{};rows,err:=service.pool.Query(ctx,`SELECT product_name,unit_price::DOUBLE PRECISION,quantity FROM shipment_offer_items WHERE shipment_offer_id=$1 ORDER BY id`,offerID);if err!=nil{return "",err};for rows.Next(){var item integration.PaymentItem;if err:=rows.Scan(&item.Name,&item.Price,&item.Quantity);err!=nil{rows.Close();return "",err};items=append(items,item)};rows.Close();var delivery float64;_ = service.pool.QueryRow(ctx,`SELECT delivery_fee::DOUBLE PRECISION FROM shipment_offers WHERE id=$1`,offerID).Scan(&delivery);if delivery>0{items=append(items,integration.PaymentItem{Name:"Доставка",Price:delivery,Quantity:1})}
	if paymentID==0{key,err=idempotenceKey();if err!=nil{return "",err};paymentID,err=service.reserveOfferAndCreatePayment(ctx,offerID,orderID,key,amount);if err!=nil{return "",err}}
	created,err:=service.attemptPaymentCreation(ctx,paymentID,integration.PaymentRequest{IdempotenceKey:key,Amount:amount,Description:"Оплата отправки по заказу "+number+" — Фикусин",ReturnURL:service.returnURL+"/account/orders/"+number,Email:email,Phone:phone,Items:items});if err!=nil{return "",err};return created.ConfirmationURL,nil
}

func (service *Service) reserveOfferAndCreatePayment(ctx context.Context,offerID,orderID int64,key string,amount float64)(int64,error){
	tx,err:=service.pool.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return 0,err};defer func(){_ = tx.Rollback(ctx)}()
	var status string;if err:=tx.QueryRow(ctx,`SELECT status FROM shipment_offers WHERE id=$1 FOR UPDATE`,offerID).Scan(&status);err!=nil{return 0,err};if status!="offered"{return 0,errors.New("предложение уже обрабатывается")}
	rows,err:=tx.Query(ctx,`SELECT soi.id,soi.order_item_id,soi.variant_id,soi.quantity,oi.reserved_qty FROM shipment_offer_items soi JOIN order_items oi ON oi.id=soi.order_item_id WHERE soi.shipment_offer_id=$1 ORDER BY soi.id`,offerID);if err!=nil{return 0,err}
	type line struct{id,orderItemID int64;variantID *int64;quantity,reserved int};lines:=[]line{};for rows.Next(){var current line;if err:=rows.Scan(&current.id,&current.orderItemID,&current.variantID,&current.quantity,&current.reserved);err!=nil{rows.Close();return 0,err};lines=append(lines,current)};rows.Close();if err:=rows.Err();err!=nil{return 0,err}
	for _,current:=range lines{needed:=max(0,current.quantity-current.reserved);if needed==0{continue};if current.variantID==nil{return 0,errors.New("у товара нет складской позиции")};stock,err:=tx.Query(ctx,`SELECT id,GREATEST(available_qty-reserved_qty,0) FROM inventory WHERE variant_id=$1 ORDER BY id FOR UPDATE`,*current.variantID);if err!=nil{return 0,err};type stockRow struct{id int64;free int};stocks:=[]stockRow{};for stock.Next(){var row stockRow;if err:=stock.Scan(&row.id,&row.free);err!=nil{stock.Close();return 0,err};stocks=append(stocks,row)};stock.Close();if err:=stock.Err();err!=nil{return 0,err};remaining:=needed;for _,row:=range stocks{inventoryID,free:=row.id,row.free;take:=min(free,remaining);if take==0{continue};if _,err:=tx.Exec(ctx,`UPDATE inventory SET reserved_qty=reserved_qty+$2 WHERE id=$1`,inventoryID,take);err!=nil{stock.Close();return 0,err};if _,err:=tx.Exec(ctx,`INSERT INTO shipment_offer_inventory_reservations(shipment_offer_item_id,inventory_id,quantity) VALUES($1,$2,$3) ON CONFLICT(shipment_offer_item_id,inventory_id) DO UPDATE SET quantity=shipment_offer_inventory_reservations.quantity+EXCLUDED.quantity`,current.id,inventoryID,take);err!=nil{stock.Close();return 0,err};remaining-=take;if remaining==0{break}};stock.Close();if remaining>0{return 0,errors.New("товар уже закончился: обновите предложение")};if _,err:=tx.Exec(ctx,`UPDATE order_items SET reserved_qty=reserved_qty+$2 WHERE id=$1`,current.orderItemID,needed);err!=nil{return 0,err};if _,err:=tx.Exec(ctx,`UPDATE shipment_offer_items SET checkout_reserved_qty=checkout_reserved_qty+$2 WHERE id=$1`,current.id,needed);err!=nil{return 0,err}}
	var paymentID int64;err=tx.QueryRow(ctx,`INSERT INTO payments(order_id,shipment_offer_id,idempotence_key,amount,status) VALUES($1,$2,$3,$4,'pending') ON CONFLICT (order_id) WHERE status='pending' DO NOTHING RETURNING id`,orderID,offerID,key,amount).Scan(&paymentID);if errors.Is(err,pgx.ErrNoRows){return 0,errors.New("по заказу уже открыта другая оплата")};if err!=nil{return 0,err};if _,err:=tx.Exec(ctx,`UPDATE shipment_offers SET status='payment_pending',checkout_locked_until=CURRENT_TIMESTAMP+INTERVAL '15 minutes',updated_at=CURRENT_TIMESTAMP WHERE id=$1`,offerID);err!=nil{return 0,err};if err:=tx.Commit(ctx);err!=nil{return 0,err};return paymentID,nil
}

func (service *Service) releaseOfferCheckoutReservation(ctx context.Context,offerID int64)error{tx,err:=service.pool.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return err};defer func(){_ = tx.Rollback(ctx)}();if _,err:=tx.Exec(ctx,`UPDATE inventory i SET reserved_qty=GREATEST(0,i.reserved_qty-r.quantity) FROM shipment_offer_inventory_reservations r JOIN shipment_offer_items soi ON soi.id=r.shipment_offer_item_id WHERE i.id=r.inventory_id AND soi.shipment_offer_id=$1`,offerID);err!=nil{return err};if _,err:=tx.Exec(ctx,`UPDATE order_items oi SET reserved_qty=GREATEST(0,oi.reserved_qty-soi.checkout_reserved_qty) FROM shipment_offer_items soi WHERE oi.id=soi.order_item_id AND soi.shipment_offer_id=$1`,offerID);err!=nil{return err};if _,err:=tx.Exec(ctx,`DELETE FROM shipment_offer_inventory_reservations r USING shipment_offer_items soi WHERE r.shipment_offer_item_id=soi.id AND soi.shipment_offer_id=$1`,offerID);err!=nil{return err};if _,err:=tx.Exec(ctx,`UPDATE shipment_offer_items SET checkout_reserved_qty=0 WHERE shipment_offer_id=$1`,offerID);err!=nil{return err};return tx.Commit(ctx)}

// SyncOutstanding is the webhook path for mutable orders. One successful
// top-up no longer means the entire order is paid: after recording the charge
// we recompute the balance from all payments and refunds.
func (service *Service) SyncOutstanding(ctx context.Context, providerPaymentID string) error {
	if !service.Configured() { return errors.New("оплата не настроена") }
	payment, err := service.provider.FetchPayment(ctx, providerPaymentID)
	if err != nil { return err }
	var paymentRowID, orderID int64
	var shipmentOfferID *int64
	var expected float64
	if err := service.pool.QueryRow(ctx, `
		SELECT id,order_id,shipment_offer_id,amount::DOUBLE PRECISION FROM payments WHERE provider_payment_id=$1
	`, providerPaymentID).Scan(&paymentRowID, &orderID,&shipmentOfferID, &expected); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			var adopted bool
			paymentRowID,orderID,shipmentOfferID,expected,adopted,err=service.adoptUnknownFromMetadata(ctx,payment)
			if err!=nil{return fmt.Errorf("adopt unknown payment: %w",err)}
			if !adopted{
				safePaymentID := strings.ReplaceAll(providerPaymentID, "\n", "")
				safePaymentID = strings.ReplaceAll(safePaymentID, "\r", "")
				service.logger.Warn("unknown payment notified", "payment_id", safePaymentID)
				return nil
			}
		} else {
			return fmt.Errorf("load payment: %w", err)
		}
	}
	paid := payment.Paid && payment.Status == "succeeded" && cents(payment.Amount) >= cents(expected)
	status := payment.Status
	var paidAt any
	if paid { status = StatusPaid; paidAt = time.Now() }
	if _, err := service.pool.Exec(ctx, `
		UPDATE payments SET status=$2,paid_at=$3,updated_at=CURRENT_TIMESTAMP WHERE id=$1
	`, paymentRowID, status, paidAt); err != nil {
		return fmt.Errorf("update payment: %w", err)
	}
	if shipmentOfferID!=nil { if paid { _,err=service.pool.Exec(ctx,`UPDATE shipment_offers SET status='paid',checkout_locked_until=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND status IN ('offered','payment_pending')`,*shipmentOfferID) } else if status=="canceled"||status=="cancelled" { if err=service.releaseOfferCheckoutReservation(ctx,*shipmentOfferID);err!=nil{return err};_,err=service.pool.Exec(ctx,`UPDATE shipment_offers SET status=CASE WHEN expires_at<=CURRENT_TIMESTAMP THEN 'expired' ELSE 'offered' END,checkout_locked_until=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=$1`,*shipmentOfferID) };if err!=nil{return err} }
	_, err = service.Reconcile(ctx, orderID)
	return err
}

type refundablePayment struct {
	id         int64
	providerID string
	available  int64
}

func (service *Service) refundablePayments(ctx context.Context, orderID int64) ([]refundablePayment, error) {
	rows, err := service.pool.Query(ctx, `
		SELECT p.id,p.provider_payment_id,
			ROUND((p.amount-COALESCE((SELECT SUM(r.amount) FROM payment_refunds r
				WHERE r.payment_id=p.id AND r.status='succeeded'),0))*100)::BIGINT
		FROM payments p
		WHERE p.order_id=$1 AND p.status='paid' AND p.provider_payment_id<>''
		ORDER BY p.id DESC
	`, orderID)
	if err != nil { return nil, err }
	defer rows.Close()
	result := []refundablePayment{}
	for rows.Next() {
		var item refundablePayment
		if err := rows.Scan(&item.id,&item.providerID,&item.available); err != nil { return nil, err }
		if item.available > 0 { result = append(result,item) }
	}
	return result, rows.Err()
}

// resumePendingRefunds reuses the same idempotence key after an ambiguous
// network failure. Creating a new key could return the same money twice.
func (service *Service) resumePendingRefunds(ctx context.Context, orderID int64) (bool, error) {
	rows, err := service.pool.Query(ctx, `
		SELECT r.id,p.provider_payment_id,r.amount::DOUBLE PRECISION,r.idempotence_key
		FROM payment_refunds r JOIN payments p ON p.id=r.payment_id
		WHERE r.order_id=$1 AND r.status='pending' ORDER BY r.id
	`, orderID)
	if err != nil { return false, err }
	type pending struct{ id int64; provider string; amount float64; key string }
	items:=[]pending{}
	for rows.Next(){ var item pending; if err:=rows.Scan(&item.id,&item.provider,&item.amount,&item.key);err!=nil{rows.Close();return false,err};items=append(items,item) }
	rows.Close(); if err:=rows.Err();err!=nil{return false,err}
	if len(items)==0{return false,nil}
	for _,item:=range items{
		if err:=service.provider.Refund(ctx,item.provider,item.amount,item.key);err!=nil{return true,err}
		if _,err:=service.pool.Exec(ctx,`UPDATE payment_refunds SET status='succeeded' WHERE id=$1`,item.id);err!=nil{return true,err}
	}
	_,err=service.Reconcile(ctx,orderID)
	return true,err
}

// RefundAmount returns an arbitrary part of the money to the same payment
// method through YooKassa. If several charges funded the order, the refund is
// split across them without exceeding what each charge still holds.
func (service *Service) RefundAmount(ctx context.Context, orderID int64, amount float64, reason string) (Balance, error) {
	if !service.Configured() { return Balance{}, errors.New("возврат недоступен: оплата не настроена") }
	requested := cents(amount)
	if requested <= 0 { return Balance{}, errors.New("сумма возврата должна быть больше нуля") }
	if resumed, err := service.resumePendingRefunds(ctx, orderID); resumed {
		if err != nil { return Balance{}, err }
		return service.BalanceForOrder(ctx, orderID)
	}
	balance, err := service.BalanceForOrder(ctx, orderID)
	if err != nil { return Balance{}, err }
	if requested > cents(balance.NetPaid) { return balance, errors.New("нельзя вернуть больше, чем фактически осталось оплачено") }
	payments, err := service.refundablePayments(ctx, orderID)
	if err != nil { return balance, err }
	remaining := requested
	for _, payment := range payments {
		if remaining <= 0 { break }
		part := min(remaining, payment.available)
		key, err := idempotenceKey(); if err != nil { return balance, err }
		var refundID int64
		if err := service.pool.QueryRow(ctx, `
			INSERT INTO payment_refunds(order_id,payment_id,idempotence_key,amount,reason,status)
			VALUES($1,$2,$3,$4,$5,'pending') RETURNING id
		`, orderID,payment.id,key,rublesFromCents(part),strings.TrimSpace(reason)).Scan(&refundID); err != nil {
			return balance, fmt.Errorf("record refund attempt: %w",err)
		}
		if err := service.provider.Refund(ctx,payment.providerID,rublesFromCents(part),key); err != nil {
			// Keep pending: the same idempotence key is reused on the next try.
			return balance, err
		}
		if _,err:=service.pool.Exec(ctx,`UPDATE payment_refunds SET status='succeeded' WHERE id=$1`,refundID);err!=nil{return balance,err}
		remaining -= part
	}
	if remaining > 0 { return balance, errors.New("недостаточно возвратных платежей по заказу") }
	return service.Reconcile(ctx, orderID)
}

// RefundExcess is used after the manager edits an already paid order. It
// automatically returns only the overpayment; if the new total is larger,
// nothing is charged until a new link is explicitly opened.
func (service *Service) RefundExcess(ctx context.Context, orderID int64, reason string) (Balance, error) {
	balance, err := service.Reconcile(ctx, orderID)
	if err != nil { return Balance{}, err }
	if cents(balance.Overpaid) <= 0 { return balance, nil }
	return service.RefundAmount(ctx, orderID, balance.Overpaid, reason)
}
