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
	// Responses of internal warehouse methods contain the saved document and
	// all of its rows. A real receipt is routinely larger than 64 KiB; cutting
	// it at that boundary turns a successful Saby write into an ambiguous
	// "unexpected end of JSON input" on our side.
	maxSabyResponse = 8 << 20
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
	token        string
	tokenUntil   time.Time
}

type sabyCatalogRows []map[string]any

// UnmarshalJSON accepts both Saby catalogue shapes seen in production. Most
// tenants return an array, while some wrap it in an object such as
// {"nomenclatures":{"items":[...]}}. A strict []map decoder made a healthy
// integration look disconnected whenever Saby switched between the shapes.
func (rows *sabyCatalogRows) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) || len(trimmed) == 0 {
		*rows = nil
		return nil
	}
	if trimmed[0] == '[' {
		var decoded []map[string]any
		if err := json.Unmarshal(trimmed, &decoded); err != nil {
			return err
		}
		*rows = decoded
		return nil
	}
	if trimmed[0] != '{' {
		return fmt.Errorf("ожидался массив или объект каталога")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &object); err != nil {
		return err
	}
	for _, key := range []string{"items", "nomenclatures", "result", "data"} {
		if nested, exists := object[key]; exists {
			var decoded sabyCatalogRows
			if err := json.Unmarshal(nested, &decoded); err != nil {
				return err
			}
			*rows = decoded
			return nil
		}
	}
	// A single card is also a valid one-row page. Do not turn metadata-only
	// objects into phantom goods.
	if _, hasID := object["id"]; hasID {
		var item map[string]any
		if err := json.Unmarshal(trimmed, &item); err != nil {
			return err
		}
		*rows = []map[string]any{item}
		return nil
	}
	*rows = nil
	return nil
}

type sabyCatalogPage struct {
	Nomenclatures sabyCatalogRows `json:"nomenclatures"`
	Items         sabyCatalogRows `json:"items"`
	Result        sabyCatalogRows `json:"result"`
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
	return client.fetchCatalogSection(ctx, base, "", make(map[string]bool), 0, nil)
}

func (client *SabyClient) fetchCatalogSection(ctx context.Context, base url.Values, folder string, seenFolders map[string]bool, depth int, sectionPath []string) ([]map[string]any, error) {
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
			item["sectionPath"] = append([]string(nil), sectionPath...)
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
		if !sabyBool(item["isParent"]) {
			continue
		}
		folderID := sabyValue(item["hierarchicalId"])
		if folderID == "" || seenFolders[folderID] {
			continue
		}
		seenFolders[folderID] = true
		childPath := append(append([]string(nil), sectionPath...), sabyValue(item["name"]))
		children, err := client.fetchCatalogSection(ctx, base, folderID, seenFolders, depth+1, childPath)
		if err != nil {
			return nil, err
		}
		rows = append(rows, children...)
	}
	return rows, nil
}

func sabyBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		return err == nil && parsed
	default:
		return false
	}
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
			if field == "balance" {
				if !emptySabyValue(value) {
					current[field] = value
				}
				continue
			}
			// The catalogue card is Saby's master price for this store. A
			// configured price column may be channel-specific or stale, so it
			// is only a fallback when the card itself has no price.
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
		UnitCost float64 `json:"unitCost"`
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
		return procurement.ActionExecution{}, errors.New("некорректный состав документа Saby")
	}
	if item.Channel == "saby_price" {
		return procurement.ActionExecution{}, errors.New("публичный API Saby не поддерживает внутренний документ «Изменение цен»; используйте импорт XLSX в Склад → Документы → Из файла")
	}
	if item.Channel != "saby_receipt" {
		return procurement.ActionExecution{}, fmt.Errorf("канал %s не поддерживается", item.Channel)
	}

	quantities := make(map[int64]float64, len(payload.Lines))
	for _, line := range payload.Lines {
		id, err := strconv.ParseInt(strings.TrimSpace(line.SabyID), 10, 64)
		if err != nil || id <= 0 {
			return procurement.ActionExecution{}, fmt.Errorf("у товара %q нет числового идентификатора Saby", line.Name)
		}
		quantities[id] += float64(line.Quantity)
	}

	// РеалВх owns the whole receipt lifecycle. Its numeric @Документ is the only
	// stable identifier accepted by the create/read/add-row methods. Do not mix
	// this flow with СБИС.ЗаписатьДокумент: in production that public call
	// created a second empty receipt instead of updating the Retail document.
	internalID, _ := strconv.ParseInt(strings.TrimSpace(item.ExternalOperationID), 10, 64)
	link := safeSabyURL(item.ExternalURL)
	if internalID <= 0 {
		// A UUID here belongs to one of the failed public-header attempts and
		// cannot be addressed by РеалВх. Start one valid Retail receipt.
		link = ""
	}
	var document map[string]any
	savedLineCount := 0
	var err error
	if internalID > 0 {
		document, savedLineCount, err = client.readReceipt(ctx, internalID)
		if err != nil {
			execution := procurement.ActionExecution{ExternalOperationID: strconv.FormatInt(internalID, 10), ExternalURL: link}
			return execution, fmt.Errorf("открыть созданное поступление Saby %s: %w", link, err)
		}
	} else {
		filter := sabyRecord(map[string]any{"ТипДокумента": int64(224)})
		if err := client.rpc(ctx, "РеалВх.Создать", map[string]any{"Фильтр": filter, "ИмяМетода": "РеалВх.Список"}, &document); err != nil {
			return procurement.ActionExecution{}, fmt.Errorf("создать поступление Saby: %w", err)
		}
		if document["_type"] == nil {
			document["_type"] = "record"
		}
		internalID, err = sabyInt64Field(document, "@Документ")
		if err != nil {
			return procurement.ActionExecution{}, errors.New("Saby не вернул числовой идентификатор поступления")
		}
	}
	guid := strings.TrimSpace(fmt.Sprint(sabyRecordField(document, "ИдентификаторДокумента")))
	if guid == "" || guid == "<nil>" {
		return procurement.ActionExecution{ExternalOperationID: strconv.FormatInt(internalID, 10)}, errors.New("Saby не вернул UUID поступления")
	}
	if link == "" {
		link = "https://ret.saby.ru/opendoc.html?guid=" + url.QueryEscape(guid) + "&f3=259&client=43033516"
	}
	execution := procurement.ActionExecution{ExternalOperationID: strconv.FormatInt(internalID, 10), ExternalURL: link}
	setSabyRecordField(document, "Примечание", fmt.Sprintf("Ficusin Store, закупка №%d", payload.OrderID))
	if savedLineCount == 0 {
		fields := []any{
			map[string]any{"n": "Номенклатура", "t": "Число целое"},
			map[string]any{"n": "КодЕГАИС", "t": "Строка"},
			map[string]any{"n": "Количество", "t": "Число вещественное"},
			map[string]any{"n": "Раздел", "t": "Строка"},
		}
		rows := make([]any, 0, len(quantities))
		for id, quantity := range quantities {
			rows = append(rows, []any{id, "", quantity, nil})
		}
		recordSet := map[string]any{"_type": "recordset", "d": rows, "s": fields}
		var added any
		if err := client.rpc(ctx, "РеалВх.NomCreateWithSaveBatch", map[string]any{
			"doc_rec": document, "rs": recordSet,
			"actions": sabyRecord(map[string]any{"changed_document": true}),
		}, &added); err != nil {
			return execution, fmt.Errorf("добавить товары в поступление Saby %s: %w", link, err)
		}
	}
	_, finalLineCount, err := client.readReceipt(ctx, internalID)
	if err != nil {
		return execution, fmt.Errorf("проверить поступление Saby %s: %w", execution.ExternalURL, err)
	}
	if finalLineCount != len(quantities) {
		return execution, fmt.Errorf("Saby сохранил %d товарных строк из %d; документ: %s", finalLineCount, len(quantities), link)
	}
	execution.Completed = true
	return execution, nil
}

