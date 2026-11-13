package integration

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"time"
)

// Cancel is deliberately not connected to order policy. All identifiers must
// come from a retained owned-document receipt, never a title/amount search.
type SabyOrderRealizationCancelRequest struct {
	Identity       SabyOrderRealizationIdentity
	PublicUUID     string
	OrganizationID int64
	RegulationID   int64
	BuyerID        int64
	Lines          []SabyOrderRealizationPostLine
	ExpectedRowIDs []int64 // Exact IDs from retained positive save/read receipts.
}
type SabyOrderRealizationCancelResult struct {
	// unposted confirms a fresh native document/line readback, not a physical
	// stock balance. An unknown outcome permits read-only reconciliation only.
	Outcome    string
	HTTPStatus int
	Receipt    json.RawMessage
}

// CancelOrderRealizationPosting implements only the captured protocol-7
// Документ.CancelPosting binding. The caller must durably mark its attempt
// before calling and must never replay it after an unknown result.
func (client *SabyClient) CancelOrderRealizationPosting(ctx context.Context, expected SabyOrderRealizationCancelRequest) (SabyOrderRealizationCancelResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result := SabyOrderRealizationCancelResult{Outcome: "not_sent"}
	snapshot, lines, err := client.sabyCancelReadOwned(ctx, expected)
	if err != nil {
		return result, err
	}
	if sabyCancelCleared(snapshot, lines) {
		result.Outcome = "already_unposted"
		return result, nil
	}
	if !snapshot.HeaderPosted || !snapshot.ClosedOutgoingLines {
		return result, errors.New("Складское состояние реализации не подтверждено для отмены проведения")
	}
	for _, line := range lines {
		if !line.Closed {
			return result, errors.New("Не подтверждено закрытие всех строк отменяемой реализации")
		}
	}
	params := map[string]any{"docRec": map[string]any{"_type": "record", "f": 0, "s": []any{map[string]any{"t": "Число целое", "n": "@Документ"}}, "d": []any{expected.Identity.ID}}, "eventRec": nil}
	result.Outcome = "unknown"
	result.Receipt, result.HTTPStatus, err = client.sabyOrderRealizationWriteRPCOnce(ctx, "Документ.CancelPosting", params)
	if err != nil {
		return result, errors.New("Ответ отмены проведения СБИС неизвестен; повторная запись запрещена, требуется чтение документа")
	}
	if len(result.Receipt) > 64<<10 {
		result.Receipt = nil
		return result, errors.New("Неизвестный размер подтверждения отмены; требуется чтение документа")
	}
	record, err := sabyFillDecode(result.Receipt)
	if err != nil {
		return result, errors.New("СБИС не подтвердил формат отмены проведения; требуется чтение документа")
	}
	fields, err := sabyFillSchema(record)
	if err != nil || len(fields) != 5 {
		return result, errors.New("Неизвестная схема подтверждения отмены проведения")
	}
	for _, name := range []string{"НеактуальныеПроводки", "ВОчередиНаПроведение", "ВОчередиНаРасчетПроводок", "operationResult"} {
		value, ok := sabyRecordField(record, name).(bool)
		if fields[name] != "Логическое" || !ok || value != (name == "operationResult") {
			return result, errors.New("СБИС не подтвердил завершение отмены проведения")
		}
	}
	message, ok := sabyRecordField(record, "message").(string)
	if fields["message"] != "Строка" || !ok || message != "" {
		return result, errors.New("СБИС вернул непроверенный результат отмены проведения")
	}
	priorLines := lines
	snapshot, lines, err = client.sabyCancelReadOwned(ctx, expected)
	if err != nil || !sabyCancelCleared(snapshot, lines) || !sabyCancelSameLines(priorLines, lines) {
		return result, errors.New("Отмена проведения ещё не подтверждена чтением; повторная запись запрещена")
	}
	result.Outcome = "unposted"
	return result, nil
}

