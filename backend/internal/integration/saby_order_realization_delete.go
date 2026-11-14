package integration

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

type SabyOrderRealizationDeleteRequest struct {
	Identity                              SabyOrderRealizationIdentity
	PublicUUID                            string
	OrganizationID, RegulationID, BuyerID int64
	ExpectedLines                         []SabyOrderRealizationPricedLine
	RowID                                 int64
}
type SabyOrderRealizationDeleteResult struct {
	Outcome    string
	HTTPStatus int
	Receipt    json.RawMessage
}

// DeleteOrderRealizationRow implements the captured CustomDocNomDelete binding.
// Acknowledgement is not retirement proof: callers must independently read the
// same document and confirm this exact row disappeared, retaining every other ID.
func (client *SabyClient) DeleteOrderRealizationRow(ctx context.Context, expected SabyOrderRealizationDeleteRequest) (SabyOrderRealizationDeleteResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result := SabyOrderRealizationDeleteResult{Outcome: "not_sent"}
	if expected.RowID <= 0 || len(expected.ExpectedLines) == 0 || len(expected.ExpectedLines) > 4096 || expected.OrganizationID <= 0 || expected.RegulationID <= 0 || expected.BuyerID <= 0 || !sabyOrderRealizationUUID.MatchString(expected.PublicUUID) {
		return result, errors.New("Не подтверждена точная собственная удаляемая строка")
	}
	found := false
	for _, line := range expected.ExpectedLines {
		if line.ID == expected.RowID {
			found = true
		}
	}
	if !found || !sabyRevisionExactIDs(sabyRevisionIDs(expected.ExpectedLines), expected.ExpectedLines) {
		return result, errors.New("Нет сохранённого подтверждения удаляемой строки")
	}
	snapshot, err := client.ReadOrderRealizationWithLines(ctx, expected.Identity)
	if err != nil {
		return result, err
	}
	face, faceErr := sabyInt64Field(snapshot.document, "Лицо3")
	if snapshot.OrganizationID != expected.OrganizationID || snapshot.RegulationID != expected.RegulationID || snapshot.BuyerID != expected.BuyerID || faceErr != nil || face != expected.OrganizationID || !strings.EqualFold(snapshot.PublicUUID, expected.PublicUUID) {
		return result, errors.New("Реквизиты удаляемой строки не совпали с собственной реализацией")
	}
	lines, err := client.ReadOrderRealizationPricedLines(ctx, expected.Identity)
	if err != nil {
		return result, err
	}
	if !sabyRealizationProviderCancelSameReads(snapshot.Lines, lines) || !sabyRevisionExactIDs(sabyRevisionIDs(expected.ExpectedLines), lines) || !sabyRealizationProviderRetainsLines(expected.ExpectedLines, lines) || !sabyRevisionOpen(snapshot, lines, false) {
		return result, errors.New("Состав или складское состояние изменилось перед удалением строки")
	}
	additional, err := sabyFillDecode([]byte(sabyOrderFillAdditionalJSON))
	if err != nil {
		return result, err
	}
	// These are the exact differences between captured Update and Delete options.
	delete(additional, "init_vat_in_init_price")
	additional["new_list"], additional["СкладскойДокумент"] = true, expected.Identity.ID
	result.Outcome = "unknown"
	result.Receipt, result.HTTPStatus, err = client.sabyOrderRealizationWriteRPCOnce(ctx, "РеалИсх.CustomDocNomDelete", map[string]any{"ИдО": []string{strconv.FormatInt(expected.RowID, 10)}, "ДопПоля": additional})
	if err != nil {
		return result, errors.New("Ответ удаления неизвестен; повторная запись запрещена")
	}
	if len(result.Receipt) > 512<<10 {
		result.Receipt = nil
		return result, errors.New("Неизвестный размер подтверждения удаления")
	}
	record, err := sabyFillDecode(result.Receipt)
	if err != nil {
		return result, err
	}
	fields, err := sabyFillSchema(record)
	if err != nil || len(fields) != 9 {
		return result, errors.New("Неизвестная схема подтверждения удаления")
	}
	for name, kind := range map[string]string{"result": "Число целое", "details": "JSON-объект", "nav_res": "Строка", "new_rs": "Выборка", "upd_rs": "Выборка", "del_rs": "Выборка", "missed_rs": "Выборка", "data": "Запись", "total_rec": "Запись"} {
		if fields[name] != kind {
			return result, errors.New("Неизвестная схема ответа удаления строки")
		}
	}
	count, err := sabyInt64Field(record, "result")
	details, ok := sabyRecordField(record, "details").(map[string]any)
	if err != nil || count != 1 || !ok || len(details) != 0 || sabyRecordField(record, "nav_res") != "" {
		return result, errors.New("СБИС не подтвердил единственное удаление строки")
	}
	for _, name := range []string{"new_rs", "upd_rs", "missed_rs", "data"} {
		if sabyRecordField(record, name) != nil {
			return result, errors.New("Удаление затронуло неподтверждённые строки")
		}
	}
	set, ok := sabyRecordField(record, "del_rs").(map[string]any)
	if !ok {
		return result, errors.New("Нет точного подтверждения удаляемого ID")
	}
	rows, err := sabyFillRecordset(set)
	if err != nil || len(rows) != 1 {
		return result, errors.New("Неверное количество удаляемых строк")
	}
	line, err := sabyFillParseLine(rows[0], expected.Identity, false)
	if err != nil || line.ID != expected.RowID || line.Closed || !sabyRealizationProviderRetainsLines([]SabyOrderRealizationPricedLine{line}, expected.ExpectedLines) {
		return result, errors.New("Подтверждение удаления относится к другой строке")
	}
	result.Outcome = "acknowledged"
	return result, nil
}