func (client *SabyClient) readReceipt(ctx context.Context, internalID int64) (map[string]any, int, error) {
	if internalID <= 0 {
		return nil, 0, errors.New("некорректный числовой идентификатор поступления Saby")
	}
	var result any
	if err := client.rpc(ctx, "ДокОтгрВх.Прочитать", map[string]any{"ИдО": internalID, "ИмяМетода": "ДокОтгрВх.Список"}, &result); err != nil {
		return nil, 0, err
	}
	document := findSabyRecord(result, "@Документ")
	if document == nil {
		return nil, 0, errors.New("Saby не вернул внутреннюю запись документа")
	}
	if document["_type"] == nil {
		document["_type"] = "record"
	}
	return document, sabyLineCount(result), nil
}

func sabyInt64Field(record map[string]any, name string) (int64, error) {
	value := strings.TrimSpace(fmt.Sprint(sabyRecordField(record, name)))
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("поле %s не является положительным Int64", name)
	}
	return id, nil
}

func findSabyRecord(value any, fieldName string) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		if sabyRecordField(typed, fieldName) != nil {
			return typed
		}
		for _, child := range typed {
			if found := findSabyRecord(child, fieldName); found != nil {
				return found
			}
		}
	case []any:
		for _, child := range typed {
			if found := findSabyRecord(child, fieldName); found != nil {
				return found
			}
		}
	}
	return nil
}

func sabyLineQuantities(value any) map[int64]float64 {
	result := make(map[int64]float64)
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			fields, _ := typed["s"].([]any)
			data, _ := typed["d"].([]any)
			nomIndex, quantityIndex := sabyFieldIndex(fields, "Номенклатура"), sabyFieldIndex(fields, "Количество")
			if nomIndex >= 0 && quantityIndex >= 0 {
				if len(data) > 0 {
					if _, recordSet := data[0].([]any); recordSet {
						for _, rawRow := range data {
							row, _ := rawRow.([]any)
							addSabyQuantity(result, row, nomIndex, quantityIndex)
						}
					} else {
						addSabyQuantity(result, data, nomIndex, quantityIndex)
					}
				}
			}
			for key, child := range typed {
				if key != "d" || nomIndex < 0 {
					walk(child)
				}
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	return result
}

// sabyLineCount deliberately ignores relation-cell contents. Their protocol-6
// shape varies between nomenclature cards, while the recordset schema and row
// count are stable enough to answer the only retry question: is the document
// empty or has the atomic batch already been written?
func sabyLineCount(value any) int {
	maximum := 0
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			fields, _ := typed["s"].([]any)
			data, _ := typed["d"].([]any)
			if sabyFieldIndex(fields, "Номенклатура") >= 0 && sabyFieldIndex(fields, "Количество") >= 0 && len(data) > 0 {
				count := 1
				if _, recordSet := data[0].([]any); recordSet {
					count = len(data)
				}
				if count > maximum {
					maximum = count
				}
			}
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	return maximum
}

func sabyFieldIndex(fields []any, name string) int {
	for index, raw := range fields {
		field, _ := raw.(map[string]any)
		if field["n"] == name {
			return index
		}
	}
	return -1
}

func addSabyQuantity(result map[int64]float64, row []any, nomIndex, quantityIndex int) {
	if nomIndex >= len(row) || quantityIndex >= len(row) {
		return
	}
	id, ok := sabyNestedInt64(row[nomIndex], "@Номенклатура", "Номенклатура", "ИдО", "id")
	if !ok || id <= 0 {
		return
	}
	quantity, ok := sabyNestedFloat(row[quantityIndex], "Количество", "quantity", "value")
	if ok {
		result[id] += quantity
	}
}

// Internal Saby recordsets do not keep relation cells scalar. Depending on
// the account and the method version, `Номенклатура` can be an Int64, a plain
// object or a nested protocol-6 record containing `@Номенклатура`. Treating
// the object as text made a successfully saved receipt look empty (`0 из 3`).
func sabyNestedInt64(value any, preferredKeys ...string) (int64, bool) {
	if number, ok := sabyScalarFloat(value); ok {
		return int64(number), number > 0 && number == float64(int64(number))
	}
	for _, key := range preferredKeys {
		if nested, ok := sabyNamedValue(value, key); ok {
			if number, found := sabyNestedInt64(nested); found {
				return number, true
			}
		}
	}
	return 0, false
}

func sabyNestedFloat(value any, preferredKeys ...string) (float64, bool) {
	if number, ok := sabyScalarFloat(value); ok {
		return number, true
	}
	for _, key := range preferredKeys {
		if nested, ok := sabyNamedValue(value, key); ok {
			if number, found := sabyNestedFloat(nested); found {
				return number, true
			}
		}
	}
	return 0, false
}

func sabyScalarFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	case string:
		number, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(typed), ",", "."), 64)
		return number, err == nil
	default:
		return 0, false
	}
}

