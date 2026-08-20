package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const russianPostAPIBaseURL = "https://otpravka-api.pochta.ru"

var ErrRussianPostAddress = errors.New("russian post address is not recognized")

// DeliveryQuote is the provider-neutral price shown at checkout. Price is in
// roubles, while delivery times are whole calendar days when the provider
// supplies them.
type DeliveryQuote struct {
	Price    float64 `json:"price"`
	DaysMin  int     `json:"daysMin,omitempty"`
	DaysMax  int     `json:"daysMax,omitempty"`
	Service  string  `json:"service,omitempty"`
}

// RussianPostClient uses the contract API "Отправка". The application token
// and user authorization key are deliberately kept in the environment: they
// must never be persisted in shop settings or sent to the browser.
type RussianPostClient struct {
	accessToken string
	userAuthKey string
	fromIndex   string
	baseURL     string
	httpClient  *http.Client
}

func NewRussianPostClient(accessToken, userAuthKey, fromIndex string) *RussianPostClient {
	return &RussianPostClient{
		accessToken: strings.TrimSpace(accessToken),
		userAuthKey: strings.TrimSpace(userAuthKey),
		fromIndex:   strings.TrimSpace(fromIndex),
		baseURL:     russianPostAPIBaseURL,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (client *RussianPostClient) Configured() bool {
	return client != nil && client.accessToken != "" && client.userAuthKey != "" && len(client.fromIndex) == 6
}

type russianPostCleanAddress struct {
	Index       string `json:"index"`
	QualityCode string `json:"quality-code"`
	RawAddress  string `json:"raw-address"`
}

type russianPostTariffResponse struct {
	TotalRate int64 `json:"total-rate"`
	TotalVAT  int64 `json:"total-vat"`
	Delivery  struct {
		MinDays int `json:"min-days"`
		MaxDays int `json:"max-days"`
	} `json:"delivery-time"`
}

// Calculate quotes a normal domestic parcel to the customer's address. The
// address is normalized by Russian Post first, so a typo cannot silently turn
// into a quote for another postcode. Dimensions are sent in millimetres as
// required by the Отправка API; our catalog stores centimetres.
func (client *RussianPostClient) Calculate(ctx context.Context, address string, parcel Parcel) (DeliveryQuote, error) {
	if !client.Configured() {
		return DeliveryQuote{}, errors.New("russian post is not configured")
	}
	if !parcel.Measured() {
		return DeliveryQuote{}, errors.New("parcel dimensions are missing")
	}
	address = strings.TrimSpace(address)
	if address == "" {
		return DeliveryQuote{}, ErrRussianPostAddress
	}

	cleaned, err := client.normalizeAddress(ctx, address)
	if err != nil {
		return DeliveryQuote{}, err
	}
	if len(cleaned.Index) != 6 || cleaned.QualityCode == "UNDEF_05" {
		return DeliveryQuote{}, ErrRussianPostAddress
	}

	requestBody := map[string]any{
		"index-from":     client.fromIndex,
		"index-to":       cleaned.Index,
		"mail-category":  "ORDINARY",
		"mail-type":      "POSTAL_PARCEL",
		"mass":           parcel.WeightGrams,
		"payment-method": "CASHLESS",
		"dimension": map[string]int{
			"length": parcel.LengthCM * 10,
			"width":  parcel.WidthCM * 10,
			"height": parcel.HeightCM * 10,
		},
	}
	var tariff russianPostTariffResponse
	if err := client.doJSON(ctx, http.MethodPost, "/1.0/tariff", requestBody, &tariff); err != nil {
		return DeliveryQuote{}, err
	}
	if tariff.TotalRate <= 0 {
		return DeliveryQuote{}, errors.New("russian post returned an empty tariff")
	}
	return DeliveryQuote{
		// Отправка returns the contract rate and VAT separately. The customer
		// must see the final amount that will actually be charged.
		Price:   float64(tariff.TotalRate+tariff.TotalVAT) / 100,
		DaysMin: tariff.Delivery.MinDays,
		DaysMax: tariff.Delivery.MaxDays,
		Service: "Почта России",
	}, nil
}

func (client *RussianPostClient) normalizeAddress(ctx context.Context, address string) (russianPostCleanAddress, error) {
	body := []map[string]string{{"id": "checkout", "original-address": address}}
	var response []russianPostCleanAddress
	if err := client.doJSON(ctx, http.MethodPost, "/1.0/clean/address", body, &response); err != nil {
		return russianPostCleanAddress{}, err
	}
	if len(response) == 0 {
		return russianPostCleanAddress{}, ErrRussianPostAddress
	}
	return response[0], nil
}

func (client *RussianPostClient) doJSON(ctx context.Context, method, path string, input, output any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("encode russian post request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(client.baseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build russian post request: %w", err)
	}
	request.Header.Set("Authorization", "AccessToken "+client.accessToken)
	request.Header.Set("X-User-Authorization", "Basic "+client.userAuthKey)
	request.Header.Set("Content-Type", "application/json;charset=UTF-8")
	request.Header.Set("Accept", "application/json")

	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("russian post request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("russian post returned HTTP %d", response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(output); err != nil {
		return fmt.Errorf("decode russian post response: %w", err)
	}
	return nil
}
