package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
)

const (
	defaultWBBase   = "https://discounts-prices-api.wildberries.ru"
	defaultOzonBase = "https://api-seller.ozon.ru"
)

type MarketplaceExecutor struct {
	client       *http.Client
	wbToken      string
	ozonClientID string
	ozonAPIKey   string
	wbBase       string
	ozonBase     string
}

func NewMarketplaceExecutor(wbToken, ozonClientID, ozonAPIKey string) *MarketplaceExecutor {
	return &MarketplaceExecutor{
		client: &http.Client{Timeout: 20 * time.Second}, wbToken: strings.TrimSpace(wbToken),
		ozonClientID: strings.TrimSpace(ozonClientID), ozonAPIKey: strings.TrimSpace(ozonAPIKey),
		wbBase: defaultWBBase, ozonBase: defaultOzonBase,
	}
}

func (executor *MarketplaceExecutor) Configured(channel string) bool {
	switch channel {
	case "wb":
		return executor != nil && executor.wbToken != ""
	case "ozon":
		return executor != nil && executor.ozonClientID != "" && executor.ozonAPIKey != ""
	default:
		return false
	}
}

func (executor *MarketplaceExecutor) Execute(ctx context.Context, item procurement.ActionItem) (procurement.ActionExecution, error) {
	switch item.Channel {
	case "wb":
		return executor.executeWB(ctx, item)
	case "ozon":
		return executor.executeOzon(ctx, item)
	default:
		return procurement.ActionExecution{}, fmt.Errorf("канал %s не поддерживается", item.Channel)
	}
}

func (executor *MarketplaceExecutor) executeWB(ctx context.Context, item procurement.ActionItem) (procurement.ActionExecution, error) {
	if !executor.Configured("wb") {
		return procurement.ActionExecution{}, errors.New("токен Wildberries не настроен")
	}
	nmID, err := strconv.ParseInt(strings.TrimSpace(item.ExternalArticle), 10, 64)
	if err != nil || nmID <= 0 {
		return procurement.ActionExecution{}, errors.New("для Wildberries нужен числовой nmID, а не артикул продавца")
	}
	if item.ExternalOperationID == "" {
		price := int64(item.NewValue)
		discount := int64(0)
		if item.CompareAtValue != nil && *item.CompareAtValue > item.NewValue {
			price = int64(*item.CompareAtValue)
			discount = int64((1 - item.NewValue/float64(price)) * 100)
		}
		payload := map[string]any{"data": []map[string]any{{"nmID": nmID, "price": price, "discount": discount}}}
		var response struct {
			Data struct {
				ID int64 `json:"id"`
			} `json:"data"`
			Error     bool   `json:"error"`
			ErrorText string `json:"errorText"`
		}
		if err := executor.request(ctx, http.MethodPost, executor.wbBase+"/api/v2/upload/task", payload,
			map[string]string{"Authorization": executor.wbToken}, &response); err != nil {
			return procurement.ActionExecution{}, err
		}
		if response.Error || response.Data.ID <= 0 {
			return procurement.ActionExecution{}, fmt.Errorf("Wildberries отклонил загрузку: %s", safeRemoteMessage(response.ErrorText))
		}
		return procurement.ActionExecution{ExternalOperationID: strconv.FormatInt(response.Data.ID, 10), RetryAfter: 5 * time.Second}, nil
	}

	uploadID, err := strconv.ParseInt(item.ExternalOperationID, 10, 64)
	if err != nil || uploadID <= 0 {
		return procurement.ActionExecution{}, errors.New("повреждён идентификатор загрузки Wildberries")
	}
	endpoint := executor.wbBase + "/api/v2/history/tasks?" + url.Values{"uploadID": {strconv.FormatInt(uploadID, 10)}}.Encode()
	var response struct {
		Data struct {
			Status int `json:"status"`
		} `json:"data"`
		Error     bool   `json:"error"`
		ErrorText string `json:"errorText"`
	}
	err = executor.request(ctx, http.MethodGet, endpoint, nil, map[string]string{"Authorization": executor.wbToken}, &response)
	if err != nil {
		var remote *remoteError
		if errors.As(err, &remote) && remote.Status == http.StatusNotFound {
			return procurement.ActionExecution{ExternalOperationID: item.ExternalOperationID, RetryAfter: 5 * time.Second}, nil
		}
		return procurement.ActionExecution{ExternalOperationID: item.ExternalOperationID}, err
	}
	if response.Error {
		return procurement.ActionExecution{ExternalOperationID: item.ExternalOperationID}, fmt.Errorf("Wildberries не подтвердил загрузку: %s", safeRemoteMessage(response.ErrorText))
	}
	switch response.Data.Status {
	case 3:
		return procurement.ActionExecution{Completed: true, ExternalOperationID: item.ExternalOperationID}, nil
	case 4, 5, 6:
		return procurement.ActionExecution{ExternalOperationID: item.ExternalOperationID}, fmt.Errorf("Wildberries завершил загрузку со статусом %d", response.Data.Status)
	default:
		return procurement.ActionExecution{ExternalOperationID: item.ExternalOperationID, RetryAfter: 5 * time.Second}, nil
	}
}