func (client *SabyClient) sabyCancelReadOwned(ctx context.Context, expected SabyOrderRealizationCancelRequest) (SabyOrderRealizationSnapshot, []SabyOrderRealizationPricedLine, error) {
	var empty SabyOrderRealizationSnapshot
	wanted, err := sabyOrderRealizationPostTotals(expected.Lines)
	if err != nil || expected.OrganizationID <= 0 || expected.RegulationID <= 0 || expected.BuyerID <= 0 || !sabyOrderRealizationUUID.MatchString(expected.PublicUUID) || len(expected.ExpectedRowIDs) == 0 || len(expected.ExpectedRowIDs) > 4096 {
		return empty, nil, errors.New("Не подтверждены реквизиты и полный состав собственной реализации")
	}
	expectedIDs := map[int64]bool{}
	for _, id := range expected.ExpectedRowIDs {
		if id <= 0 || expectedIDs[id] {
			return empty, nil, errors.New("Не подтверждены сохранённые ID строк собственной реализации")
		}
		expectedIDs[id] = true
	}
	for _, line := range expected.Lines {
		if _, err = sabyFillNumber(line.UnitPrice, true); err != nil {
			return empty, nil, err
		}
	}
	snapshot, err := client.ReadOrderRealizationWithLines(ctx, expected.Identity)
	if err != nil {
		return empty, nil, err
	}
	face, faceErr := sabyInt64Field(snapshot.document, "Лицо3")
	if snapshot.OrganizationID != expected.OrganizationID || snapshot.RegulationID != expected.RegulationID || snapshot.BuyerID != expected.BuyerID || faceErr != nil || face != expected.OrganizationID || !strings.EqualFold(snapshot.PublicUUID, expected.PublicUUID) {
		return empty, nil, errors.New("Отмена остановлена: реализация относится к другим реквизитам или операции")
	}
	priced, err := client.ReadOrderRealizationPricedLines(ctx, expected.Identity)
	if err != nil {
		return empty, nil, err
	}
	byID := map[int64]SabyOrderRealizationLine{}
	for _, line := range snapshot.Lines {
		byID[line.ID] = line
	}
	actualLines := make([]SabyOrderRealizationPostLine, 0, len(priced))
	for _, line := range priced {
		if !expectedIDs[line.ID] {
			return empty, nil, errors.New("В реализации обнаружена строка без сохранённого подтверждения")
		}
		delete(expectedIDs, line.ID)
		prior, exists := byID[line.ID]
		qty, valid := new(big.Rat).SetString(string(line.Quantity))
		oldQty, oldValid := new(big.Rat).SetString(string(prior.Quantity))
		if !exists || !valid || !oldValid || qty.Cmp(oldQty) != 0 || line.NomenclatureID != prior.NomenclatureID || line.Direction != 1 || line.Direction != prior.Direction || line.Closed != prior.Closed {
			return empty, nil, errors.New("Складские строки реализации изменились между проверками")
		}
		delete(byID, line.ID)
		actualLines = append(actualLines, SabyOrderRealizationPostLine{NomenclatureID: line.NomenclatureID, Quantity: line.Quantity, UnitPrice: line.UnitPrice})
	}
	actual, err := sabyOrderRealizationPostTotals(actualLines)
	if len(expectedIDs) != 0 || len(byID) != 0 || err != nil || !sabyOrderRealizationPostSameTotals(wanted, actual) {
		return empty, nil, errors.New("Состав, цены или количества реализации не совпали; отмена не выполнялась")
	}
	return snapshot, priced, nil
}
func sabyCancelCleared(snapshot SabyOrderRealizationSnapshot, lines []SabyOrderRealizationPricedLine) bool {
	// Verified cancellation clears these flags to explicit false. A nullable
	// initial draft is not evidence that an attempted cancellation completed.
	if !snapshot.PostingStateKnown || snapshot.Posted || snapshot.HasEntries || snapshot.PostingErrors || snapshot.StaleEntries || snapshot.PostingQueued || snapshot.EntriesQueued || len(lines) == 0 {
		return false
	}
	for _, line := range lines {
		if line.Closed {
			return false
		}
	}
	return true
}

func sabyCancelSameLines(before, after []SabyOrderRealizationPricedLine) bool {
	if len(before) != len(after) {
		return false
	}
	byID := map[int64]SabyOrderRealizationPricedLine{}
	for _, line := range before {
		byID[line.ID] = line
	}
	for _, line := range after {
		old, exists := byID[line.ID]
		if !exists || old.NomenclatureID != line.NomenclatureID || old.DocumentID != line.DocumentID || old.WarehouseID != line.WarehouseID || old.Direction != line.Direction {
			return false
		}
		for _, pair := range [][2]json.Number{{old.Quantity, line.Quantity}, {old.UnitPrice, line.UnitPrice}, {old.LineSum, line.LineSum}} {
			a, av := new(big.Rat).SetString(string(pair[0]))
			b, bv := new(big.Rat).SetString(string(pair[1]))
			if !av || !bv || a.Cmp(b) != 0 {
				return false
			}
		}
		delete(byID, line.ID)
	}
	return len(byID) == 0
}
