package integration

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
)

// WNC/operations.moveWithModifiers + beforeItemsMove are deliberately used
// instead of putting Folder into a generic existing-card Update. Vendor
// questions require a human answer and are never auto-accepted.
func (c *SabyClient) moveSabyEditorFolder(ctx context.Context, id, folder int64) error {
	if id <= 0 || folder <= 0 {
		return errors.New("Нужны числовые идентификаторы товара и раздела СБИС")
	}
	empty := sabyRecord(map[string]any{})
	check := map[string]any{"_type": "record", "s": []any{
		map[string]any{"n": "MODIFIERS_TRANSFER_QUESTION", "t": "Логическое"},
		map[string]any{"n": "@Номенклатура", "t": map[string]any{"n": "Массив", "t": "Число целое"}},
		map[string]any{"n": "@Раздел", "t": "Число целое"}, map[string]any{"n": "filter", "t": "Запись"},
	}, "d": []any{nil, []int64{id}, folder, empty}}
	var raw json.RawMessage
	if err := c.rpc(ctx, "Nomenclature.CheckBeforeMove", map[string]any{"record": check}, &raw); err != nil {
		return err
	}
	reply, err := parseSabyEditorSingleNativeRecord(raw)
	if err != nil {
		return err
	}
	questions := reply.value("questions")
	if len(questions) > 0 && string(questions) != "null" && string(questions) != "[]" {
		return errors.New("СБИС требует подтверждения условий переноса: выполните перенос в СБИС вручную")
	}
	keys := []string{strconv.FormatInt(id, 10)}
	selection := map[string]any{"_type": "record", "s": []any{
		map[string]any{"n": "marked", "t": map[string]any{"n": "Массив", "t": "Строка"}}, map[string]any{"n": "selected", "t": map[string]any{"n": "Массив", "t": "Строка"}}, map[string]any{"n": "excluded", "t": map[string]any{"n": "Массив", "t": "Строка"}}, map[string]any{"n": "recursive", "t": "Логическое"}, map[string]any{"n": "type", "t": "Строка"},
	}, "d": []any{keys, keys, []string{}, false, "all"}}
	filter := map[string]any{"_type": "record", "s": []any{map[string]any{"n": "selection", "t": "Запись"}}, "d": []any{selection}}
	return c.sabyEditorWriteRPCOnce(ctx, "Nomenclature.Move", map[string]any{"filter": filter, "folder_id": folder, "calculate_nutrition": nil}, nil)
}
