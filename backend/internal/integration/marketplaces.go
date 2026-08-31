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

// maxMarketplaceResponse — потолок одного ответа площадки. Страница в
// тысячу отправлений Ozon укладывается в единицы мегабайт; запас взят
// с расчётом на самый разговорчивый ответ, но так, чтобы битый или
// бесконечный поток не съел память.
const maxMarketplaceResponse = 64 << 20

const (
	defaultWBBase    = "https://discounts-prices-api.wildberries.ru"
	defaultWBReports = "https://statistics-api.wildberries.ru"
	defaultWBContent = "https://content-api.wildberries.ru"
	defaultOzonBase  = "https://api-seller.ozon.ru"
)

type MarketplaceExecutor struct {
	client       *http.Client
	wbLimiter    WBRequestLimiter
	wbToken      string
	ozonClientID string
	ozonAPIKey   string
	wbBase        string
	wbReportsBase string
	wbContentBase string
	ozonBase      string
}

// WBRequestLimiter coordinates seller-token limits across deployments and
// application instances. The production implementation stores reservations
// in PostgreSQL; tests and deployments without a database keep using the
// in-process paced transport.
type WBRequestLimiter interface {
	ReserveWBRequest(context.Context, string, time.Duration) (time.Duration, error)
	DeferWBRequests(context.Context, string, time.Duration) error
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
		// Транспорт с паузой: площадки считают не только объём, но и частоту.
		client: &http.Client{Timeout: 90 * time.Second, Transport: newPacedTransport(nil)},
		wbToken: strings.TrimSpace(wbToken),
		ozonClientID: strings.TrimSpace(ozonClientID), ozonAPIKey: strings.TrimSpace(ozonAPIKey),
		wbBase: defaultWBBase, wbReportsBase: defaultWBReports,
		wbContentBase: defaultWBContent, ozonBase: defaultOzonBase,
	}
}

func (executor *MarketplaceExecutor) WithWBRequestLimiter(limiter WBRequestLimiter) *MarketplaceExecutor {
	executor.wbLimiter = limiter
	return executor
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
	// The previous implementation called the strict financial report first
	// and immediately fell back to operational sales after its 429. One sync
	// therefore consumed two seller-wide API calls. The local mirror needs the
	// rolling operational window only; older rows remain in PostgreSQL.
	return executor.fetchWBOperationalSales(ctx, from, to)
}

// fetchWBOperationalSales is the single source for the hourly sales mirror.
// It contains both sales and returns and keeps the rolling window used by the
// procurement recommendation. Exactly one request is made per mirror run.
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
			date, err := posting.saleDate()
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
		// Аналитика Ozon отдаёт продажи по SKU и названию товара, а
		// справочник связан по offer_id — коду продавца. Совпасть они не
		// могут, и строки из аналитики связать не с чем: на экране это
		// выглядело как «загружено 1529, связано 0», то есть отказ,
		// переодетый в успех. Пусть лучше канал честно скажет, что
		// отправлений не нашёл.
		return nil, fmt.Errorf(
			"Ozon не вернул отправлений за период: %s — %s",
			from.Format("2006-01-02"), to.Format("2006-01-02"),
		)
	}
	return records, nil
}

func (executor *MarketplaceExecutor) fetchOzonPostings(
	ctx context.Context,
	path string,
	from, to time.Time,
) ([]ozonPosting, error) {
	const limit = 1000
	// Ozon отдаёт отправления окном, а не всей историей: годовой запрос
	// возвращается пустым, и со стороны это неотличимо от «продаж не было».
	// Поэтому ходим месяцами. Заодно страницы получаются короче, а к ним
	// применяется пауза между запросами.
	const window = 30 * 24 * time.Hour
	result := make([]ozonPosting, 0)
	for start := from; !start.After(to); start = start.Add(window) {
		finish := start.Add(window - time.Second)
		if last := to.Add(24*time.Hour - time.Second); finish.After(last) {
			finish = last
		}
		page, err := executor.fetchOzonPostingWindow(ctx, path, start, finish, limit)
		if err != nil {
			return nil, err
		}
		result = append(result, page...)
	}
	return result, nil
}

