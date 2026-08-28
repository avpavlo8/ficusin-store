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
	"sync"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
	sabydomain "github.com/avpavlo8/ficusin-store/backend/internal/saby"
)

const (
	defaultSabyAuth    = "https://online.sbis.ru/oauth/service/"
	defaultSabyAPI     = "https://api.sbis.ru"
	defaultSabyService = "https://online.sbis.ru/service/?srv=1"
)

// SabyClient caches one service session. Saby allows only five active
// sessions per application, so authenticating for every request is unsafe.
type SabyClient struct {
	client       *http.Client
	appClientID  string
	appSecret    string
	secretKey    string
	pointID      int64
	priceListID  int64
	authURL      string
	apiBase      string
	serviceURL   string
	mu           sync.Mutex
	catalogMu    sync.Mutex
	priceTypeMu  sync.Mutex
	token        string
	tokenUntil   time.Time
	priceDocType string
}

type sabyCatalogPage struct {
	Nomenclatures []map[string]any `json:"nomenclatures"`
	Items         []map[string]any `json:"items"`
	Result        []map[string]any `json:"result"`
}

func (page sabyCatalogPage) rows() []map[string]any {
	if page.Nomenclatures != nil {
		return page.Nomenclatures
	}
	if page.Items != nil {
		return page.Items
	}
	return page.Result
}

// FetchCatalog reads both the complete point catalogue and the configured
// price list. The complete catalogue supplies identity and stock for every
// card; the price-list snapshot supplies the effective retail price and its
// stock when the item is present there. This is the same merge contract used
// by the scheduled GitHub sync, now available immediately to an operator.
func (client *SabyClient) FetchCatalog(ctx context.Context) ([]sabydomain.CatalogItem, error) {
	if !client.Configured() {
		return nil, errors.New("ключи Saby не настроены")
	}
	client.catalogMu.Lock()
	defer client.catalogMu.Unlock()

	base := url.Values{
		"pointId": {strconv.FormatInt(client.pointID, 10)},
		"withBalance": {"true"}, "withBarcode": {"true"}, "pageSize": {"1000"},
	}
	complete, err := client.fetchCatalogTree(ctx, base)
	if err != nil {
		return nil, fmt.Errorf("прочитать каталог Saby: %w", err)
	}
	pricedQuery := cloneValues(base)
	pricedQuery.Set("priceListId", strconv.FormatInt(client.priceListID, 10))
	pricedQuery.Set("noStopList", "true")
	priced, err := client.fetchCatalogTree(ctx, pricedQuery)
	if err != nil {
		return nil, fmt.Errorf("прочитать прайс-лист Saby: %w", err)
	}
	merged := mergeSabyCatalog(complete, priced)
	if len(merged) == 0 {
		return nil, errors.New("Saby вернул пустой каталог")
	}
	encoded, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("упаковать каталог Saby: %w", err)
	}
	var items []sabydomain.CatalogItem
	if err := json.Unmarshal(encoded, &items); err != nil {
		return nil, fmt.Errorf("разобрать каталог Saby: %w", err)
	}
	return items, nil
}

func (client *SabyClient) fetchCatalogTree(ctx context.Context, base url.Values) ([]map[string]any, error) {
	return client.fetchCatalogSection(ctx, base, "", make(map[string]bool), 0)
}

func (client *SabyClient) fetchCatalogSection(ctx context.Context, base url.Values, folder string, seenFolders map[string]bool, depth int) ([]map[string]any, error) {
	rows := make([]map[string]any, 0)
	seenRows := make(map[string]bool)
	for pageNumber := 0; pageNumber < 200; pageNumber++ {
		query := cloneValues(base)
		query.Set("page", strconv.Itoa(pageNumber))
		if folder != "" {
			query.Set("folder", folder)
		}
		var page sabyCatalogPage
		if err := client.authorizedJSON(ctx, http.MethodGet, client.apiBase+"/retail/v2/nomenclature/list?"+query.Encode(), nil, &page); err != nil {
			return nil, err
		}
		fresh := 0
		for _, item := range page.rows() {
			key := sabyValue(item["hierarchicalId"])
			if key == "" {
				key = sabyValue(item["id"])
			}
			if key == "" || seenRows[key] {
				continue
			}
			seenRows[key] = true
			rows = append(rows, item)
			fresh++
		}
		if fresh == 0 {
			break
		}
		if pageNumber == 199 {
			return nil, errors.New("превышен предел страниц каталога")
		}
	}
	if depth >= 8 {
		return rows, nil
	}
	parents := append([]map[string]any(nil), rows...)
	for _, item := range parents {
		if parent, _ := item["isParent"].(bool); !parent {
			continue
		}
		folderID := sabyValue(item["hierarchicalId"])
		if folderID == "" || seenFolders[folderID] {
			continue
		}
		seenFolders[folderID] = true
		children, err := client.fetchCatalogSection(ctx, base, folderID, seenFolders, depth+1)
		if err != nil {
			return nil, err
		}
		rows = append(rows, children...)
	}
	return rows, nil
}