func (executor *MarketplaceExecutor) executeOzon(ctx context.Context, item procurement.ActionItem) (procurement.ActionExecution, error) {
	if !executor.Configured("ozon") {
		return procurement.ActionExecution{}, errors.New("ключи Ozon не настроены")
	}
	price := strconv.FormatInt(int64(item.NewValue), 10)
	oldPrice := "0"
	if item.CompareAtValue != nil && *item.CompareAtValue > item.NewValue {
		oldPrice = strconv.FormatInt(int64(*item.CompareAtValue), 10)
	}
	payload := map[string]any{"prices": []map[string]any{{
		"offer_id": strings.TrimSpace(item.ExternalArticle), "price": price,
		"old_price": oldPrice, "min_price": price, "currency_code": "RUB", "auto_action_enabled": "UNKNOWN",
	}}}
	var response struct {
		Result []struct {
			OfferID string `json:"offer_id"`
			Updated bool   `json:"updated"`
			Errors  []struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"errors"`
		} `json:"result"`
	}
	err := executor.request(ctx, http.MethodPost, executor.ozonBase+"/v1/product/import/prices", payload,
		map[string]string{"Client-Id": executor.ozonClientID, "Api-Key": executor.ozonAPIKey}, &response)
	if err != nil {
		return procurement.ActionExecution{}, err
	}
	if len(response.Result) != 1 || !response.Result[0].Updated {
		message := "Ozon не подтвердил изменение цены"
		if len(response.Result) == 1 && len(response.Result[0].Errors) > 0 {
			message += ": " + safeRemoteMessage(response.Result[0].Errors[0].Message)
		}
		return procurement.ActionExecution{}, errors.New(message)
	}
	return procurement.ActionExecution{Completed: true}, nil
}

type remoteError struct {
	Status  int
	Message string
}

func (err *remoteError) Error() string {
	return fmt.Sprintf("внешний API ответил %d: %s", err.Status, err.Message)
}

func (executor *MarketplaceExecutor) request(ctx context.Context, method, endpoint string, payload any, headers map[string]string, target any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode marketplace request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return fmt.Errorf("create marketplace request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := executor.client.Do(request)
	if err != nil {
		return fmt.Errorf("marketplace request failed: %w", err)
	}
	defer response.Body.Close() //nolint:errcheck
	content, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return fmt.Errorf("read marketplace response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &remoteError{Status: response.StatusCode, Message: safeRemoteMessage(string(content))}
	}
	if err := json.Unmarshal(content, target); err != nil {
		return fmt.Errorf("decode marketplace response: %w", err)
	}
	return nil
}

func safeRemoteMessage(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\n", " "))
	if value == "" {
		return "без описания"
	}
	runes := []rune(value)
	if len(runes) > 300 {
		value = string(runes[:300]) + "…"
	}
	return value
}
