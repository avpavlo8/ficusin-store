package integration

import (
	"context"
	"errors"
	"fmt"
	"github.com/avpavlo8/ficusin-store/backend/internal/money"
	"net/http"
	"net/url"
	"strings"
)

type SettlementReceiptRequest struct {
	PaymentID string
	Key       string
	Body      map[string]any
}
type FiscalReceipt struct {
	ID        string `json:"id"`
	PaymentID string `json:"payment_id"`
	Type      string `json:"type"`
	Status    string `json:"status"`
}

func (c *YooKassaClient) PrepareSettlementReceipt(paymentID, key string, original PaymentRequest, items []PaymentItem) (SettlementReceiptRequest, error) {
	if paymentID == "" || key == "" || strings.TrimSpace(original.Email) == "" || len(items) == 0 || len(items) > 80 {
		return SettlementReceiptRequest{}, errors.New("для чека зачёта нужны email и точные позиции исходного платежа")
	}
	request := original
	request.Items = append([]PaymentItem{}, items...)
	var total int64
	for i, item := range request.Items {
		unit, err := money.Exact(item.Price)
		if err != nil || unit <= 0 || item.Quantity <= 0 || unit > money.MaxMinor/int64(item.Quantity) || total > money.MaxMinor-unit*int64(item.Quantity) {
			return SettlementReceiptRequest{}, errors.New("некорректная сумма чека зачёта")
		}
		total += unit * int64(item.Quantity)
		request.Items[i].Mode = "full_payment"
	}
	body := c.receipt(request)
	body["payment_id"] = paymentID
	body["type"] = "payment"
	body["send"] = true
	body["settlements"] = []map[string]any{{"type": "prepayment", "amount": map[string]string{"value": money.Format(total), "currency": "RUB"}}}
	return SettlementReceiptRequest{PaymentID: paymentID, Key: key, Body: body}, nil
}
func (c *YooKassaClient) CreateSettlementReceipt(ctx context.Context, request SettlementReceiptRequest) (FiscalReceipt, error) {
	if request.PaymentID == "" || request.Key == "" || request.Body == nil {
		return FiscalReceipt{}, errors.New("некорректный запрос чека зачёта")
	}
	return c.receiptResponse(ctx, http.MethodPost, "/receipts", request.Key, request.Body)
}
func (c *YooKassaClient) FetchReceipt(ctx context.Context, id string) (FiscalReceipt, error) {
	if id == "" {
		return FiscalReceipt{}, errors.New("не указан чек")
	}
	return c.receiptResponse(ctx, http.MethodGet, "/receipts/"+url.PathEscape(id), "", nil)
}
func (c *YooKassaClient) receiptResponse(ctx context.Context, method, path, key string, body any) (FiscalReceipt, error) {
	var receipt FiscalReceipt
	if err := c.send(ctx, method, path, key, body, &receipt); err != nil {
		return receipt, err
	}
	if receipt.ID == "" || receipt.PaymentID == "" || receipt.Type != "payment" || (receipt.Status != "pending" && receipt.Status != "succeeded" && receipt.Status != "canceled") {
		return FiscalReceipt{}, fmt.Errorf("%w: некорректный ответ чека", ErrPaymentOutcomeUnknown)
	}
	return receipt, nil
}