func mergeSabyCatalog(complete, priced []map[string]any) []map[string]any {
	byID := make(map[string]map[string]any, len(complete)+len(priced))
	order := make([]string, 0, len(complete)+len(priced))
	for _, item := range complete {
		key := sabyValue(item["id"])
		if key == "" {
			continue
		}
		copyItem := make(map[string]any, len(item))
		for field, value := range item {
			copyItem[field] = value
		}
		byID[key] = copyItem
		order = append(order, key)
	}
	for _, item := range priced {
		key := sabyValue(item["id"])
		if key == "" {
			continue
		}
		current, exists := byID[key]
		if !exists {
			current = make(map[string]any, len(item))
			byID[key] = current
			order = append(order, key)
		}
		for field, value := range item {
			if field == "cost" || field == "balance" {
				if !emptySabyValue(value) {
					current[field] = value
				}
				continue
			}
			if existing, ok := current[field]; !ok || emptySabyValue(existing) {
				current[field] = value
			}
		}
	}
	result := make([]map[string]any, 0, len(order))
	seen := make(map[string]bool, len(order))
	for _, key := range order {
		if !seen[key] {
			result = append(result, byID[key])
			seen[key] = true
		}
	}
	return result
}

func cloneValues(source url.Values) url.Values {
	result := make(url.Values, len(source))
	for key, values := range source {
		result[key] = append([]string(nil), values...)
	}
	return result
}

func sabyValue(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func emptySabyValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return false
	}
}

func NewSabyClient(appClientID, appSecret, secretKey string, pointID, priceListID int64) *SabyClient {
	return &SabyClient{
		client: &http.Client{Timeout: 20 * time.Second},
		appClientID: strings.TrimSpace(appClientID), appSecret: strings.TrimSpace(appSecret),
		secretKey: strings.TrimSpace(secretKey), pointID: pointID, priceListID: priceListID,
		authURL: defaultSabyAuth, apiBase: defaultSabyAPI, serviceURL: defaultSabyService,
	}
}

type sabyDraftPayload struct {
	OrderID     int64  `json:"orderId"`
	OrderNumber string `json:"orderNumber"`
	Supplier    struct {
		Name  string `json:"name"`
		TaxID string `json:"taxId"`
		KPP   string `json:"kpp"`
	} `json:"supplier"`
	Lines []struct {
		SabyID   string  `json:"sabyId"`
		Code     string  `json:"code"`
		Name     string  `json:"name"`
		Quantity int     `json:"quantity"`
		NewPrice float64 `json:"newPrice"`
	} `json:"lines"`
}

type sabyRPCResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
		Details string `json:"details"`
	} `json:"error"`
}

