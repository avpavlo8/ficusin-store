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

// SMSRUClient sends OTP login codes over SMS using the SMS.ru gateway
// (https://sms.ru/api). If no API key is configured, Send only logs the
// code instead of making a network call, which is convenient for local
// development. It satisfies the auth.CodeSender interface.
type SMSRUClient struct {
  	apiKey     string
  	sender     string
  	httpClient *http.Client
  }

func NewSMSRUClient(apiKey, sender string) *SMSRUClient {
  	return &SMSRUClient{
      		apiKey:     apiKey,
      		sender:     sender,
      		httpClient: &http.Client{Timeout: 15 * time.Second},
      	}
  }

func (client *SMSRUClient) Send(ctx context.Context, phone, code string) error {
  	message := fmt.Sprintf("Код для входа в Фикусин: %s", code)
  	if client.apiKey == "" {
      		fmt.Printf("[dev] OTP code for %s: %s\n", phone, code)
      		return nil
      	}

  	values := url.Values{}
  	values.Set("api_id", client.apiKey)
  	values.Set("to", phone)
  	values.Set("msg", message)
  	values.Set("json", "1")
  	if client.sender != "" {
      		values.Set("from", client.sender)
      	}

  	request, err := http.NewRequestWithContext(
      		ctx,
      		http.MethodPost,
      		"https://sms.ru/sms/send",
      		strings.NewReader(values.Encode()),
      	)
  	if err != nil {
      		return err
      	}
  	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

  	response, err := client.httpClient.Do(request)
  	if err != nil {
      		return fmt.Errorf("send SMS: %w", err)
      	}
  	defer response.Body.Close()

  	var result struct {
      		Status     string `json:"status"`
      		StatusCode int    `json:"status_code"`
      		Sms        map[string]struct {
            			Status     string `json:"status"`
            			StatusCode int    `json:"status_code"`
            			StatusText string `json:"status_text"`
            		} `json:"sms"`
      	}
  	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
      		return fmt.Errorf("decode SMS.ru response: %w", err)
      	}
  	if result.Status != "OK" {
      		return fmt.Errorf("SMS.ru error: status_code=%d", result.StatusCode)
      	}
  	for _, item := range result.Sms {
      		if item.Status != "OK" {
            			return fmt.Errorf("SMS.ru delivery error: %s", item.StatusText)
            		}
      	}
  	return nil
  }