func (executor *MarketplaceExecutor) fetchOzonPostingWindow(
	ctx context.Context,
	path string,
	since, until time.Time,
	limit int,
) ([]ozonPosting, error) {
	result := make([]ozonPosting, 0)
	for offset := 0; offset < 100000; offset += limit {
		with := map[string]bool{"analytics_data": false, "financial_data": false}
		// Статус в фильтре не задаём: у FBS и FBO наборы значений разные, и
		// неизвестное площадке значение молча возвращает пустоту. Доставленные
		// отбираем сами, уже разобрав ответ.
		payload := map[string]any{
			"dir": "ASC", "filter": map[string]any{
				"since": since.Format(time.RFC3339), "to": until.Format(time.RFC3339),
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
		// The mirror reads operational sales from the statistics API, so its
		// category ping verifies the permission without consuming the report.
		response = nil
		if err := executor.requestRead(ctx, http.MethodGet, executor.wbReportsBase+"/ping", nil,
			map[string]string{"Authorization": executor.wbToken}, &response); err != nil {
			return fmt.Errorf("проверить доступ Wildberries к статистике: %w", err)
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

func (executor *MarketplaceExecutor) ExecuteGroup(ctx context.Context, items []procurement.ActionItem) []procurement.ActionOutcome {
	if len(items) == 0 {
		return nil
	}
	switch items[0].Channel {
	case "wb":
		return executor.executeWBGroup(ctx, items)
	case "ozon":
		return executor.executeOzonGroup(ctx, items)
	default:
		outcomes := make([]procurement.ActionOutcome, 0, len(items))
		for _, item := range items {
			result, err := executor.Execute(ctx, item)
			outcomes = append(outcomes, procurement.ActionOutcome{ItemID: item.ID, Result: result, Err: err})
		}
		return outcomes
	}
}

func sameOutcome(items []procurement.ActionItem, result procurement.ActionExecution, err error) []procurement.ActionOutcome {
	outcomes := make([]procurement.ActionOutcome, 0, len(items))
	for _, item := range items {
		outcomes = append(outcomes, procurement.ActionOutcome{ItemID: item.ID, Result: result, Err: err})
	}
	return outcomes
}

func (executor *MarketplaceExecutor) executeWB(ctx context.Context, item procurement.ActionItem) (procurement.ActionExecution, error) {
	outcome := executor.executeWBGroup(ctx, []procurement.ActionItem{item})[0]
	return outcome.Result, outcome.Err
}

func (executor *MarketplaceExecutor) executeWBGroup(ctx context.Context, items []procurement.ActionItem) []procurement.ActionOutcome {
	if !executor.Configured("wb") {
		return sameOutcome(items, procurement.ActionExecution{}, errors.New("токен Wildberries не настроен"))
	}
	if items[0].ExternalOperationID == "" {
		data := make([]map[string]any, 0, len(items))
		for _, item := range items {
			nmID, err := strconv.ParseInt(strings.TrimSpace(item.ExternalArticle), 10, 64)
			if err != nil || nmID <= 0 {
				return sameOutcome(items, procurement.ActionExecution{}, errors.New("для Wildberries нужен числовой nmID, а не артикул продавца"))
			}
			price := int64(item.NewValue)
		discount := int64(0)
		if item.CompareAtValue != nil && *item.CompareAtValue > item.NewValue {
			price = int64(*item.CompareAtValue)
			discount = int64((1 - item.NewValue/float64(price)) * 100)
		}
			data = append(data, map[string]any{"nmID": nmID, "price": price, "discount": discount})
		}
		payload := map[string]any{"data": data}
		var response struct {
			Data struct {
				ID int64 `json:"id"`
			} `json:"data"`
			Error     bool   `json:"error"`
			ErrorText string `json:"errorText"`
		}
		if err := executor.request(ctx, http.MethodPost, executor.wbBase+"/api/v2/upload/task", payload,
			map[string]string{"Authorization": executor.wbToken}, &response); err != nil {
			// WB reports an idempotent no-op as HTTP 400. The requested prices
			// are already effective, so the batch is complete rather than failed.
			if strings.Contains(strings.ToLower(err.Error()), "already set") || strings.Contains(strings.ToLower(err.Error()), "уже установ") {
				return sameOutcome(items, procurement.ActionExecution{Completed: true}, nil)
			}
			return sameOutcome(items, marketplaceRetryExecution(err), err)
		}
		if response.Error || response.Data.ID <= 0 {
			return sameOutcome(items, procurement.ActionExecution{}, fmt.Errorf("Wildberries отклонил загрузку: %s", safeRemoteMessage(response.ErrorText)))
		}
		return sameOutcome(items, procurement.ActionExecution{ExternalOperationID: strconv.FormatInt(response.Data.ID, 10), RetryAfter: 5 * time.Second}, nil)
	}

	uploadID, err := strconv.ParseInt(items[0].ExternalOperationID, 10, 64)
	if err != nil || uploadID <= 0 {
		return sameOutcome(items, procurement.ActionExecution{}, errors.New("повреждён идентификатор загрузки Wildberries"))
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
			return sameOutcome(items, procurement.ActionExecution{ExternalOperationID: items[0].ExternalOperationID, RetryAfter: 5 * time.Second}, nil)
		}
		result := marketplaceRetryExecution(err)
		result.ExternalOperationID = items[0].ExternalOperationID
		return sameOutcome(items, result, err)
	}
	if response.Error {
		return sameOutcome(items, procurement.ActionExecution{ExternalOperationID: items[0].ExternalOperationID}, fmt.Errorf("Wildberries не подтвердил загрузку: %s", safeRemoteMessage(response.ErrorText)))
	}
	switch response.Data.Status {
	case 3:
		return sameOutcome(items, procurement.ActionExecution{Completed: true, ExternalOperationID: items[0].ExternalOperationID}, nil)
	case 4, 5, 6:
		return sameOutcome(items, procurement.ActionExecution{ExternalOperationID: items[0].ExternalOperationID}, fmt.Errorf("Wildberries завершил загрузку со статусом %d", response.Data.Status))
	default:
		return sameOutcome(items, procurement.ActionExecution{ExternalOperationID: items[0].ExternalOperationID, RetryAfter: 5 * time.Second}, nil)
	}
}

func (executor *MarketplaceExecutor) executeOzon(ctx context.Context, item procurement.ActionItem) (procurement.ActionExecution, error) {
	outcome := executor.executeOzonGroup(ctx, []procurement.ActionItem{item})[0]
	return outcome.Result, outcome.Err
}

func (executor *MarketplaceExecutor) executeOzonGroup(ctx context.Context, items []procurement.ActionItem) []procurement.ActionOutcome {
	if !executor.Configured("ozon") {
		return sameOutcome(items, procurement.ActionExecution{}, errors.New("ключи Ozon не настроены"))
	}
	prices := make([]map[string]any, 0, len(items))
	for _, item := range items {
		price := strconv.FormatInt(int64(item.NewValue), 10)
		oldPrice := "0"
		if item.CompareAtValue != nil && *item.CompareAtValue > item.NewValue {
			oldPrice = strconv.FormatInt(int64(*item.CompareAtValue), 10)
		}
		// Optional empty fields are deliberately omitted: Ozon validates an
		// explicitly supplied min_price/enum even when the seller did not set it.
		prices = append(prices, map[string]any{
			"offer_id": strings.TrimSpace(item.ExternalArticle), "price": price,
			"old_price": oldPrice, "currency_code": "RUB",
		})
	}
	payload := map[string]any{"prices": prices}
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
		return sameOutcome(items, marketplaceRetryExecution(err), err)
	}
	byOffer := make(map[string]struct {
		updated bool
		message string
	}, len(response.Result))
	for _, item := range response.Result {
		message := ""
		if len(item.Errors) > 0 {
			message = safeRemoteMessage(item.Errors[0].Message)
		}
		byOffer[item.OfferID] = struct { updated bool; message string }{item.Updated, message}
	}
	outcomes := make([]procurement.ActionOutcome, 0, len(items))
	for _, item := range items {
		remote, found := byOffer[strings.TrimSpace(item.ExternalArticle)]
		if found && remote.updated {
			outcomes = append(outcomes, procurement.ActionOutcome{ItemID: item.ID, Result: procurement.ActionExecution{Completed: true}})
			continue
		}
		message := "Ozon не подтвердил изменение цены"
		if found && remote.message != "" {
			message += ": " + remote.message
		}
		outcomes = append(outcomes, procurement.ActionOutcome{ItemID: item.ID, Err: errors.New(message)})
	}
	return outcomes
}

type remoteError struct {
	Status     int
	Message    string
	RetryAfter time.Duration
}

func (err *remoteError) RetryDelay() time.Duration { return err.RetryAfter }

// A 429 response means the marketplace rejected the mutation before applying
// it, so retrying after its advertised window is safe. Network and 5xx errors
// remain non-immediate because their outcome can be ambiguous.
func marketplaceRetryExecution(err error) procurement.ActionExecution {
	var remote *remoteError
	if !errors.As(err, &remote) || remote.Status != http.StatusTooManyRequests {
		return procurement.ActionExecution{}
	}
	delay := remote.RetryAfter
	if delay <= 0 {
		delay = 65 * time.Second
	}
	return procurement.ActionExecution{RetryAfter: delay}
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
	if bucket := wbRequestBucket(endpoint); bucket != "" && executor.wbLimiter != nil {
		wait, err := executor.wbLimiter.ReserveWBRequest(ctx, bucket, marketplacePace(request.URL.Hostname()))
		if err != nil {
			return fmt.Errorf("reserve Wildberries API request: %w", err)
		}
		if err := waitForContext(ctx, wait); err != nil {
			return err
		}
	}
	response, err := executor.client.Do(request)
	if err != nil {
		return fmt.Errorf("marketplace request failed: %w", err)
	}
	defer response.Body.Close() //nolint:errcheck
	// Потолок в 64 килобайта, стоявший здесь раньше, резал не мусор, а
	// данные: страница продаж Ozon или карточек Wildberries весит сотни
	// килобайт, обрывалась на середине, и разбор падал с «unexpected end of
	// JSON input». Со стороны это выглядело как молчание площадки, хотя
	// площадка отвечала исправно. Потолок нужен — но такой, в который
	// помещается настоящая страница, а не такой, который её отрезает.
	limit := int64(maxMarketplaceResponse)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// Тело ошибки читаем скупо: оно идёт в сообщение человеку.
		limit = 64 << 10
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, limit))
	if err != nil {
		return fmt.Errorf("read marketplace response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		remote := &remoteError{Status: response.StatusCode, Message: safeRemoteMessage(string(content)), RetryAfter: marketplaceRetryAfter(response)}
		if remote.Status == http.StatusTooManyRequests && executor.wbLimiter != nil {
			delay := remote.RetryAfter
			if delay <= 0 {
				delay = 65 * time.Second
			}
			if bucket := wbRequestBucket(endpoint); bucket != "" {
				_ = executor.wbLimiter.DeferWBRequests(ctx, bucket, delay)
			}
		}
		return remote
	}
	if int64(len(content)) >= limit {
		return fmt.Errorf("%s ответил %d, но ответ длиннее %d МБ — уменьшите размер страницы",
			requestPath(endpoint), response.StatusCode, maxMarketplaceResponse>>20)
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
		// A 429 has already published its X-RateLimit-Retry window through the
		// shared gate. Return it to the durable worker instead of sleeping and
		// spending two more requests from the same user operation.
		if errors.As(lastErr, &remote) && remote.Status == http.StatusTooManyRequests && wbRequestBucket(endpoint) != "" {
			return lastErr
		}
		retry := remote != nil && (remote.Status == http.StatusTooManyRequests || remote.Status >= 500)
		if !retry && !errors.As(lastErr, &empty) {
			return lastErr
		}
		delay := time.Duration(attempt+1) * time.Second
		if err := waitForContext(ctx, delay); err != nil {
			return err
		}
	}
	return lastErr
}

func waitForContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func wbRequestBucket(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	switch {
	case strings.HasSuffix(host, "statistics-api.wildberries.ru"),
		strings.HasSuffix(host, "finance-api.wildberries.ru"):
		return "sales"
	case strings.HasSuffix(host, "discounts-prices-api.wildberries.ru"):
		return "prices"
	case strings.HasSuffix(host, "content-api.wildberries.ru"):
		return "content"
	case strings.HasSuffix(host, "wildberries.ru"):
		return "general"
	default:
		return ""
	}
}

func marketplaceRetryAfter(response *http.Response) time.Duration {
	value := firstNonEmpty(response.Header.Get("Retry-After"), response.Header.Get("X-Ratelimit-Retry"))
	value = strings.TrimSpace(strings.TrimSuffix(value, "s"))
	seconds, err := strconv.ParseFloat(value, 64)
	if err == nil && seconds > 0 {
		return time.Duration(seconds * float64(time.Second))
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
	prices, err := executor.fetchWBPrices(ctx)
	if err != nil {
		return nil, fmt.Errorf("прочитать текущие цены Wildberries (токену нужна категория «Цены и скидки»): %w", err)
	}
	for index := range items {
		if price, ok := prices[items[index].ExternalID]; ok {
			items[index].CurrentPrice = price.current
			items[index].CurrentBasePrice = price.base
		}
	}
	return items, nil
}

type channelPrice struct {
	current *float64
	base    *float64
}

func numberPointer(value marketplaceNumber) *float64 {
	if value <= 0 {
		return nil
	}
	converted := float64(value)
	return &converted
}

func (executor *MarketplaceExecutor) fetchWBPrices(ctx context.Context) (map[string]channelPrice, error) {
	result := make(map[string]channelPrice)
	headers := map[string]string{"Authorization": executor.wbToken}
	for offset := 0; offset < 200000; offset += 1000 {
		endpoint := executor.wbBase + "/api/v2/list/goods/filter?" + url.Values{
			"limit": {"1000"}, "offset": {strconv.Itoa(offset)},
		}.Encode()
		var response struct {
			Data struct {
				ListGoods []struct {
					NmID  int64 `json:"nmID"`
					Sizes []struct {
						Price           marketplaceNumber `json:"price"`
						DiscountedPrice marketplaceNumber `json:"discountedPrice"`
					} `json:"sizes"`
				} `json:"listGoods"`
			} `json:"data"`
		}
		if err := executor.requestRead(ctx, http.MethodGet, endpoint, nil, headers, &response); err != nil {
			return nil, err
		}
		for _, product := range response.Data.ListGoods {
			var current, base marketplaceNumber
			for _, size := range product.Sizes {
				if size.DiscountedPrice > current {
					current = size.DiscountedPrice
				}
				if size.Price > base {
					base = size.Price
				}
			}
			result[strconv.FormatInt(product.NmID, 10)] = channelPrice{current: numberPointer(current), base: numberPointer(base)}
		}
		if len(response.Data.ListGoods) < 1000 {
			break
		}
	}
	return result, nil
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
	prices, err := executor.fetchOzonPrices(ctx)
	if err != nil {
		return nil, fmt.Errorf("прочитать текущие цены Ozon (ключу нужен доступ к товарам и ценам): %w", err)
	}
	for index := range items {
		if price, ok := prices[items[index].ExternalID]; ok {
			items[index].CurrentPrice = price.current
			items[index].CurrentBasePrice = price.base
		}
	}
	return items, nil
}

func (executor *MarketplaceExecutor) fetchOzonPrices(ctx context.Context) (map[string]channelPrice, error) {
	result := make(map[string]channelPrice)
	headers := map[string]string{"Client-Id": executor.ozonClientID, "Api-Key": executor.ozonAPIKey}
	cursor := ""
	for page := 0; page < 200; page++ {
		var response struct {
			Items []struct {
				OfferID string `json:"offer_id"`
				Price struct {
					MarketingSellerPrice marketplaceNumber `json:"marketing_seller_price"`
					MarketingPrice       marketplaceNumber `json:"marketing_price"`
					RetailPrice          marketplaceNumber `json:"retail_price"`
					OldPrice             marketplaceNumber `json:"old_price"`
				} `json:"price"`
			} `json:"items"`
			Cursor string `json:"cursor"`
		}
		payload := map[string]any{"filter": map[string]any{"visibility": "ALL"}, "cursor": cursor, "limit": 1000}
		if err := executor.requestRead(ctx, http.MethodPost, executor.ozonBase+"/v5/product/info/prices", payload, headers, &response); err != nil {
			return nil, err
		}
		for _, product := range response.Items {
			current := product.Price.MarketingSellerPrice
			if current <= 0 {
				current = product.Price.MarketingPrice
			}
			if current <= 0 {
				current = product.Price.RetailPrice
			}
			result[strings.TrimSpace(product.OfferID)] = channelPrice{
				current: numberPointer(current), base: numberPointer(product.Price.OldPrice),
			}
		}
		if len(response.Items) == 0 || response.Cursor == "" || response.Cursor == cursor {
			break
		}
		cursor = response.Cursor
	}
	return result, nil
}
