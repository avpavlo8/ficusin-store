package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"regexp"
	"time"
)

type SabyOrderRealizationPostLine struct {
	NomenclatureID int64
	Quantity       json.Number
	UnitPrice      json.Number // Exact rubles, including an explicit zero.
}

// All expected identities and quantities must come from the caller's durable
// order/document mapping, never from a name/amount search or an arbitrary record.
type SabyOrderRealizationPostRequest struct {
	Identity       SabyOrderRealizationIdentity
	OrganizationID int64
	RegulationID   int64
	BuyerID        int64
	Lines          []SabyOrderRealizationPostLine
}

type SabyOrderRealizationPostResult struct {
	// queued means waiting for independent readback, NOT confirmed posting.
	// unknown must never trigger a blind retry; rejected is a vendor check result.
	Outcome    string
	Code       string
	HTTPStatus int
	Receipt    json.RawMessage // Internal only: may contain vendor/business details.
}

var sabyOrderRealizationPostQuantity = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

// PostOrderRealization implements the exact protocol-7 Документ.PostAsync UI
// binding. The worker must durably mark its post attempt BEFORE this call and
// reconcile by readback after a lost response. This method has no replay path.
func (client *SabyClient) PostOrderRealization(ctx context.Context, expected SabyOrderRealizationPostRequest) (SabyOrderRealizationPostResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	result := SabyOrderRealizationPostResult{Outcome: "not_sent"}
	wanted, err := sabyOrderRealizationPostTotals(expected.Lines)
	if err != nil || expected.OrganizationID <= 0 || expected.RegulationID <= 0 || expected.BuyerID <= 0 {
		return result, errors.New("Для проведения нужны подтверждённые организация, операция, покупатель и состав реализации")
	}
	snapshot, err := client.ReadOrderRealizationWithLines(ctx, expected.Identity)
	if err != nil {
		return result, err
	}
	if snapshot.OrganizationID != expected.OrganizationID || snapshot.RegulationID != expected.RegulationID {
		return result, errors.New("Организация или операция реализации СБИС не совпала")
	}
	if snapshot.BuyerID != expected.BuyerID {
		return result, errors.New("Покупатель реализации СБИС не совпал")
	}
	priced, err := client.ReadOrderRealizationPricedLines(ctx, expected.Identity)
	if err != nil {
		return result, err
	}
	base := make(map[int64]SabyOrderRealizationLine, len(snapshot.Lines))
	for _, line := range snapshot.Lines {
		base[line.ID] = line
	}
	actualLines := make([]SabyOrderRealizationPostLine, 0, len(priced))
	for _, line := range priced {
		prior, exists := base[line.ID]
		q, qOK := new(big.Rat).SetString(string(line.Quantity))
		oldQ, oldOK := new(big.Rat).SetString(string(prior.Quantity))
		if !exists || line.Direction != 1 || line.NomenclatureID != prior.NomenclatureID || !qOK || !oldOK || q.Cmp(oldQ) != 0 || line.Closed != prior.Closed {
			return result, errors.New("Строки реализации СБИС изменились во время проверки")
		}
		delete(base, line.ID)
		actualLines = append(actualLines, SabyOrderRealizationPostLine{NomenclatureID: line.NomenclatureID, Quantity: line.Quantity, UnitPrice: line.UnitPrice})
	}
	if len(base) != 0 {
		return result, errors.New("Состав реализации СБИС изменился во время проверки")
	}
	actual, err := sabyOrderRealizationPostTotals(actualLines)
	if err != nil || !sabyOrderRealizationPostSameTotals(wanted, actual) {
		return result, errors.New("Состав или количество реализации СБИС не совпало с заказом")
	}
	if snapshot.HeaderPosted && snapshot.ClosedOutgoingLines {
		result.Outcome = "already_posted"
		return result, nil
	}
	if snapshot.PostingQueued || snapshot.EntriesQueued {
		result.Outcome = "queued"
		return result, nil
	}
	if snapshot.Posted || snapshot.HasEntries || snapshot.PostingErrors || snapshot.StaleEntries {
		return result, errors.New("Складское состояние реализации требует проверки перед проведением")
	}
	params, err := sabyOrderRealizationPostParams(snapshot)
	if err != nil {
		return result, err
	}
	result.Outcome = "unknown"
	result.Receipt, result.HTTPStatus, err = client.sabyOrderRealizationWriteRPCOnce(ctx, "Документ.PostAsync", params)
	if err != nil {
		return result, errors.New("Ответ на проведение СБИС не подтверждён; требуется проверка документа без повторной отправки")
	}
	if len(result.Receipt) > 64<<10 {
		result.Receipt = nil
		return result, errors.New("Подтверждение проведения СБИС превышает допустимый размер; требуется проверка документа")
	}
	decoded := bytes.TrimSpace(result.Receipt)
	if len(decoded) == 0 || bytes.Equal(decoded, []byte("null")) {
		return result, errors.New("СБИС не вернул подтверждение запроса проведения")
	}
	// The live EMPTY_NOM reply is a JSON object encoded inside a JSON string.
	if decoded[0] == '"' {
		var nested string
		if json.Unmarshal(decoded, &nested) != nil {
			return result, errors.New("Не удалось прочитать ответ проведения СБИС")
		}
		decoded = []byte(nested)
	}
	var receipt struct {
		Result string `json:"result"`
		Detail []struct {
			CheckType string `json:"check_type"`
		} `json:"detail"`
	}
	if json.Unmarshal(decoded, &receipt) != nil {
		return result, errors.New("Формат подтверждения проведения СБИС неизвестен; требуется проверка документа")
	}
	if receipt.Result == "error" {
		result.Outcome = "rejected"
		for _, detail := range receipt.Detail {
			if detail.CheckType == "EMPTY_NOM" {
				result.Code = "EMPTY_NOM"
			}
		}
		return result, errors.New("СБИС отклонил проверку проведения; документ не подтверждён как проведённый")
	}
	// No success body is treated as stock proof. A returned asynchronous receipt
	// is retained for the worker's independent header/line/stock reconciliation.
	result.Outcome = "queued"
	return result, nil
}