func sabyNamedValue(value any, name string) (any, bool) {
	record, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	for key, nested := range record {
		if strings.EqualFold(key, name) {
			return nested, true
		}
	}
	fields, _ := record["s"].([]any)
	data, _ := record["d"].([]any)
	for index, raw := range fields {
		field, _ := raw.(map[string]any)
		fieldName := strings.TrimSpace(fmt.Sprint(field["n"]))
		if strings.EqualFold(fieldName, name) && index < len(data) {
			return data[index], true
		}
	}
	return nil, false
}

func sabyRecord(values map[string]any) map[string]any {
	data := make([]any, 0, len(values))
	fields := make([]any, 0, len(values))
	for name, value := range values {
		typeName := "Строка"
		switch value.(type) {
		case bool:
			typeName = "Логическое"
		case int, int64:
			typeName = "Число целое"
		case float64:
			typeName = "Число вещественное"
		}
		data = append(data, value)
		fields = append(fields, map[string]any{"n": name, "t": typeName})
	}
	return map[string]any{"_type": "record", "d": data, "s": fields}
}

func sabyRecordField(record map[string]any, name string) any {
	fields, _ := record["s"].([]any)
	data, _ := record["d"].([]any)
	for index, raw := range fields {
		field, _ := raw.(map[string]any)
		if field["n"] == name && index < len(data) {
			return data[index]
		}
	}
	return record[name]
}

func setSabyRecordField(record map[string]any, name string, value any) bool {
	fields, _ := record["s"].([]any)
	data, _ := record["d"].([]any)
	for index, raw := range fields {
		field, _ := raw.(map[string]any)
		if field["n"] == name && index < len(data) {
			data[index] = value
			record["d"] = data
			return true
		}
	}
	if _, exists := record[name]; exists {
		record[name] = value
		return true
	}
	return false
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
	request := map[string]any{"jsonrpc": "2.0", "protocol": 6, "method": method, "params": params, "id": 1}
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
	limit := int64(maxSabyResponse)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		limit = 64 << 10
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil { return response.StatusCode, fmt.Errorf("read Saby response: %w", err) }
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, &remoteError{Status: response.StatusCode, Message: sabyErrorMessage(content)}
	}
	if int64(len(content)) > limit {
		return response.StatusCode, fmt.Errorf("Saby ответил %d, но ответ длиннее %d МБ", response.StatusCode, maxSabyResponse>>20)
	}
	if len(bytes.TrimSpace(content)) == 0 {
		return response.StatusCode, fmt.Errorf("Saby ответил %d без тела ответа", response.StatusCode)
	}
	if err := json.Unmarshal(content, target); err != nil {
		return response.StatusCode, fmt.Errorf("разобрать ответ Saby (%d): %w", response.StatusCode, err)
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
