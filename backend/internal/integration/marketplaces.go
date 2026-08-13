package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
)

const (
	defaultWBBase    = "https://discounts-prices-api.wildberries.ru"
	defaultWBStats   = "https://finance-api.wildberries.ru"
	defaultWBReports = "https://statistics-api.wildberries.ru"
	defaultWBContent = "https://content-api.wildberries.ru"
	defaultOzonBase  = "https://api-seller.ozon.ru"
)

type MarketplaceExecutor struct {
	client       *http.Client
	wbToken      string
	ozonClientID string
	ozonAPIKey   string
	wbBase        string
	wbStatsBase   string
	wbReportsBase string
	wbContentBase string
	ozonBase      string
}

type marketplaceNumber float64

func (value *marketplaceNumber) UnmarshalJSON(data []byte) error {
	raw := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if raw == "" || raw == "null" {
		*value = 0
		return nil
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return err
	}
	*value = marketplaceNumber(parsed)
	return nil
}

func NewMarketplaceExecutor(wbToken, ozonClientID, ozonAPIKey string) *MarketplaceExecutor {
	return &MarketplaceExecutor{
		client: &http.Client{Timeout: 20 * time.Second}, wbToken: strings.TrimSpace(wbToken),
		ozonClientID: strings.TrimSpace(ozonClientID), ozonAPIKey: strings.TrimSpace(ozonAPIKey),
		wbBase: defaultWBBase, wbStatsBase: defaultWBStats, wbReportsBase: defaultWBReports,
		wbContentBase: defaultWBContent, ozonBase: defaultOzonBase,
	}
}

func (executor *MarketplaceExecutor) FetchSales(
	ctx context.Context,
	channel string,
	from, to time.Time,
) ([]procurement.SalesRecord, error) {
	switch channel {
	case "wb":
		return executor.fetchWBSales(ctx, from, to)
	case "ozon":
		return executor.fetchOzonSales(ctx, from, to)
	default:
		return nil, fmt.Errorf("история продаж канала %s не поддерживается", channel)
	}
}

func (executor *MarketplaceExecutor) fetchWBSales(ctx context.Context, from, to time.Time) ([]procurement.SalesRecord, error) {
	if !executor.Configured("wb") {
		return nil, errors.New("токен Wildberries не настроен")
	}
	const limit = 100000
	type wbReportRow struct {
		RrdID           int64             `json:"rrdId"`
		NmID            int64             `json:"nmId"`
		DocType         string            `json:"docTypeName"`
		Operation       string            `json:"sellerOperName"`
		Quantity        marketplaceNumber `json:"quantity"`
		RetailAmount    marketplaceNumber `json:"retailAmount"`
		SaleDate        string            `json:"saleDt"`
		RealizationDate string            `json:"rrDate"`
	}
	rows := make([]wbReportRow, 0)
	var rrdID int64
	for page := 0; page < 100; page++ {
		payload := map[string]any{
			"dateFrom": from.Format("2006-01-02"), "dateTo": to.Format("2006-01-02"),
			"limit": limit, "rrdId": rrdID, "period": "daily",
			"fields": []string{"rrdId", "nmId", "docTypeName", "sellerOperName", "quantity", "retailAmount", "saleDt", "rrDate"},
		}
		var response []wbReportRow
		headers := map[string]string{"Authorization": executor.wbToken}
		err := executor.request(ctx, http.MethodPost, executor.wbStatsBase+"/api/finance/v1/sales-reports/detailed", payload, headers, &response)
		if isRateLimit(err) {
			return executor.fetchWBOperationalSales(ctx, from, to)
		}
		if err != nil {
			err = executor.requestRead(ctx, http.MethodPost, executor.wbStatsBase+"/api/finance/v1/sales-reports/detailed", payload, headers, &response)
		}
		if err != nil {
			if isRateLimit(err) {
				return executor.fetchWBOperationalSales(ctx, from, to)
			}
			return nil, fmt.Errorf("получить продажи Wildberries: %w", err)
		}
		if len(response) == 0 {
			break
		}
		rows = append(rows, response...)
		next := response[len(response)-1].RrdID
		if len(response) < limit {
			break
		}
		if next <= 0 || next == rrdID {
			return nil, errors.New("Wildberries вернул некорректный курсор финансового отчёта")
		}
		rrdID = next
	}
	records := make([]procurement.SalesRecord, 0, len(rows))
	for _, row := range rows {
		if row.NmID <= 0 || row.Quantity == 0 {
			continue
		}
		date, err := parseMarketplaceDate(firstNonEmpty(row.SaleDate, row.RealizationDate))
		if err != nil || date.Before(from) || date.After(to.Add(24*time.Hour-time.Second)) {
			continue
		}
		sign := 1
		kind := strings.ToLower(row.DocType + " " + row.Operation)
		if strings.Contains(kind, "возврат") || strings.Contains(kind, "return") {
			sign = -1
		} else if !strings.Contains(kind, "продаж") && !strings.Contains(kind, "sale") {
			continue
		}
		records = append(records, procurement.SalesRecord{
			Date: date, ExternalID: strconv.FormatInt(row.NmID, 10),
			Units: sign * int(math.Round(math.Abs(float64(row.Quantity)))), GrossRUB: float64(sign) * math.Abs(float64(row.RetailAmount)),
		})
	}
	return records, nil
}