// Shared with the separately captured outgoing line bindings. This transport
// preserves the cached service session/limiter, but never refreshes/replays a
// write or follows a redirect with credentials. Only proven methods are allowed.
func (client *SabyClient) sabyOrderRealizationWriteRPCOnce(ctx context.Context, method string, params any) (json.RawMessage, int, error) {
	switch method {
	case "Документ.PostAsync", "Документ.CancelPosting", "РеалИсх.DocNomCreate", "РеалИсх.CustomDocNomUpdate", "РеалИсх.CustomDocNomDelete", "РеалИсх.Записать":
	default:
		return nil, 0, errors.New("Неподдерживаемое изменение реализации СБИС")
	}
	if !client.Configured() {
		return nil, 0, errors.New("Подключение СБИС не настроено")
	}
	token, err := client.accessToken(ctx, false)
	if err != nil {
		return nil, 0, errors.New("Не удалось получить сервисную сессию СБИС")
	}
	transport := &http.Client{Transport: client.client.Transport, Timeout: client.client.Timeout, Jar: client.client.Jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	writer := &SabyClient{client: transport, requestLimiter: client.requestLimiter}
	var response sabyRPCResponse
	request := map[string]any{"jsonrpc": "2.0", "protocol": 7, "method": method, "params": params, "id": 1}
	status, err := writer.requestJSON(ctx, http.MethodPost, client.serviceURL, request, token, &response)
	if err != nil {
		return nil, status, errors.New("Ответ изменения СБИС не подтверждён")
	}
	if response.Error != nil {
		return nil, status, errors.New("СБИС вернул ошибку изменения реализации")
	}
	return append(json.RawMessage(nil), response.Result...), status, nil
}

type sabyOrderRealizationPostItem struct {
	nomenclatureID int64
	unitPrice      string
}

func sabyOrderRealizationPostTotals(lines []SabyOrderRealizationPostLine) (map[sabyOrderRealizationPostItem]*big.Rat, error) {
	if len(lines) == 0 || len(lines) > 1000 {
		return nil, errors.New("Неверный состав реализации")
	}
	totals := make(map[sabyOrderRealizationPostItem]*big.Rat, len(lines))
	for _, line := range lines {
		q := string(line.Quantity)
		if line.NomenclatureID <= 0 || len(q) > 40 || !sabyOrderRealizationPostQuantity.MatchString(q) {
			return nil, errors.New("Неверное количество реализации")
		}
		value, ok := new(big.Rat).SetString(q)
		if !ok || value.Sign() <= 0 {
			return nil, errors.New("Неверное количество реализации")
		}
		p := string(line.UnitPrice)
		if len(p) > 40 || !sabyOrderRealizationPostQuantity.MatchString(p) {
			return nil, errors.New("Неверная цена реализации")
		}
		price, valid := new(big.Rat).SetString(p)
		if !valid || price.Sign() < 0 {
			return nil, errors.New("Неверная цена реализации")
		}
		key := sabyOrderRealizationPostItem{line.NomenclatureID, price.RatString()}
		if totals[key] == nil {
			totals[key] = new(big.Rat)
		}
		totals[key].Add(totals[key], value)
	}
	return totals, nil
}

func sabyOrderRealizationPostSameTotals(wanted, actual map[sabyOrderRealizationPostItem]*big.Rat) bool {
	if len(wanted) != len(actual) {
		return false
	}
	for id, value := range wanted {
		if actual[id] == nil || value.Cmp(actual[id]) != 0 {
			return false
		}
	}
	return true
}

func sabyOrderRealizationPostParams(snapshot SabyOrderRealizationSnapshot) (map[string]any, error) {
	date, _ := sabyRecordField(snapshot.document, "Дата").(string)
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return nil, errors.New("СБИС не вернул дату реализации")
	}
	face, err := sabyInt64Field(snapshot.document, "Лицо3")
	if err != nil || face != snapshot.OrganizationID {
		return nil, errors.New("СБИС не подтвердил организацию отгрузки")
	}
	buyer, err := sabyInt64Field(snapshot.document, "Лицо1")
	if err != nil || buyer <= 0 {
		return nil, errors.New("СБИС не вернул покупателя")
	}
	// Preserve the exact captured field order/types; never send the entire fresh
	// header, which includes unrelated mutable fields and personal information.
	names := []string{"@Документ", "ТипДокумента", "Дата", "ДокументНашаОрганизация", "Лицо3", "Лицо3.Название", "Лицо1", "СкладскойДокумент.Склад", "isAcceptance", "errJournalEnabled"}
	types := []string{"Число целое", "Связь", "Дата", "Связь", "Связь", "Связь", "Связь", "Связь", "Логическое", "Логическое"}
	fields := make([]any, len(names))
	for i, name := range names {
		fields[i] = map[string]any{"t": types[i], "n": name}
	}
	// The UI sends no display name in this relation slot, even though the fresh
	// header may contain it. Bind the verified numeric organization only.
	doc := map[string]any{"_type": "record", "f": 2, "s": fields,
		"d": []any{snapshot.ID, snapshot.Type, date, snapshot.OrganizationID, face, nil, buyer, snapshot.WarehouseID, nil, true}}
	checks := map[string]any{"_type": "record", "f": 1, "s": []any{
		map[string]any{"t": "Запись", "n": "DocRec"}, map[string]any{"t": map[string]any{"n": "Массив", "t": "Строка"}, "n": "Operation"}}, "d": []any{doc, []string{"posting"}}}
	outer := map[string]any{"_type": "record", "f": 0, "s": []any{
		map[string]any{"t": "Число целое", "n": "@Документ"}, map[string]any{"t": "Запись", "n": "CheckSettings"}, map[string]any{"t": "Логическое", "n": "skipCheck"}}, "d": []any{snapshot.ID, checks, nil}}
	return map[string]any{"params": outer}, nil
}
