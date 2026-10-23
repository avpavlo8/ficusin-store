package payment

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/avpavlo8/ficusin-store/backend/internal/money"
	"sort"
)

type RefundSelection struct {
	PaymentID int64 `json:"paymentId"`
	ItemIndex int   `json:"itemIndex"`
	Quantity  int   `json:"quantity"`
}
type RefundOption struct {
	PaymentID int64   `json:"paymentId"`
	ItemIndex int     `json:"itemIndex"`
	Quantity  int     `json:"quantity"`
	Name      string  `json:"name"`
	Price     float64 `json:"price"`
	original  integration.PaymentItem
}
type selectedRefund struct {
	amount int64
	items  []integration.PaymentItem
}

func selectionSignature(items []RefundSelection) (string, error) {
	if len(items) > 1000 {
		return "", errors.New("слишком много позиций возврата")
	}
	copyItems := append([]RefundSelection{}, items...)
	sort.Slice(copyItems, func(i, j int) bool {
		if copyItems[i].PaymentID != copyItems[j].PaymentID {
			return copyItems[i].PaymentID < copyItems[j].PaymentID
		}
		return copyItems[i].ItemIndex < copyItems[j].ItemIndex
	})
	for i, item := range copyItems {
		if item.PaymentID <= 0 || item.ItemIndex < 0 || item.Quantity <= 0 || item.Quantity > 10000 || (i > 0 && copyItems[i-1].PaymentID == item.PaymentID && copyItems[i-1].ItemIndex == item.ItemIndex) {
			return "", errors.New("некорректные или повторные позиции возврата")
		}
	}
	raw, err := json.Marshal(copyItems)
	return string(raw), err
}
func (s *Service) refundOptions(ctx context.Context, orderID int64) ([]RefundOption, error) {
	rows, err := s.pool.Query(ctx, `SELECT p.id,p.request_payload,
 COALESCE((SELECT jsonb_agg(jsonb_build_object('selections',r.selections)) FROM payment_refunds r WHERE r.payment_id=p.id AND r.status IN ('pending','succeeded')),'[]'::jsonb)
 FROM payments p WHERE p.order_id=$1 AND p.status='paid' AND p.provider_payment_id<>'' ORDER BY p.id DESC`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	options := []RefundOption{}
	for rows.Next() {
		var id int64
		var raw, history []byte
		if err = rows.Scan(&id, &raw, &history); err != nil {
			return nil, err
		}
		var request integration.PaymentRequest
		if len(raw) == 0 || json.Unmarshal(raw, &request) != nil {
			continue
		}
		var refunds []struct {
			Selections []RefundSelection `json:"selections"`
		}
		if err = json.Unmarshal(history, &refunds); err != nil {
			return nil, err
		}
		used := map[int]int{}
		unknown := false
		for _, refund := range refunds {
			if len(refund.Selections) == 0 {
				unknown = true
				break
			}
			for _, item := range refund.Selections {
				if item.PaymentID == id {
					used[item.ItemIndex] += item.Quantity
				}
			}
		}
		if unknown {
			continue
		} // Historical amount-only refunds cannot identify remaining fiscal units.
		for i, item := range request.Items {
			price, e := money.Exact(item.Price)
			if e != nil || price <= 0 || item.Quantity <= used[i] {
				continue
			}
			options = append(options, RefundOption{id, i, item.Quantity - used[i], item.Name, item.Price, item})
		}
	}
	return options, rows.Err()
}
func (s *Service) selectedRefunds(ctx context.Context, orderID int64, items []RefundSelection, requested int64) (map[int64]selectedRefund, error) {
	result := map[int64]selectedRefund{}
	if len(items) == 0 {
		return result, nil
	}
	options, err := s.refundOptions(ctx, orderID)
	if err != nil {
		return nil, err
	}
	var total int64
	for _, item := range items {
		found := false
		for _, option := range options {
			if option.PaymentID != item.PaymentID || option.ItemIndex != item.ItemIndex {
				continue
			}
			found = true
			if item.Quantity <= 0 || item.Quantity > option.Quantity {
				return nil, errors.New("количество возврата превышает доступное в исходном платеже")
			}
			unit, e := money.Exact(option.Price)
			if e != nil || unit > money.MaxMinor/int64(item.Quantity) || total > money.MaxMinor-unit*int64(item.Quantity) {
				return nil, errors.New("сумма позиций вне диапазона")
			}
			part := result[item.PaymentID]
			part.amount += unit * int64(item.Quantity)
			line := option.original
			line.Quantity = item.Quantity
			part.items = append(part.items, line)
			result[item.PaymentID] = part
			total += unit * int64(item.Quantity)
		}
		if !found {
			return nil, errors.New("позиция исходного платежа недоступна для возврата; обновите заказ")
		}
	}
	if total != requested {
		return nil, errors.New("сумма возврата не совпадает с выбранными позициями чека")
	}
	return result, nil
}
func refundReceiptStatus(req integration.RefundRequest, result integration.Refund) string {
	switch result.ReceiptStatus {
	case "succeeded", "pending", "canceled":
		return result.ReceiptStatus
	}
	if req.Receipt == nil && !req.ReceiptByProvider {
		return "legacy_unverified"
	}
	return "unknown"
}
func (s *Service) syncRefundReceipts(ctx context.Context, orderID int64) error {
	rows, err := s.pool.Query(ctx, `SELECT id FROM payment_refunds WHERE order_id=$1 AND status='succeeded' AND provider_refund_id<>'' AND receipt_status IN ('','pending','unknown','canceled','legacy_unverified') AND (receipt_checked_at IS NULL OR receipt_checked_at<CURRENT_TIMESTAMP-INTERVAL '5 minutes')`, orderID)
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err = s.syncRefund(ctx, id); err != nil {
			return err
		}
	}
	return nil
}