// fetchWBOperationalSales uses WB's operational sales report as a fallback
// when the financial report's global one-request-per-minute seller limit is
// occupied by another integration. The report contains both sales and returns
// and keeps 90 days, which covers the procurement recommendation window.
func (executor *MarketplaceExecutor) fetchWBOperationalSales(ctx context.Context, from, to time.Time) ([]procurement.SalesRecord, error) {
	type wbSale struct {
		NmID          int64             `json:"nmId"`
		SaleID        string            `json:"saleID"`
		Date          string            `json:"date"`
		LastChange    string            `json:"lastChangeDate"`
		FinishedPrice marketplaceNumber `json:"finishedPrice"`
		PriceWithDisc marketplaceNumber `json:"priceWithDisc"`
	}
	endpoint, err := url.Parse(executor.wbReportsBase + "/api/v1/supplier/sales")
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	query.Set("dateFrom", from.Format(time.RFC3339))
	query.Set("flag", "0")
	endpoint.RawQuery = query.Encode()
	var rows []wbSale
	if err := executor.requestRead(ctx, http.MethodGet, endpoint.String(), nil,
		map[string]string{"Authorization": executor.wbToken}, &rows); err != nil {
		return nil, fmt.Errorf("получить оперативные продажи Wildberries: %w", err)
	}
	records := make([]procurement.SalesRecord, 0, len(rows))
	for _, row := range rows {
		date, err := parseMarketplaceDate(firstNonEmpty(row.Date, row.LastChange))
		if err != nil || date.Before(from) || date.After(to.Add(24*time.Hour-time.Second)) || row.NmID <= 0 {
			continue
		}
		sign := 1
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(row.SaleID)), "R") {
			sign = -1
		}
		amount := math.Abs(float64(row.FinishedPrice))
		if amount == 0 {
			amount = math.Abs(float64(row.PriceWithDisc))
		}
		records = append(records, procurement.SalesRecord{
			Date: date, ExternalID: strconv.FormatInt(row.NmID, 10), Units: sign, GrossRUB: float64(sign) * amount,
		})
	}
	return records, nil
}

func isRateLimit(err error) bool {
	var remote *remoteError
	return errors.As(err, &remote) && remote.Status == http.StatusTooManyRequests
}

type ozonPosting struct {
	CreatedAt string `json:"created_at"`
	Status    string `json:"status"`
	Products  []struct {
		OfferID  string `json:"offer_id"`
		Quantity int    `json:"quantity"`
		Price    string `json:"price"`
	} `json:"products"`
}

func (executor *MarketplaceExecutor) fetchOzonSales(ctx context.Context, from, to time.Time) ([]procurement.SalesRecord, error) {
	if !executor.Configured("ozon") {
		return nil, errors.New("ключи Ozon не настроены")
	}
	records := make([]procurement.SalesRecord, 0)
	for _, path := range []string{"/v3/posting/fbs/list", "/v2/posting/fbo/list"} {
		items, err := executor.fetchOzonPostings(ctx, path, from, to)
		if err != nil {
			var empty *emptyBodyError
			if errors.As(err, &empty) {
				continue
			}
			return nil, err
		}
		for _, posting := range items {
			if posting.Status != "delivered" {
				continue
			}
			date, err := parseMarketplaceDate(posting.CreatedAt)
			if err != nil {
				continue
			}
			for _, product := range posting.Products {
				price, _ := strconv.ParseFloat(product.Price, 64)
				if strings.TrimSpace(product.OfferID) != "" && product.Quantity > 0 {
					records = append(records, procurement.SalesRecord{
						Date: date, ExternalID: product.OfferID, Units: product.Quantity,
						GrossRUB: price * float64(product.Quantity),
					})
				}
			}
		}
	}
	if len(records) == 0 {
		return executor.fetchOzonAnalyticsSales(ctx, from, to)
	}
	return records, nil
}

