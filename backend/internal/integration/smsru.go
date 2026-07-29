package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SMSRUClient authorizes users via SMS.ru's call-based OTP ("Отправить
// четырехзначный авторизационный код звонком", https://sms.ru/api/code_call):
// SMS.ru calls the phone from a random number and the last four digits of
// that number are the code. This avoids registering (and paying a monthly
// fee for) an alphanumeric sender name with every mobile operator, which
// SMS.ru now requires for ordinary text messages.
//
// If no API key is configured, RequestCall only logs a fake code instead of
// making a network call, which is convenient for local development. It
// satisfies the auth.CodeSender interface.
type SMSRUClient struct {
	apiKey     string
	httpClient *http.Client
}

func NewSMSRUClient(apiKey string) *SMSRUClient {
	return &SMSRUClient{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// RequestCall asks SMS.ru to call phone and returns the four-digit code the
// user must read back to us (the last four digits of the calling number).
// ip should be the end user's IP address, not the server's — it lets
// SMS.ru rate-limit abusive callers; pass "-1" if it isn't available.
func (client *SMSRUClient) RequestCall(ctx context.Context, phone, ip string) (string, error) {
	if client.apiKey == "" {
		fmt.Printf("[dev] OTP call-code for %s: 0000\n", phone)
		return "0000", nil
	}

	if ip == "" {
		ip = "-1"
	}

	values := url.Values{}
	values.Set("api_id", client.apiKey)
	values.Set("phone", phone)
	values.Set("ip", ip)
	values.Set("json", "1")

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://sms.ru/code/call",
		strings.NewReader(values.Encode()),
	)
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := client.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("request call: %w", err)
	}
	defer response.Body.Close()

	var result struct {
		Status     string `json:"status"`
		StatusCode int    `json:"status_code"`
		StatusText string `json:"status_text"`
		Code       string `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode SMS.ru response: %w", err)
	}
	if result.Status != "OK" {
		return "", fmt.Errorf("SMS.ru error: %s (status_code=%d)", result.StatusText, result.StatusCode)
	}
	if result.Code == "" {
		return "", fmt.Errorf("SMS.ru call response missing code")
	}
	return result.Code, nil
}
