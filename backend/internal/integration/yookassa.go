package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const yooKassaBaseURL = "https://api.yookassa.ru/v3"

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
}

type Payment struct {
	ID              string
	Status          string
	Paid            bool
	Amount          float64
	ConfirmationURL string
}

// CreatePayment starts a payment and returns the page to send the customer
// to. Nothing is captured until the customer actually pays.
func (client *YooKassaClient) CreatePayment(
	ctx context.Context,
	request PaymentRequest,
) (Payment, error) {
	if !client.Configured() {
		return Payment{}, errors.New("оплата не настроена: YOOKASSA_SHOP_ID и YOOKASSA_SECRET_KEY")
	}
	if request.Amount <= 0 {
		return Payment{}, errors.New("сумма к оплате должна быть больше нуля")
	}
	body := map[string]any{
		"amount": map[string]string{
			"value":    rubles(request.Amount),
			"currency": "RUB",
		},
		// capture: true takes the money in one step. A two-step hold would
		// need someone to confirm every payment by hand.
		"capture":     true,
		"description": truncateRunes(request.Description, 128),
		"confirmation": map[string]string{
			"type":       "redirect",
			"return_url": request.ReturnURL,
		},
	}
	if client.sendReceipt {
		body["receipt"] = client.receipt(request)
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
	return response.toPayment(), nil
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
	return response.toPayment(), nil
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
		return fmt.Errorf("оплата временно недоступна: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Description string `json:"description"`
			Parameter   string `json:"parameter"`
		}
		_ = json.NewDecoder(response.Body).Decode(&failure)
		if failure.Description != "" {
			return fmt.Errorf("ЮKassa отказала: %s", failure.Description)
		}
		return fmt.Errorf("оплата временно недоступна (%d)", response.StatusCode)
	}
	return json.NewDecoder(response.Body).Decode(destination)
}

type yooKassaPayment struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Paid   bool   `json:"paid"`
	Amount struct {
		Value string `json:"value"`
	} `json:"amount"`
	Confirmation struct {
		ConfirmationURL string `json:"confirmation_url"`
	} `json:"confirmation"`
}

func (payment yooKassaPayment) toPayment() Payment {
	amount, _ := strconv.ParseFloat(payment.Amount.Value, 64)
	return Payment{
		ID:              payment.ID,
		Status:          payment.Status,
		Paid:            payment.Paid,
		Amount:          amount,
		ConfirmationURL: payment.Confirmation.ConfirmationURL,
	}
}

// rubles formats an amount the way YooKassa insists on seeing it: two
// decimal places, a dot, no spaces.
func rubles(amount float64) string {
	return strconv.FormatFloat(amount, 'f', 2, 64)
}

// Refund sends the money back. YooKassa refuses to refund more than was
// paid, but we check too: a refund larger than the payment would be a bug
// that costs real money.
func (client *YooKassaClient) Refund(
	ctx context.Context,
	paymentID string,
	amount float64,
	idempotenceKey string,
) error {
	if !client.Configured() {
		return errors.New("оплата не настроена")
	}
	if strings.TrimSpace(paymentID) == "" || amount <= 0 {
		return errors.New("нечего возвращать")
	}
	body := map[string]any{
		"payment_id": paymentID,
		"amount": map[string]string{
			"value":    rubles(amount),
			"currency": "RUB",
		},
	}
	var response struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := client.send(ctx, http.MethodPost, "/refunds", idempotenceKey, body, &response); err != nil {
		return err
	}
	if response.Status == "canceled" {
		return errors.New("ЮKassa отклонила возврат")
	}
	return nil
}
