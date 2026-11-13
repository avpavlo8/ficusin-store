package integration

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
)

// ListSabyEditorCategories uses the source contract of Saby's own category
// selector (WNCCore/categories/helpers/getSource, Types/source.passQuery).
// A failed/truncated response must not replace a previously complete cache.
func (s *SabyEditorHierarchySource) ListSabyEditorCategories(ctx context.Context) ([]ce.Category, error) {
	if s == nil || !s.Client.Configured() {
		return nil, errors.New("Подключение СБИС не настроено")
	}
	const pageSize, maxPages = 30, 100
	fields := []string{"Name", "IsFolderUI", "TypeObject", "StateSystem", "ContainsAnyItem", "HasCharacteristicAccounting", "CharacteristicsAccounting", "Deleted", "IsCategory"}
	result := []ce.Category{}
	seen := map[string]string{}
	pageIDs := map[string]bool{}
	for page := 0; page < maxPages; page++ {
		var recordset map[string]any
		params := map[string]any{
			"Фильтр":     sabyRecord(map[string]any{"GetPath": 0, "TranslitSearchString": true}),
			"Сортировка": nil,
			"Навигация":  sabyRecord(map[string]any{"Страница": page, "РазмерСтраницы": pageSize, "ЕстьЕще": true}),
			"ДопПоля":    fields,
		}
		if err := s.Client.rpc(ctx, "NomenclatureCategory.List", params, &recordset); err != nil {
			return nil, err
		}
		rows, err := sabyEditorCategoryRows(recordset)
		if err != nil {
			return nil, err
		}
		if len(rows) > pageSize {
			return nil, errors.New("СБИС превысил размер страницы категорий")
		}
		added := 0
		for _, row := range rows {
			id := sabyValue(sabyRecordField(row, "Id"))
			numeric, e := strconv.ParseInt(id, 10, 64)
			name := strings.TrimSpace(sabyValue(sabyRecordField(row, "Name")))
			if e != nil || numeric <= 0 || name == "" {
				return nil, errors.New("СБИС вернул категорию без идентификатора или названия")
			}
			if !pageIDs[id] {
				pageIDs[id] = true
				added++
			}
			// IsCategory is explicitly projected by the vendor's selector. Folder-only
			// nodes are not selectable categories; unknown schema is not guessed.
			categoryValue := sabyRecordField(row, "IsCategory")
			if categoryValue == nil {
				// Nullable hierarchy-only nodes are not selectable categories.
				// Distinguish an explicit null from an absent projection.
				schema, _ := row["s"].([]any)
				present := false
				for _, raw := range schema {
					field, _ := raw.(map[string]any)
					if field["n"] == "IsCategory" {
						present = true
						break
					}
				}
				if !present {
					return nil, errors.New("СБИС не вернул признак категории")
				}
				continue
			}
			category, ok := categoryValue.(bool)
			if !ok {
				return nil, errors.New("СБИС вернул неизвестный признак категории")
			}
			deleted, _ := sabyRecordField(row, "Deleted").(bool)
			if !category || deleted {
				continue
			}
			if prior, exists := seen[id]; exists {
				if prior != name {
					return nil, errors.New("СБИС вернул противоречивые категории")
				}
				continue
			}
			seen[id] = name
			result = append(result, ce.Category{ID: id, Name: name})
		}
		if len(rows) < pageSize {
			return result, nil
		}
		// A server ignoring navigation would otherwise loop and falsely cache a
		// truncated list. Count all IDs, including folder nodes, for page progress.
		if page > 0 && added == 0 {
			return nil, errors.New("СБИС не продвинул страницу категорий")
		}
	}
	return nil, errors.New("Список категорий СБИС превысил безопасный предел загрузки")
}

func sabyEditorCategoryRows(rs map[string]any) ([]map[string]any, error) {
	schema, ok := rs["s"].([]any)
	if !ok {
		return nil, errors.New("СБИС вернул неизвестную схему списка категорий")
	}
	data, ok := rs["d"].([]any)
	if !ok {
		return nil, errors.New("СБИС вернул неизвестные строки категорий")
	}
	rows := make([]map[string]any, 0, len(data))
	for _, raw := range data {
		values, ok := raw.([]any)
		if !ok || len(values) != len(schema) {
			return nil, fmt.Errorf("СБИС вернул неверную строку категории")
		}
		rows = append(rows, map[string]any{"s": schema, "d": values})
	}
	return rows, nil
}

func (s *SabyEditorMetadataSource) ListSabyEditorCategories(ctx context.Context) ([]ce.Category, error) {
	if s == nil || !s.Configured() {
		return nil, errors.New("Подключение СБИС не настроено")
	}
	source, ok := s.Hierarchy.(interface {
		ListSabyEditorCategories(context.Context) ([]ce.Category, error)
	})
	if !ok {
		return nil, errors.New("Справочник категорий СБИС не подключён")
	}
	return source.ListSabyEditorCategories(ctx)
}
