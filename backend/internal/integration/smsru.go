package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// SMSRUClient authorizes users via SMS.ru's "call from the user" flow
// (https://sms.ru/api/call): we ask SMS.ru for a phone number, the user
// calls it from their own phone (the call is free and gets dropped
// automatically), and we poll SMS.ru to see whether that call came in.
//
// This replaces the older "we call the user" flow
// (https://sms.ru/api/code_call), which SMS.ru's own documentation now
// marks as deprecated ("Данный функционал устарел и скоро будет выведен
// из обращения") and which, in practice, stopped reliably placing calls
// even though it kept returning status "OK" and charging the balance.
//
// If no API key is configured, RequestCallCheck fabricates a check
// instead of calling SMS.ru, and CallCheckStatus reports it confirmed on
// the first poll — convenient for local development.
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

// RequestCallCheck registers phone with SMS.ru and returns the number the
// user must call from their own phone to authenticate (both the raw
// digits and a human-formatted version), plus a check_id used to poll the
// call's status afterwards. The user has 5 minutes to place the call.
func (client *SMSRUClient) RequestCallCheck(
	ctx context.Context,
	phone string,
) (checkID, callPhone, callPhonePretty string, err error) {
	if client.apiKey == "" {
		fmt.Printf("[dev] call-check for %s: pretend to call +7 (800) 000-00-00\n", phone)
		return "dev-check", "78000000000", "+7 (800) 000-00-00", nil
	}

	values := url.Values{}
	values.Set("api_id", client.apiKey)
	values.Set("phone", phone)
	values.Set("json", "1")

	var result struct {
		Status          string `json:"status"`
		StatusCode      int    `json:"status_code"`
		StatusText      string `json:"status_text"`
		CheckID         string `json:"check_id"`
		CallPhone       string `json:"call_phone"`
		CallPhonePretty string `json:"call_phone_pretty"`
	}
	if err := client.get(ctx, "https://sms.ru/callcheck/add", values, &result); err != nil {
		return "", "", "", err
	}
	if result.Status != "OK" {
		return "", "", "", fmt.Errorf("SMS.ru error: %s (status_code=%d)", result.StatusText, result.StatusCode)
	}
	if result.CheckID == "" || result.CallPhone == "" {
		return "", "", "", fmt.Errorf("SMS.ru callcheck/add response missing check_id/call_phone")
	}
	return result.CheckID, result.CallPhone, result.CallPhonePretty, nil
}

// CallCheckStatus reports whether the user has already called the number
// we gave them for checkID. confirmed becomes true once the call has come
// in. expired becomes true once the 5-minute window has elapsed (or the
// checkID is otherwise no longer valid), at which point the caller should
// request a new number instead of continuing to poll this one.
func (client *SMSRUClient) CallCheckStatus(ctx context.Context, checkID string) (confirmed, expired bool, err error) {
	if client.apiKey == "" {
		return true, false, nil
	}

	values := url.Values{}
	values.Set("api_id", client.apiKey)
	values.Set("check_id", checkID)
	values.Set("json", "1")

	var result struct {
		Status      string `json:"status"`
		StatusCode  int    `json:"status_code"`
		StatusText  string `json:"status_text"`
		CheckStatus string `json:"check_status"`
	}
	if err := client.get(ctx, "https://sms.ru/callcheck/status", values, &result); err != nil {
		return false, false, err
	}
	if result.Status != "OK" {
		return false, false, fmt.Errorf("SMS.ru error: %s (status_code=%d)", result.StatusText, result.StatusCode)
	}
	switch result.CheckStatus {
	case "401":
		return true, false, nil
	case "400":
		return false, false, nil
	case "402":
		return false, true, nil
	default:
		return false, false, fmt.Errorf("unexpected SMS.ru check_status: %s", result.CheckStatus)
	}
}

func (client *SMSRUClient) get(ctx context.Context, endpoint string, values url.Values, out any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+values.Encode(), nil)
	if err != nil {
		return err
	}

	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("request %s: %w", endpoint, err)
	}
	defer response.Body.Close()

	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return fmt.Errorf("decode SMS.ru response: %w", err)
	}
	return nil
}