// CreateDraft only records a Saby document. It deliberately never calls
// PrepareAction/ExecuteAction: the operator remains the only person who can
// press "Провести" and affect stock or prices.
func (client *SabyClient) CreateDraft(ctx context.Context, item procurement.ActionItem) (procurement.ActionExecution, error) {
	if !client.Configured() {
		return procurement.ActionExecution{}, errors.New("ключи Saby не настроены")
	}
	var payload sabyDraftPayload
	if err := json.Unmarshal(item.Payload, &payload); err != nil || payload.OrderID <= 0 || len(payload.Lines) == 0 {
		return procurement.ActionExecution{}, errors.New("некорректный снимок черновика Saby")
	}
	if item.Channel == "saby_receipt" && len(payload.Supplier.TaxID) == 10 && len(payload.Supplier.KPP) != 9 {
		return procurement.ActionExecution{}, errors.New("у российского поставщика не заполнен КПП")
	}
	organization, warehouse, err := client.receiptContext(ctx)
	if err != nil {
		return procurement.ActionExecution{}, err
	}
	document := map[string]any{
		"Дата": time.Now().In(time.FixedZone("MSK", 3*60*60)).Format("02.01.2006"),
		"Примечание": fmt.Sprintf("Черновик Ficusin Store, закупка №%d", payload.OrderID),
		"НашаОрганизация": organization,
	}
	lines := make([]map[string]any, 0, len(payload.Lines))
	if item.Channel == "saby_receipt" {
		document["Тип"] = "ДокОтгрВх"
		document["Название"] = "Поступление"
		document["Регламент"] = map[string]any{"Название": "Поступление"}
		counterparty := map[string]any{"Название": payload.Supplier.Name}
		if payload.Supplier.TaxID != "" {
			counterparty["ИНН"] = payload.Supplier.TaxID
		}
		if payload.Supplier.KPP != "" {
			counterparty["КПП"] = payload.Supplier.KPP
		}
		document["Контрагент"] = map[string]any{"СвЮЛ": counterparty}
		document["Склад"] = warehouse
		for _, line := range payload.Lines {
			lines = append(lines, map[string]any{
				"КодЕИ": "796", "НазваниеЕИ": "шт", "Количество": strconv.Itoa(line.Quantity),
				"НомНомер": line.Code, "Номенклатура": line.Name, "НДС": "Без НДС",
				"СуммаСебест": "0", "СуммаСебестБезНДС": "0", "СуммаСебестНДС": "0",
			})
		}
	} else if item.Channel == "saby_price" {
		priceDocType, err := client.discoverPriceDocumentType(ctx)
		if err != nil {
			return procurement.ActionExecution{}, err
		}
		document["Тип"] = priceDocType
		document["Название"] = "Изменение цен"
		document["Регламент"] = map[string]any{"Название": "Изменение цен"}
		for _, line := range payload.Lines {
			lines = append(lines, map[string]any{
				"КодЕИ": "796", "НазваниеЕИ": "шт", "НомНомер": line.Code,
				"Номенклатура": line.Name, "Цена": strconv.FormatFloat(line.NewPrice, 'f', 2, 64),
			})
		}
	} else {
		return procurement.ActionExecution{}, fmt.Errorf("канал %s не поддерживается", item.Channel)
	}
	document["Наименования"] = lines
	var written map[string]any
	if err := client.rpc(ctx, "СБИС.ЗаписатьДокумент", map[string]any{"Документ": document}, &written); err != nil {
		return procurement.ActionExecution{}, fmt.Errorf("создать черновик Saby: %w", err)
	}
	id := nestedString(written, "Идентификатор")
	if id == "" {
		return procurement.ActionExecution{}, errors.New("Saby не вернул идентификатор черновика")
	}
	return procurement.ActionExecution{Completed: true, ExternalOperationID: id, ExternalURL: safeSabyURL(nestedString(written, "СсылкаДляНашаОрганизация"))}, nil
}

// Internal price documents are not listed in Saby's public EDO type table.
// Probe price-only aliases with a read-only list call and cache the alias that
// this account accepts before writing a draft. Probing never creates or posts
// a document.
func (client *SabyClient) discoverPriceDocumentType(ctx context.Context) (string, error) {
	client.priceTypeMu.Lock()
	defer client.priceTypeMu.Unlock()
	if client.priceDocType != "" {
		return client.priceDocType, nil
	}
	candidates := []string{"АктИзмЦен", "ИзмЦен", "АктИзмененияЦен", "ИзменениеЦены", "ИзмененияЦен", "PriceChange", "PriceListChange"}
	dateTo := time.Now().In(time.FixedZone("MSK", 3*60*60))
	dateFrom := dateTo.AddDate(-5, 0, 0)
	for _, candidate := range candidates {
		var result any
		err := client.rpc(ctx, "СБИС.СписокДокументов", map[string]any{"Фильтр": map[string]any{
			"ДатаС": dateFrom.Format("02.01.2006"), "ДатаПо": dateTo.Format("02.01.2006"),
			"Тип": candidate, "Навигация": map[string]any{"РазмерСтраницы": "1"},
		}}, &result)
		if err == nil {
			client.priceDocType = candidate
			return candidate, nil
		}
		if !strings.Contains(strings.ToLower(err.Error()), "не найден тип документа") {
			return "", fmt.Errorf("определить тип документа изменения цен Saby: %w", err)
		}
	}
	return "", errors.New("Saby не предоставил API-тип документа «Изменение цен» для этого аккаунта")
}

