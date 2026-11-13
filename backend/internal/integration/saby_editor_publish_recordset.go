package integration

import (
	"encoding/json"
	"errors"
	"strconv"
)

type sabyEditorNativeRecordSet struct {
	raw    map[string]json.RawMessage
	schema []json.RawMessage
	rows   [][]json.RawMessage
	fields map[string]int
}

func parseSabyEditorNativeRecordSet(raw json.RawMessage) (*sabyEditorNativeRecordSet, error) {
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		return nil, errors.New("СБИС вернул неверную таблицу карточки")
	}
	var kind string
	json.Unmarshal(values["_type"], &kind)
	if kind != "recordset" {
		return nil, errors.New("СБИС вернул неизвестную таблицу карточки")
	}
	set := &sabyEditorNativeRecordSet{raw: values, fields: map[string]int{}}
	if json.Unmarshal(values["s"], &set.schema) != nil || json.Unmarshal(values["d"], &set.rows) != nil {
		return nil, errors.New("СБИС вернул неизвестную схему таблицы")
	}
	for i, field := range set.schema {
		var meta struct {
			Name string `json:"n"`
		}
		if json.Unmarshal(field, &meta) != nil || meta.Name == "" {
			return nil, errors.New("Поле таблицы СБИС без имени")
		}
		if _, ok := set.fields[meta.Name]; ok {
			return nil, errors.New("Повторяющееся поле таблицы СБИС")
		}
		set.fields[meta.Name] = i
	}
	for _, row := range set.rows {
		if len(row) != len(set.schema) {
			return nil, errors.New("Строка СБИС не соответствует схеме")
		}
	}
	return set, nil
}
func (s *sabyEditorNativeRecordSet) marshal() (json.RawMessage, error) {
	s.raw["d"], _ = json.Marshal(s.rows)
	return json.Marshal(s.raw)
}
func (s *sabyEditorNativeRecordSet) cell(row int, field string) json.RawMessage {
	if i, ok := s.fields[field]; ok && row >= 0 && row < len(s.rows) {
		return s.rows[row][i]
	}
	return nil
}
func (s *sabyEditorNativeRecordSet) set(row int, field string, value any) error {
	if i, ok := s.fields[field]; ok && row >= 0 && row < len(s.rows) {
		encoded, e := json.Marshal(value)
		if e != nil {
			return e
		}
		s.rows[row][i] = encoded
		return nil
	}
	return errors.New("СБИС не вернул схему поля таблицы")
}
func sabyEditorNativeInt(raw json.RawMessage) (int64, error) {
	var n json.Number
	if json.Unmarshal(raw, &n) != nil {
		return 0, errors.New("СБИС не вернул числовой идентификатор")
	}
	i, e := strconv.ParseInt(string(n), 10, 64)
	if e != nil {
		return 0, errors.New("СБИС вернул неверный числовой идентификатор")
	}
	return i, nil
}

// Add an EAN13 using the UI's createCodeModel/getNewCodeId algorithm. Other
// barcodes, the server's nominal number and the seller article are preserved.
func sabyEditorAddEAN13(raw json.RawMessage, barcode string) (json.RawMessage, error) {
	if err := validateSabyEditorEAN13(barcode); err != nil {
		return nil, err
	}
	set, err := parseSabyEditorNativeRecordSet(raw)
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"Id", "Code", "Type"} {
		if _, ok := set.fields[name]; !ok {
			return nil, errors.New("СБИС не вернул схему штрихкодов")
		}
	}
	next := int64(-100)
	for i := range set.rows {
		id, e := sabyEditorNativeInt(set.cell(i, "Id"))
		if e != nil {
			return nil, e
		}
		if id <= next {
			next = id - 1
		}
		var code string
		json.Unmarshal(set.cell(i, "Code"), &code)
		if code == barcode {
			typ, e := sabyEditorNativeInt(set.cell(i, "Type"))
			if e != nil || typ != 2 {
				return nil, errors.New("Штрихкод СБИС имеет другой тип")
			}
			return set.marshal()
		}
	}
	row := make([]json.RawMessage, len(set.schema))
	for i := range row {
		row[i] = json.RawMessage("null")
	}
	set.rows = append(set.rows, row)
	i := len(set.rows) - 1
	if err = set.set(i, "Id", next); err != nil {
		return nil, err
	}
	set.set(i, "Code", barcode)
	set.set(i, "Type", 2)
	return set.marshal()
}

// Patch only the configured price list and its original measure unit. The
// money schema and all other price rows remain exactly as returned by Saby.
func sabyEditorSetPrice(raw json.RawMessage, priceListID, minor int64) (json.RawMessage, error) {
	if priceListID <= 0 || minor <= 0 {
		return nil, errors.New("Нужны прайс-лист и положительная цена СБИС")
	}
	set, err := parseSabyEditorNativeRecordSet(raw)
	if err != nil {
		return nil, err
	}
	selected := -1
	for i := range set.rows {
		list, e := sabyEditorNativeInt(set.cell(i, "PriceList"))
		if e != nil {
			return nil, e
		}
		if list == priceListID {
			if selected >= 0 {
				return nil, errors.New("В прайс-листе СБИС несколько единиц цены: нужна проверка")
			}
			selected = i
		}
	}
	if selected < 0 {
		return nil, errors.New("СБИС не вернул цену выбранного прайс-листа")
	}
	var editable bool
	if json.Unmarshal(set.cell(selected, "CanEdit"), &editable) != nil || !editable {
		return nil, errors.New("СБИС запрещает изменение выбранной цены")
	}
	// json.Number keeps decimal kopecks exact on the wire, without float64.
	price := json.Number(strconv.FormatInt(minor/100, 10) + "." + leftPad2(minor%100))
	if err = set.set(selected, "Price", price); err != nil {
		return nil, err
	}
	if err = set.set(selected, "IsUpdated", true); err != nil {
		return nil, err
	}
	return set.marshal()
}
func leftPad2(n int64) string {
	if n < 10 {
		return "0" + strconv.FormatInt(n, 10)
	}
	return strconv.FormatInt(n, 10)
}

func validateSabyEditorEAN13(barcode string) error {
	if len(barcode) != 13 {
		return errors.New("Нужен штрихкод EAN-13")
	}
	sum := 0
	for i, c := range barcode {
		if c < '0' || c > '9' {
			return errors.New("Штрихкод должен содержать цифры")
		}
		weight := 1
		if i%2 == 1 {
			weight = 3
		}
		sum += int(c-'0') * weight
	}
	if sum%10 != 0 {
		return errors.New("Неверная контрольная цифра EAN-13")
	}
	return nil
}
