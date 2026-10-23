package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/avpavlo8/ficusin-store/backend/internal/money"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const yooKassaBaseURL = "https://api.yookassa.ru/v3"

// ErrPaymentOutcomeUnknown means the request may have reached YooKassa even
// though its result did not reach us. Callers must retry only with the same
// idempotence key.
var ErrPaymentOutcomeUnknown = errors.New("результат операции ЮKassa неизвестен")

// YooKassaClient takes card payments. Like every other integration here it
// is switched off by an empty key rather than by a flag: a shop with no
// credentials simply does not offer online payment.
type YooKassaClient struct {
	shopID     string
	secretKey  string
	httpClient *http.Client
	// sendReceipt is on when YooKassa is the one printing the fiscal
	// receipt. If the shop punches receipts through its own till, sending
	// the composition here would produce a second receipt for one sale.
	sendReceipt bool
	taxSystem   int
	vatCode     int
}

func NewYooKassaClient(shopID, secretKey string, sendReceipt bool, taxSystem, vatCode int) *YooKassaClient {
	return &YooKassaClient{
		shopID:      strings.TrimSpace(shopID),
		secretKey:   strings.TrimSpace(secretKey),
		httpClient:  &http.Client{Timeout: 20 * time.Second},
		sendReceipt: sendReceipt,
		taxSystem:   taxSystem,
		vatCode:     max(1, vatCode),
	}
}

func (client *YooKassaClient) Configured() bool {
	return client != nil && client.shopID != "" && client.secretKey != ""
}

// PaymentItem is one line of the fiscal receipt.
type PaymentItem struct {
	Name     string
	Price    float64
	Quantity int
}

type PaymentRequest struct {
	// IdempotenceKey makes a repeated request return the original payment
	// instead of charging twice. It must be stable for one order attempt.
	IdempotenceKey string
	Amount         float64
	Description    string
	ReturnURL      string
	// Email and Phone go on the receipt only. They never leave for anywhere
	// else, and nothing is sent at all when the shop prints its own.
	Email string
	Phone string
	Items []PaymentItem
	// Metadata ties an otherwise ambiguous provider object back to the exact
	// durable attempt in our database. It contains no customer data.
	Metadata map[string]string
}

type Payment struct {
	ID              string
	Status          string
	Paid            bool
	Amount          float64
	ConfirmationURL string
	Description     string
	Metadata        map[string]string
}

// CreatePayment starts a two-stage payment and returns the page to send the
// customer to. The customer's money is held until CapturePayment succeeds.
func (client *YooKassaClient) CreatePayment(
	ctx context.Context,
	request PaymentRequest,
) (Payment, error) {
	if !client.Configured() {
		return Payment{}, errors.New("оплата не настроена: YOOKASSA_SHOP_ID и YOOKASSA_SECRET_KEY")
	}
	amountMinor, amountErr := money.Exact(request.Amount)
	if amountErr != nil || amountMinor <= 0 {
		return Payment{}, errors.New("сумма к оплате должна быть больше нуля")
	}
	body := map[string]any{
		"amount": map[string]string{
			"value":    rubles(request.Amount),
			"currency": "RUB",
		},
		"capture":     false,
		"description": truncateRunes(request.Description, 128),
		"confirmation": map[string]string{
			"type":       "redirect",
			"return_url": request.ReturnURL,
		},
	}
	if client.sendReceipt {
		var receiptMinor int64
		for _, item := range request.Items {
			price, err := money.Exact(item.Price)
			if err != nil || price < 0 || item.Quantity <= 0 || price > money.MaxMinor/int64(item.Quantity) || receiptMinor > money.MaxMinor-price*int64(item.Quantity) {
				return Payment{}, errors.New("некорректная сумма позиции чека")
			}
			receiptMinor += price * int64(item.Quantity)
		}
		if len(request.Items) == 0 || receiptMinor != amountMinor {
			return Payment{}, errors.New("сумма чека не совпадает с платежом")
		}
		body["receipt"] = client.receipt(request)
	}
	if len(request.Metadata) > 0 {
		body["metadata"] = request.Metadata
	}
	var response yooKassaPayment
	if err := client.send(
		ctx,
		http.MethodPost,
		"/payments",
		request.IdempotenceKey,
		body,
		&response,
	); err != nil {
		return Payment{}, err
	}
	return response.toPayment()
}