func safeSabyURL(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "https" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "sbis.ru" && host != "saby.ru" && !strings.HasSuffix(host, ".sbis.ru") && !strings.HasSuffix(host, ".saby.ru") {
		return ""
	}
	return parsed.String()
}

func (client *SabyClient) rpc(ctx context.Context, method string, params any, target any) error {
	request := map[string]any{"jsonrpc": "2.0", "method": method, "params": params, "id": 1}
	var response sabyRPCResponse
	if err := client.authorizedJSON(ctx, http.MethodPost, client.serviceURL, request, &response); err != nil {
		return err
	}
	if response.Error != nil {
		message := strings.TrimSpace(response.Error.Message + " " + response.Error.Details)
		return fmt.Errorf("Saby RPC %v: %s", response.Error.Code, safeRemoteMessage(message))
	}
	if len(response.Result) == 0 || string(response.Result) == "null" {
		return errors.New("Saby RPC вернул пустой результат")
	}
	if err := json.Unmarshal(response.Result, target); err != nil {
		return fmt.Errorf("разобрать Saby RPC: %w", err)
	}
	return nil
}

func (client *SabyClient) receiptContext(ctx context.Context) (map[string]any, map[string]any, error) {
	var result any
	if err := client.rpc(ctx, "sabyWarehouse.List", map[string]any{"filter": map[string]any{"limit": 100, "page": 0}}, &result); err != nil {
		return nil, nil, fmt.Errorf("получить склады Saby: %w", err)
	}
	candidates := collectMaps(result)
	var selected map[string]any
	for _, candidate := range candidates {
		name := strings.ToLower(nestedString(candidate, "name") + " " + nestedString(candidate, "address"))
		if nestedString(candidate, "code") == "" || nestedString(candidate, "name") == "" {
			continue
		}
		if selected == nil {
			selected = candidate
		}
		if strings.Contains(name, "новосел") && strings.Contains(name, "40") {
			selected = candidate
			break
		}
	}
	if selected == nil {
		return nil, nil, errors.New("Saby не вернул склад для поступления")
	}
	inn := nestedString(selected, "inn")
	kpp := nestedString(selected, "kpp")
	orgName := nestedString(selected, "organisation", "name")
	if orgName == "" {
		orgName = nestedString(selected, "organization", "name")
	}
	if inn == "" {
		return nil, nil, errors.New("в складе Saby не указана наша организация")
	}
	organization := make(map[string]any)
	if len(inn) == 12 {
		parts := strings.Fields(strings.TrimPrefix(strings.TrimSpace(orgName), "ИП "))
		person := map[string]any{"ИНН": inn}
		if len(parts) > 0 { person["Фамилия"] = parts[0] }
		if len(parts) > 1 { person["Имя"] = parts[1] }
		if len(parts) > 2 { person["Отчество"] = parts[2] }
		organization["СвФЛ"] = person
	} else {
		organization["СвЮЛ"] = map[string]any{"ИНН": inn, "КПП": kpp, "Название": orgName}
	}
	warehouse := map[string]any{"Название": nestedString(selected, "name"), "Номер": nestedString(selected, "code")}
	return organization, warehouse, nil
}

func collectMaps(value any) []map[string]any {
	result := make([]map[string]any, 0)
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			result = append(result, typed)
			for _, child := range typed { walk(child) }
		case []any:
			for _, child := range typed { walk(child) }
		}
	}
	walk(value)
	return result
}

func nestedString(value any, path ...string) string {
	if len(path) == 0 {
		return strings.TrimSpace(fmt.Sprint(value))
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if strings.EqualFold(key, path[0]) {
				return nestedString(child, path[1:]...)
			}
		}
		for _, child := range typed {
			if found := nestedString(child, path...); found != "" { return found }
		}
	case []any:
		for _, child := range typed {
			if found := nestedString(child, path...); found != "" { return found }
		}
	}
	return ""
}