// fetchOzonAnalyticsSales is a second independent read path for accounts where
// posting lists return an empty successful response. Ozon's analytics endpoint
// reports ordered units by SKU/day; the dimension name contains the seller's
// offer_id used by our directory.
func (executor *MarketplaceExecutor) fetchOzonAnalyticsSales(ctx context.Context, from, to time.Time) ([]procurement.SalesRecord, error) {
	const limit = 1000
	if minimum := to.AddDate(0, 0, -89); from.Before(minimum) {
		from = minimum
	}
	records := make([]procurement.SalesRecord, 0)
	for offset := 0; offset < 100000; offset += limit {
		payload := map[string]any{
			"date_from": from.Format("2006-01-02"), "date_to": to.Format("2006-01-02"),
			"metrics": []string{"ordered_units", "revenue"}, "dimension": []string{"sku", "day"},
			"filters": []any{}, "sort": []any{}, "limit": limit, "offset": offset,
		}
		var response struct {
			Result struct {
				Data []struct {
					Dimensions []struct {
						ID   string `json:"id"`
						Name string `json:"name"`
					} `json:"dimensions"`
					Metrics []marketplaceNumber `json:"metrics"`
				} `json:"data"`
			} `json:"result"`
		}
		if err := executor.requestRead(ctx, http.MethodPost, executor.ozonBase+"/v1/analytics/data", payload,
			map[string]string{"Client-Id": executor.ozonClientID, "Api-Key": executor.ozonAPIKey}, &response); err != nil {
			return nil, fmt.Errorf("получить аналитику продаж Ozon: %w", err)
		}
		for _, row := range response.Result.Data {
			if len(row.Dimensions) < 2 || len(row.Metrics) == 0 {
				continue
			}
			offerID := strings.TrimSpace(firstNonEmpty(row.Dimensions[0].Name, row.Dimensions[0].ID))
			date, err := parseMarketplaceDate(row.Dimensions[1].ID)
			units := int(math.Round(float64(row.Metrics[0])))
			if err != nil || offerID == "" || units <= 0 {
				continue
			}
			gross := float64(0)
			if len(row.Metrics) > 1 {
				gross = float64(row.Metrics[1])
			}
			records = append(records, procurement.SalesRecord{Date: date, ExternalID: offerID, Units: units, GrossRUB: gross})
		}
		if len(response.Result.Data) < limit {
			break
		}
	}
	return records, nil
}

func (executor *MarketplaceExecutor) fetchOzonPostings(
	ctx context.Context,
	path string,
	from, to time.Time,
) ([]ozonPosting, error) {
	const limit = 1000
	result := make([]ozonPosting, 0)
	for offset := 0; offset < 100000; offset += limit {
		with := map[string]bool{"analytics_data": false, "financial_data": false}
		payload := map[string]any{
			"dir": "ASC", "filter": map[string]any{
				"since": from.Format(time.RFC3339), "to": to.Add(24*time.Hour - time.Second).Format(time.RFC3339), "status": "delivered",
			},
			"limit": limit, "offset": offset, "with": with,
		}
		if strings.Contains(path, "/fbs/") {
			with["barcodes"], with["translit"] = false, false
		} else {
			payload["translit"] = true
		}
		var response struct {
			Result json.RawMessage `json:"result"`
		}
		if err := executor.requestRead(ctx, http.MethodPost, executor.ozonBase+path, payload,
			map[string]string{"Client-Id": executor.ozonClientID, "Api-Key": executor.ozonAPIKey}, &response); err != nil {
			return nil, fmt.Errorf("получить продажи Ozon %s: %w", path, err)
		}
		var page []ozonPosting
		if len(response.Result) > 0 && response.Result[0] == '[' {
			if err := json.Unmarshal(response.Result, &page); err != nil {
				return nil, fmt.Errorf("разобрать продажи Ozon: %w", err)
			}
		} else {
			var wrapped struct {
				Postings []ozonPosting `json:"postings"`
			}
			if err := json.Unmarshal(response.Result, &wrapped); err != nil {
				return nil, fmt.Errorf("разобрать продажи Ozon: %w", err)
			}
			page = wrapped.Postings
		}
		result = append(result, page...)
		if len(page) < limit {
			break
		}
	}
	return result, nil
}