// FetchPayment asks YooKassa what really happened. Notifications arrive
// unsigned over the open internet, so the answer to "is this paid" is only
// ever taken from here, never from the body of the notification.
func (client *YooKassaClient) FetchPayment(ctx context.Context, id string) (Payment, error) {
	if !client.Configured() {
		return Payment{}, errors.New("оплата не настроена")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Payment{}, errors.New("не указан платёж")
	}
	var response yooKassaPayment
	if err := client.send(ctx, http.MethodGet, "/payments/"+id, "", nil, &response); err != nil {
		return Payment{}, err
	}
	return response.toPayment()
}

// CapturePayment confirms a customer's hold after the shop has checked the
// current stock. A stable idempotence key is mandatory because a lost response
// does not prove that the capture failed. The caller must fetch the payment to
// verify the final succeeded status, including after an ambiguous outcome.
func (client *YooKassaClient) CapturePayment(ctx context.Context, paymentID, idempotenceKey string) error {
	if !client.Configured() {
		return errors.New("оплата не настроена")
	}
	paymentID = strings.TrimSpace(paymentID)
	if paymentID == "" {
		return errors.New("не указан платёж")
	}
	if strings.TrimSpace(idempotenceKey) == "" {
		return errors.New("не указан ключ идемпотентности подтверждения")
	}
	var response yooKassaPayment
	if err := client.send(ctx, http.MethodPost, "/payments/"+paymentID+"/capture", idempotenceKey, nil, &response); err != nil {
		return err
	}
	if response.Status == "canceled" {
		return errors.New("ЮKassa отменила платёж")
	}
	if response.Status != "succeeded" && response.Status != "pending" && response.Status != "waiting_for_capture" {
		return fmt.Errorf("неожиданный статус подтверждения ЮKassa: %q", response.Status)
	}
	return nil
}

func (client *YooKassaClient) receipt(request PaymentRequest) map[string]any {
	items := make([]map[string]any, 0, len(request.Items))
	for _, item := range request.Items {
		items = append(items, map[string]any{
			"description": truncateRunes(item.Name, 128),
			"quantity":    strconv.Itoa(max(1, item.Quantity)),
			"amount": map[string]string{
				"value":    rubles(item.Price),
				"currency": "RUB",
			},
			"vat_code":        client.vatCode,
			"payment_mode":    "full_prepayment",
			"payment_subject": "commodity",
		})
	}
	customer := map[string]string{}
	if request.Email != "" {
		customer["email"] = request.Email
	}
	if request.Phone != "" {
		customer["phone"] = request.Phone
	}
	receipt := map[string]any{"customer": customer, "items": items}
	if client.taxSystem > 0 {
		receipt["tax_system_code"] = client.taxSystem
	}
	return receipt
}

