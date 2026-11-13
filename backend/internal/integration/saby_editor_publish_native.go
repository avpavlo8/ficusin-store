package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// Native protocol-6 records must retain the vendor's field schemas, including
// identifier references and nested recordsets. Converting them to inferred Go
// scalar types can silently change the meaning of an Update request.
type sabyEditorNativeRecord struct {
	raw    map[string]json.RawMessage
	schema []json.RawMessage
	values []json.RawMessage
	fields map[string]int
}

func parseSabyEditorNativeRecord(data []byte) (*sabyEditorNativeRecord, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, errors.New("СБИС вернул неверную карточку")
	}
	var kind string
	if err := json.Unmarshal(raw["_type"], &kind); err != nil || kind != "record" {
		return nil, errors.New("СБИС вернул неизвестный формат карточки")
	}
	r := &sabyEditorNativeRecord{raw: raw, fields: map[string]int{}}
	if json.Unmarshal(raw["s"], &r.schema) != nil || json.Unmarshal(raw["d"], &r.values) != nil || len(r.schema) != len(r.values) {
		return nil, errors.New("СБИС вернул несовместимую схему карточки")
	}
	for i, field := range r.schema {
		var meta struct {
			Name string `json:"n"`
		}
		if json.Unmarshal(field, &meta) != nil || meta.Name == "" {
			return nil, errors.New("СБИС вернул поле без имени")
		}
		if _, exists := r.fields[meta.Name]; exists {
			return nil, errors.New("СБИС вернул повторяющееся поле карточки")
		}
		r.fields[meta.Name] = i
	}
	return r, nil
}
func (r *sabyEditorNativeRecord) value(name string) json.RawMessage {
	if i, ok := r.fields[name]; ok {
		return r.values[i]
	}
	return nil
}
func (r *sabyEditorNativeRecord) scalarString(name string) (string, error) {
	value := r.value(name)
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return "", nil
	}
	var s string
	if json.Unmarshal(value, &s) == nil {
		return s, nil
	}
	var n json.Number
	if json.Unmarshal(value, &n) == nil {
		if _, err := strconv.ParseInt(string(n), 10, 64); err == nil {
			return string(n), nil
		}
	}
	return "", fmt.Errorf("СБИС вернул неподдерживаемый тип поля %s", name)
}

// selectFields serializes only explicitly-owned fields using the read schema.
// No guessed schema is added for an absent field.
func (r *sabyEditorNativeRecord) selectFields(values map[string]json.RawMessage) (json.RawMessage, error) {
	schema := []json.RawMessage{}
	data := []json.RawMessage{}
	for i, raw := range r.schema {
		var field struct {
			Name string `json:"n"`
		}
		_ = json.Unmarshal(raw, &field)
		if value, ok := values[field.Name]; ok {
			if !json.Valid(value) {
				return nil, errors.New("Некорректное значение карточки СБИС")
			}
			schema = append(schema, r.schema[i])
			data = append(data, value)
		}
	}
	if len(schema) != len(values) {
		return nil, errors.New("СБИС не вернул схему изменяемого поля")
	}
	return json.Marshal(map[string]any{"_type": "record", "s": schema, "d": data})
}

// Read binding is verbatim LocalCatalogCard/Nomenclature/Loader.passNomRead.
// Folder is a base property; it is not a default AdditionalFields projection.
func (c *SabyClient) readSabyEditorNativeCard(ctx context.Context, id int64) (*sabyEditorNativeRecord, error) {
	if id <= 0 {
		return nil, errors.New("Нужен числовой идентификатор карточки СБИС")
	}
	fields := sabyEditorCardFields()
	option := sabyRecord(map[string]any{})
	if c.priceListID > 0 {
		option = map[string]any{"_type": "record", "s": []any{map[string]any{"n": "PriceList", "t": map[string]any{"n": "Массив", "t": "Число целое"}}}, "d": []any{[]int64{c.priceListID}}}
	}
	var raw json.RawMessage
	if err := c.rpc(ctx, "Nomenclature.Read", map[string]any{"Id": id, "AdditionalFields": fields, "Option": option}, &raw); err != nil {
		return nil, err
	}
	record, err := parseSabyEditorNativeRecord(raw)
	if err != nil {
		return nil, err
	}
	got, err := record.scalarString("Id")
	if err != nil || got != strconv.FormatInt(id, 10) {
		return nil, errors.New("Идентификатор карточки СБИС не совпал при чтении")
	}
	return record, nil
}

// SbisService.call(...).getRow() accepts one native record or a one-row
// recordset. Never select the first row from an ambiguous upload result.
func parseSabyEditorSingleNativeRecord(raw []byte) (*sabyEditorNativeRecord, error) {
	var value map[string]json.RawMessage
	if json.Unmarshal(raw, &value) != nil {
		return nil, errors.New("СБИС вернул неизвестный результат")
	}
	var kind string
	_ = json.Unmarshal(value["_type"], &kind)
	if kind == "record" {
		return parseSabyEditorNativeRecord(raw)
	}
	if kind != "recordset" {
		return nil, errors.New("СБИС вернул неизвестный результат")
	}
	var rows []json.RawMessage
	if json.Unmarshal(value["d"], &rows) != nil || len(rows) != 1 {
		return nil, errors.New("СБИС вернул неоднозначный результат")
	}
	record, err := json.Marshal(map[string]any{"_type": "record", "s": value["s"], "d": rows[0]})
	if err != nil {
		return nil, err
	}
	return parseSabyEditorNativeRecord(record)
}