func parseMarketplaceDate(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if parsed, err := time.Parse(layout, strings.TrimSpace(value)); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, errors.New("некорректная дата продажи")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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

// Probe validates credentials without changing prices, stock or orders.
func (executor *MarketplaceExecutor) Probe(ctx context.Context, channel string) error {
	switch channel {
	case "wb":
		if !executor.Configured("wb") {
			return errors.New("токен Wildberries не настроен")
		}
		var response map[string]any
		if err := executor.requestRead(ctx, http.MethodGet, executor.wbBase+"/ping", nil,
			map[string]string{"Authorization": executor.wbToken}, &response); err != nil {
			return fmt.Errorf("проверить доступ Wildberries к ценам: %w", err)
		}
		// Every WB API category has its own read-only /ping. Using the finance
		// ping verifies the permission needed for sales history without consuming
		// the financial report endpoint's strict rate limit.
		response = nil
		if err := executor.requestRead(ctx, http.MethodGet, executor.wbStatsBase+"/ping", nil,
			map[string]string{"Authorization": executor.wbToken}, &response); err != nil {
			return fmt.Errorf("проверить доступ Wildberries к финансам: %w", err)
		}
		return nil
	case "ozon":
		if !executor.Configured("ozon") {
			return errors.New("ключи Ozon не настроены")
		}
		payload := map[string]any{
			"filter": map[string]any{"visibility": "ALL"}, "last_id": "", "limit": 1,
		}
		var response map[string]any
		if err := executor.requestRead(ctx, http.MethodPost, executor.ozonBase+"/v3/product/list", payload,
			map[string]string{"Client-Id": executor.ozonClientID, "Api-Key": executor.ozonAPIKey}, &response); err != nil {
			return fmt.Errorf("проверить Ozon: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("канал %s не поддерживается", channel)
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
		"old_price": oldPrice, "min_price": "", "currency_code": "RUB", "auto_action_enabled": "UNKNOWN",
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
	Status     int
	Message    string
	RetryAfter time.Duration
}

func (err *remoteError) Error() string {
	return fmt.Sprintf("внешний API ответил %d: %s", err.Status, err.Message)
}

// emptyBodyError — код успеха и пустое тело. Отдельный тип, а не текст:
// по нему принимается решение о повторе и о запасном пути, и ловить его
// сравнением строк значило бы сломать оба, переписав сообщение.
type emptyBodyError struct {
	Path   string
	Status int
}

func (err *emptyBodyError) Error() string {
	return fmt.Sprintf("%s ответил %d с пустым телом", err.Path, err.Status)
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
		return &remoteError{Status: response.StatusCode, Message: safeRemoteMessage(string(content)), RetryAfter: marketplaceRetryAfter(response)}
	}
	if response.StatusCode == http.StatusNoContent {
		return nil
	}
	// Пустой ответ с кодом успеха и разбор, который не сошёлся, — разные
	// беды, но выглядели одинаково: «unexpected end of JSON input» и ни
	// слова о том, кто это сказал. Поэтому называем адрес, код и начало
	// тела. Тело — собственный каталог магазина, ключей в нём нет: они
	// уходят заголовком.
	if len(bytes.TrimSpace(content)) == 0 && target != nil {
		return &emptyBodyError{Path: requestPath(endpoint), Status: response.StatusCode}
	}
	if err := json.Unmarshal(content, target); err != nil {
		return fmt.Errorf("%s ответил %d, разобрать не удалось (%w): %s",
			requestPath(endpoint), response.StatusCode, err, safeRemoteMessage(string(content)))
	}
	return nil
}

// requestRead retries only read-only marketplace operations. Price writes use
// request directly: retrying a mutation after an ambiguous network failure can
// create a duplicate remote operation.
func (executor *MarketplaceExecutor) requestRead(ctx context.Context, method, endpoint string, payload any, headers map[string]string, target any) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		lastErr = executor.request(ctx, method, endpoint, payload, headers, target)
		if lastErr == nil {
			return nil
		}
		var remote *remoteError
		var empty *emptyBodyError
		retry := errors.As(lastErr, &remote) && (remote.Status == http.StatusTooManyRequests || remote.Status >= 500)
		if !retry && !errors.As(lastErr, &empty) {
			return lastErr
		}
		delay := time.Duration(attempt+1) * time.Second
		if remote != nil && remote.Status == http.StatusTooManyRequests && remote.RetryAfter == 0 {
			// WB's financial report has a strict global seller limit. A quick
			// retry only consumes the next attempt, so use a conservative pause
			// when the API omits an explicit Retry-After value.
			delay = time.Minute
		}
		if remote != nil && remote.RetryAfter > delay {
			delay = remote.RetryAfter
		}
		if delay > time.Minute {
			delay = time.Minute
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}

func marketplaceRetryAfter(response *http.Response) time.Duration {
	value := firstNonEmpty(response.Header.Get("Retry-After"), response.Header.Get("X-Ratelimit-Retry"))
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return 0
}

// requestPath оставляет от адреса только путь: хост площадки в сообщении
// ничего не объясняет, а строку удлиняет.
func requestPath(endpoint string) string {
	if parsed, err := url.Parse(endpoint); err == nil && parsed.Path != "" {
		return parsed.Path
	}
	return endpoint
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

// FetchCatalog читает карточки маркетплейса, чтобы связать их с
// номенклатурой СБИС.
//
// Без этой связи продажи WB и Ozon приходят, сохраняются и не участвуют в
// расчёте: рекомендация занижена ровно на то, что продалось на площадках.
// Заполнять сотни артикулов руками никто не станет, поэтому справочник
// подтягивается сам, а решение о связи принимается по точному совпадению
// кода или штрихкода — не по названию.
func (executor *MarketplaceExecutor) FetchCatalog(ctx context.Context, channel string) ([]procurement.ChannelProduct, error) {
	switch channel {
	case "wb":
		return executor.fetchWBCatalog(ctx)
	case "ozon":
		return executor.fetchOzonCatalog(ctx)
	default:
		return nil, fmt.Errorf("справочник канала %s не поддерживается", channel)
	}
}

func (executor *MarketplaceExecutor) fetchWBCatalog(ctx context.Context) ([]procurement.ChannelProduct, error) {
	if !executor.Configured("wb") {
		return nil, errors.New("токен Wildberries не настроен")
	}
	headers := map[string]string{"Authorization": executor.wbToken}
	items := make([]procurement.ChannelProduct, 0, 200)
	cursor := map[string]any{"limit": 100}
	// Курсор WB отдаёт следующую страницу через updatedAt и nmID последней
	// карточки. Потолок страниц выбран с запасом: он спасает от бесконечного
	// цикла, если площадка перестанет двигать курсор.
	for page := 0; page < 200; page++ {
		payload := map[string]any{
			"settings": map[string]any{
				"cursor": cursor,
				"filter": map[string]any{"withPhoto": -1},
			},
		}
		var response struct {
			Cards []struct {
				NmID       int64  `json:"nmID"`
				VendorCode string `json:"vendorCode"`
				Title      string `json:"title"`
				Sizes      []struct {
					Skus []string `json:"skus"`
				} `json:"sizes"`
			} `json:"cards"`
			Cursor struct {
				UpdatedAt string `json:"updatedAt"`
				NmID      int64  `json:"nmID"`
				Total     int    `json:"total"`
			} `json:"cursor"`
		}
		if err := executor.requestRead(ctx, http.MethodPost,
			executor.wbContentBase+"/content/v2/get/cards/list", payload, headers, &response); err != nil {
			return nil, err
		}
		for _, card := range response.Cards {
			item := procurement.ChannelProduct{
				ExternalID: strconv.FormatInt(card.NmID, 10),
				Article:    strings.TrimSpace(card.VendorCode),
				Name:       strings.TrimSpace(card.Title),
			}
			for _, size := range card.Sizes {
				for _, sku := range size.Skus {
					if sku = strings.TrimSpace(sku); sku != "" {
						item.Barcodes = append(item.Barcodes, sku)
					}
				}
			}
			items = append(items, item)
		}
		if len(response.Cards) < 100 {
			break
		}
		cursor = map[string]any{
			"limit": 100, "updatedAt": response.Cursor.UpdatedAt, "nmID": response.Cursor.NmID,
		}
	}
	return items, nil
}

func (executor *MarketplaceExecutor) fetchOzonCatalog(ctx context.Context) ([]procurement.ChannelProduct, error) {
	if !executor.Configured("ozon") {
		return nil, errors.New("ключи Ozon не настроены")
	}
	headers := map[string]string{
		"Client-Id": executor.ozonClientID, "Api-Key": executor.ozonAPIKey,
	}
	offers := make([]string, 0, 200)
	lastID := ""
	for page := 0; page < 200; page++ {
		payload := map[string]any{"filter": map[string]any{"visibility": "ALL"}, "limit": 1000, "last_id": lastID}
		var response struct {
			Result struct {
				Items []struct {
					OfferID string `json:"offer_id"`
				} `json:"items"`
				LastID string `json:"last_id"`
			} `json:"result"`
		}
		if err := executor.requestRead(ctx, http.MethodPost,
			executor.ozonBase+"/v3/product/list", payload, headers, &response); err != nil {
			return nil, err
		}
		for _, item := range response.Result.Items {
			if offer := strings.TrimSpace(item.OfferID); offer != "" {
				offers = append(offers, offer)
			}
		}
		if len(response.Result.Items) == 0 || response.Result.LastID == "" || response.Result.LastID == lastID {
			break
		}
		lastID = response.Result.LastID
	}

	// Список товаров отдаёт только offer_id — собственный код продавца,
	// который со справочником СБИС обычно не совпадает ничем. Штрихкод
	// совпадает: он один и тот же на этикетке и там, и там. Поэтому за
	// карточками ходим вторым запросом.
	items := make([]procurement.ChannelProduct, 0, len(offers))
	for start := 0; start < len(offers); start += 100 {
		finish := min(start+100, len(offers))
		var response struct {
			Items []struct {
				OfferID  string   `json:"offer_id"`
				Barcodes []string `json:"barcodes"`
				Barcode  string   `json:"barcode"`
				Name     string   `json:"name"`
			} `json:"items"`
			Result struct {
				Items []struct {
					OfferID  string   `json:"offer_id"`
					Barcodes []string `json:"barcodes"`
					Barcode  string   `json:"barcode"`
					Name     string   `json:"name"`
				} `json:"items"`
			} `json:"result"`
		}
		// Метод карточек у Ozon пережил смену версии, и какая из них жива
		// на конкретном аккаунте — видно только по ответу. Спрашиваем
		// сначала новую, потом старую; обе только читают.
		err := executor.requestRead(ctx, http.MethodPost, executor.ozonBase+"/v3/product/info/list",
			map[string]any{"offer_id": offers[start:finish]}, headers, &response)
		if err != nil {
			legacyErr := executor.requestRead(ctx, http.MethodPost, executor.ozonBase+"/v2/product/info/list",
				map[string]any{"offer_id": offers[start:finish]}, headers, &response)
			if legacyErr != nil {
				return nil, fmt.Errorf("карточки Ozon: v3 — %w; v2 — %v", err, legacyErr)
			}
		}
		details := response.Items
		if len(details) == 0 {
			details = response.Result.Items
		}
		for _, detail := range details {
			offer := strings.TrimSpace(detail.OfferID)
			if offer == "" {
				continue
			}
			item := procurement.ChannelProduct{
				ExternalID: offer, Article: offer, Name: strings.TrimSpace(detail.Name),
			}
			for _, barcode := range append(detail.Barcodes, detail.Barcode) {
				if barcode = strings.TrimSpace(barcode); barcode != "" {
					item.Barcodes = append(item.Barcodes, barcode)
				}
			}
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		// Карточки есть, а подробностей нет — значит, метод закрыт правами
		// ключа. Возвращаем то, что знаем, чтобы разведка всё равно
		// показала, по чему шло сравнение.
		for _, offer := range offers {
			items = append(items, procurement.ChannelProduct{ExternalID: offer, Article: offer})
		}
	}
	return items, nil
}