func (client *YooKassaClient) send(
	ctx context.Context,
	method, path, idempotenceKey string,
	body any,
	destination any,
) error {
	var payload *bytes.Reader
	if body == nil {
		payload = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, yooKassaBaseURL+path, payload)
	if err != nil {
		return err
	}
	request.SetBasicAuth(client.shopID, client.secretKey)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	if idempotenceKey != "" {
		request.Header.Set("Idempotence-Key", idempotenceKey)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("%w: оплата временно недоступна: %v", ErrPaymentOutcomeUnknown, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Description string `json:"description"`
			Parameter   string `json:"parameter"`
		}
		_ = json.NewDecoder(response.Body).Decode(&failure)
		if response.StatusCode >= http.StatusInternalServerError {
			return fmt.Errorf("%w: ЮKassa временно недоступна (%d)", ErrPaymentOutcomeUnknown, response.StatusCode)
		}
		if failure.Description != "" {
			return fmt.Errorf("ЮKassa отказала: %s", failure.Description)
		}
		return fmt.Errorf("оплата временно недоступна (%d)", response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
		return fmt.Errorf("%w: ответ ЮKassa не разобран: %v", ErrPaymentOutcomeUnknown, err)
	}
	return nil
}

type yooKassaPayment struct {
	ID          string            `json:"id"`
	Status      string            `json:"status"`
	Paid        bool              `json:"paid"`
	Description string            `json:"description"`
	Metadata    map[string]string `json:"metadata"`
	Amount      struct {
		Value    string `json:"value"`
		Currency string `json:"currency"`
	} `json:"amount"`
	Confirmation struct {
		ConfirmationURL string `json:"confirmation_url"`
	} `json:"confirmation"`
}

func (payment yooKassaPayment) toPayment() (Payment, error) {
	amount, err := money.Parse(payment.Amount.Value)
	if err != nil || amount <= 0 || payment.Amount.Currency != "RUB" {
		return Payment{}, fmt.Errorf("%w: некорректная сумма платежа", ErrPaymentOutcomeUnknown)
	}
	return Payment{
		ID:              payment.ID,
		Status:          payment.Status,
		Paid:            payment.Paid,
		Amount:          money.Rubles(amount),
		ConfirmationURL: payment.Confirmation.ConfirmationURL,
		Description:     payment.Description,
		Metadata:        payment.Metadata,
	}, nil
}

// rubles formats an amount the way YooKassa insists on seeing it: two
// decimal places, a dot, no spaces.
func rubles(amount float64) string {
	return money.Format(money.Kopecks(amount))
}

// RefundRequest is persisted before the request leaves the shop.
type RefundRequest struct {
	PaymentID      string
	Amount         float64
	IdempotenceKey string
	Receipt        map[string]any `json:",omitempty"`
}
type Refund struct {
	ID        string  `json:"id"`
	Status    string  `json:"status"`
	PaymentID string  `json:"payment_id"`
	Amount    float64 `json:"-"`
}

func (client *YooKassaClient) RefundUsesReceipt() bool { return client.sendReceipt }
func (client *YooKassaClient) PrepareRefund(paymentID string, amount float64, key string, original PaymentRequest) (RefundRequest, error) {
	amountMinor, err := money.Exact(amount)
	if err != nil || amountMinor <= 0 {
		return RefundRequest{}, errors.New("некорректная сумма возврата")
	}
	req := RefundRequest{PaymentID: paymentID, Amount: amount, IdempotenceKey: key}
	if client.sendReceipt {
		var total int64
		for _, item := range original.Items {
			price, err := money.Exact(item.Price)
			if err != nil || price < 0 || item.Quantity <= 0 || price > money.MaxMinor/int64(item.Quantity) || total > money.MaxMinor-price*int64(item.Quantity) {
				return req, errors.New("некорректные позиции исходного платежа")
			}
			total += price * int64(item.Quantity)
		}
		// A partial refund needs its real item composition, never a fabricated line.
		if len(original.Items) == 0 || total != amountMinor {
			return req, errors.New("для частичного возврата нужен состав чека; выполните возврат через кассу с проверкой позиций")
		}
		req.Receipt = client.receipt(original)
	}
	return req, nil
}
func (client *YooKassaClient) CreateRefund(ctx context.Context, request RefundRequest) (Refund, error) {
	amountMinor, err := money.Exact(request.Amount)
	if !client.Configured() || request.PaymentID == "" || err != nil || amountMinor <= 0 || request.IdempotenceKey == "" {
		return Refund{}, errors.New("некорректный запрос возврата")
	}
	body := map[string]any{"payment_id": request.PaymentID, "amount": map[string]string{"value": rubles(request.Amount), "currency": "RUB"}}
	if request.Receipt != nil {
		body["receipt"] = request.Receipt
	}
	return client.refundResponse(ctx, http.MethodPost, "/refunds", request.IdempotenceKey, body)
}
func (client *YooKassaClient) FetchRefund(ctx context.Context, id string) (Refund, error) {
	if id == "" {
		return Refund{}, errors.New("не указан возврат")
	}
	return client.refundResponse(ctx, http.MethodGet, "/refunds/"+url.PathEscape(id), "", nil)
}
func (client *YooKassaClient) refundResponse(ctx context.Context, method, path, key string, body any) (Refund, error) {
	var response struct {
		ID        string `json:"id"`
		Status    string `json:"status"`
		PaymentID string `json:"payment_id"`
		Amount    struct {
			Value    string `json:"value"`
			Currency string `json:"currency"`
		} `json:"amount"`
	}
	if err := client.send(ctx, method, path, key, body, &response); err != nil {
		return Refund{}, err
	}
	amount, err := money.Parse(response.Amount.Value)
	if err != nil || amount <= 0 || response.ID == "" || response.PaymentID == "" || response.Amount.Currency != "RUB" || (response.Status != "pending" && response.Status != "succeeded" && response.Status != "canceled") {
		return Refund{}, fmt.Errorf("%w: некорректный ответ возврата", ErrPaymentOutcomeUnknown)
	}
	return Refund{ID: response.ID, Status: response.Status, PaymentID: response.PaymentID, Amount: money.Rubles(amount)}, nil
}

// Kept for compatibility with callers outside the order workflow. Pending is
// explicitly not success; order refunds use CreateRefund/FetchRefund instead.
func (client *YooKassaClient) Refund(ctx context.Context, paymentID string, amount float64, key string) error {
	req, err := client.PrepareRefund(paymentID, amount, key, PaymentRequest{})
	if err != nil {
		return err
	}
	result, err := client.CreateRefund(ctx, req)
	if err != nil {
		return err
	}
	if result.Status != "succeeded" {
		return errors.New("возврат ещё не подтверждён")
	}
	return nil
}