func (client *SabyClient) Configured() bool {
	return client != nil && client.appClientID != "" && client.appSecret != "" && client.secretKey != "" &&
		client.pointID > 0 && client.priceListID > 0
}

// Probe is deliberately read-only and requests a single catalogue row from
// the configured point and price list.
func (client *SabyClient) Probe(ctx context.Context) error {
	if !client.Configured() {
		return errors.New("ключи Saby не настроены")
	}
	query := url.Values{
		"pointId": {strconv.FormatInt(client.pointID, 10)}, "priceListId": {strconv.FormatInt(client.priceListID, 10)},
		"page": {"0"}, "pageSize": {"1"}, "noStopList": {"true"},
	}
	var response map[string]json.RawMessage
	if err := client.authorizedJSON(ctx, http.MethodGet, client.apiBase+"/retail/v2/nomenclature/list?"+query.Encode(), nil, &response); err != nil {
		return fmt.Errorf("проверить Saby: %w", err)
	}
	if _, ok := response["nomenclatures"]; !ok {
		if _, ok = response["items"]; !ok {
			if _, ok = response["result"]; !ok {
				return errors.New("Saby вернул неожиданный формат каталога")
			}
		}
	}
	return nil
}

func (client *SabyClient) authorizedJSON(ctx context.Context, method, endpoint string, payload any, target any) error {
	token, err := client.accessToken(ctx, false)
	if err != nil {
		return err
	}
	status, err := client.requestJSON(ctx, method, endpoint, payload, token, target)
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		token, authErr := client.accessToken(ctx, true)
		if authErr != nil {
			return authErr
		}
		_, err = client.requestJSON(ctx, method, endpoint, payload, token, target)
	}
	return err
}

func (client *SabyClient) accessToken(ctx context.Context, force bool) (string, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if !force && client.token != "" && time.Now().Before(client.tokenUntil) {
		return client.token, nil
	}
	payload := map[string]string{
		"app_client_id": client.appClientID, "app_secret": client.appSecret, "secret_key": client.secretKey,
	}
	var response struct { Token string `json:"token"` }
	if _, err := client.requestJSON(ctx, http.MethodPost, client.authURL, payload, "", &response); err != nil {
		return "", fmt.Errorf("авторизация Saby: %w", err)
	}
	if strings.TrimSpace(response.Token) == "" {
		return "", errors.New("авторизация Saby не вернула токен")
	}
	client.token = response.Token
	// The public API does not publish a token TTL. Reuse one session for a
	// bounded period and refresh immediately on 401/403.
	client.tokenUntil = time.Now().Add(50 * time.Minute)
	return client.token, nil
}

func (client *SabyClient) requestJSON(ctx context.Context, method, endpoint string, payload any, token string, target any) (int, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil { return 0, fmt.Errorf("encode Saby request: %w", err) }
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil { return 0, fmt.Errorf("create Saby request: %w", err) }
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		contentType := "application/json"
		if strings.Contains(endpoint, "/service/") {
			contentType = "application/json-rpc;charset=utf-8"
		}
		request.Header.Set("Content-Type", contentType)
	}
	if token != "" { request.Header.Set("X-SBISAccessToken", token) }
	response, err := client.client.Do(request)
	if err != nil { return 0, fmt.Errorf("Saby request failed: %w", err) }
	defer response.Body.Close() //nolint:errcheck
	content, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil { return response.StatusCode, fmt.Errorf("read Saby response: %w", err) }
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, &remoteError{Status: response.StatusCode, Message: sabyErrorMessage(content)}
	}
	if err := json.Unmarshal(content, target); err != nil {
		return response.StatusCode, fmt.Errorf("decode Saby response: %w", err)
	}
	return response.StatusCode, nil
}

func sabyErrorMessage(content []byte) string {
	var response sabyRPCResponse
	if json.Unmarshal(content, &response) == nil && response.Error != nil {
		message := strings.TrimSpace(response.Error.Details)
		if message == "" { message = strings.TrimSpace(response.Error.Message) }
		if message != "" { return safeRemoteMessage(message) }
	}
	return safeRemoteMessage(string(content))
}
