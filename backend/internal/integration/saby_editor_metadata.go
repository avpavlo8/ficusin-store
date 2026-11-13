package integration

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
)

// SabyHierarchyResolver is deliberately separate from nomenclature readback.
// Resolving a typed foreign ID needs its own verified vendor contract; an ID
// must never be converted into a name using site categories or guessed RPCs.
type SabyHierarchyResolver interface {
	ResolveSabyEditorHierarchy(context.Context, string, string) (catalogeditor.Category, error)
}

type SabyEditorHierarchySource struct{ Client *SabyClient }

func (s *SabyEditorHierarchySource) ResolveSabyEditorHierarchy(ctx context.Context, kind, id string) (catalogeditor.Category, error) {
	if s == nil || !s.Client.Configured() {
		return catalogeditor.Category{}, errors.New("Подключение СБИС не настроено")
	}
	key, err := strconv.ParseInt(id, 10, 64)
	if err != nil || key <= 0 {
		return catalogeditor.Category{}, errors.New("СБИС не вернул числовой ID иерархии")
	}
	method := "NomenclatureCategory.Read"
	params := map[string]any{"Id": key, "AdditionalFields": []string{"Name", "Identifier"}, "Option": nil}
	idField, nameField := "Id", "Name"
	if kind == "folder" {
		method, params = "Nomenclature.Read", map[string]any{"ИдО": key, "ИмяМетода": nil}
		idField, nameField = "@Номенклатура", "Наименование"
	} else if kind != "category" {
		return catalogeditor.Category{}, errors.New("Неизвестный справочник СБИС")
	}
	var record map[string]any
	if err = s.Client.rpc(ctx, method, params, &record); err != nil {
		return catalogeditor.Category{}, err
	}
	category := catalogeditor.Category{ID: sabyValue(sabyRecordField(record, idField)), Name: strings.TrimSpace(sabyValue(sabyRecordField(record, nameField)))}
	if category.ID != id || category.Name == "" {
		return catalogeditor.Category{}, errors.New("Идентификатор категории СБИС не совпал при чтении")
	}
	return category, nil
}

type SabyEditorMetadataSource struct {
	Client    *SabyClient
	Hierarchy SabyHierarchyResolver
}

func (s *SabyEditorMetadataSource) Configured() bool {
	return s != nil && s.Client.Configured()
}

func (s *SabyEditorMetadataSource) ReadSabyEditorMetadata(ctx context.Context, identity catalogeditor.SabyIdentity) (catalogeditor.SabyMetadata, error) {
	var metadata catalogeditor.SabyMetadata
	if !s.Configured() {
		return metadata, errors.New("Подключение СБИС не настроено")
	}
	id, err := strconv.ParseInt(identity.ID, 10, 64)
	if err != nil || id <= 0 {
		return metadata, errors.New("СБИС не вернул числовой ID карточки")
	}
	var record map[string]any
	if err = s.Client.rpc(ctx, "Номенклатура.Прочитать", map[string]any{"ИдО": id, "Расширение": nil}, &record); err != nil {
		return metadata, err
	}
	metadata.ID = sabyValue(sabyRecordField(record, "@Номенклатура"))
	metadata.Code = sabyValue(sabyRecordField(record, "НомНомер"))
	metadata.UUID = sabyValue(sabyRecordField(record, "Идентификатор"))
	if err = catalogeditor.ValidateSabyMetadata(identity, metadata); err != nil {
		return catalogeditor.SabyMetadata{}, err
	}
	for _, field := range []struct {
		name, kind string
		target     *catalogeditor.Category
	}{
		{"Категория", "category", &metadata.Category},
		{"Раздел", "folder", &metadata.Folder},
	} {
		foreignID, e := sabyEditorHierarchyID(record, field.name)
		if e != nil {
			return catalogeditor.SabyMetadata{}, e
		}
		if foreignID == "" || s.Hierarchy == nil {
			continue
		}
		resolved, e := s.Hierarchy.ResolveSabyEditorHierarchy(ctx, field.kind, foreignID)
		if e != nil {
			continue
		}
		if resolved.ID != foreignID || strings.TrimSpace(resolved.Name) == "" {
			return catalogeditor.SabyMetadata{}, errors.New("СБИС вернул неверное название иерархии")
		}
		*field.target = resolved
	}
	return metadata, nil
}

// Nomenclature protocol-6 hierarchy references are singleton identifier lists,
// not record IDs taken from a translated name or from the nullable @/$ fields.
func sabyEditorHierarchyID(record map[string]any, name string) (string, error) {
	fields, _ := record["s"].([]any)
	values, _ := record["d"].([]any)
	for index, raw := range fields {
		field, _ := raw.(map[string]any)
		if field["n"] != name {
			continue
		}
		if index >= len(values) || values[index] == nil {
			return "", nil
		}
		if field["t"] != "Идентификатор" || field["s"] != "Иерархия" {
			return "", fmt.Errorf("Неподдерживаемая ссылка СБИС: %s", name)
		}
		ids, ok := values[index].([]any)
		if !ok || len(ids) > 1 {
			return "", errors.New("СБИС вернул неоднозначный идентификатор иерархии")
		}
		if len(ids) == 0 {
			return "", nil
		}
		id, err := strconv.ParseInt(sabyValue(ids[0]), 10, 64)
		if err != nil || id <= 0 {
			return "", errors.New("СБИС вернул неверный идентификатор иерархии")
		}
		return strconv.FormatInt(id, 10), nil
	}
	return "", nil
}
