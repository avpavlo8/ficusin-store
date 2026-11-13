package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// SabyOrderRealizationIdentity must come from a durable receipt or an explicit
// verified link. A title, order number or matching amount is never an identity.
type SabyOrderRealizationIdentity struct {
	ID                  int64
	UUID                string
	WarehouseID         int64
	MovementWarehouseID int64 // Separate verified ledger identity; never derive by arithmetic.
}

// SabyOrderRealizationSnapshot is internal integration data, not an API DTO.
// HeaderPosted proves only the native header state, not its goods/quantities
// or a change in physical stock. Those require a separate line/stock readback.
type SabyOrderRealizationSnapshot struct {
	ID                  int64
	UUID                string
	PublicUUID          string
	Type                int64
	TypeName            string
	NativeName          string
	WarehouseID         int64
	MovementWarehouseID int64
	OrganizationID      int64
	BuyerID             int64
	RegulationID        int64
	Posted              bool
	HasEntries          bool
	PostingErrors       bool
	StaleEntries        bool
	PostingQueued       bool
	EntriesQueued       bool
	PostingStateKnown   bool // False for observed nullable draft flags; never posting proof.
	HeaderPosted        bool
	Lines               []SabyOrderRealizationLine
	// ClosedOutgoingLines confirms native line state only. It does not compare
	// an order's frozen quantities or prove a before/after stock delta.
	ClosedOutgoingLines bool
	document            map[string]any
}

type SabyOrderRealizationLine struct {
	ID             int64
	NomenclatureID int64
	DocumentID     int64
	WarehouseID    int64
	Direction      int64
	Quantity       json.Number
	Closed         bool
}

var sabyOrderRealizationUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// ReadOrderRealization performs only the verified native outgoing Read RPC.
// Its integer Id is distinct from both the native UUID and public EDO UUID.
func (client *SabyClient) ReadOrderRealization(ctx context.Context, expected SabyOrderRealizationIdentity) (SabyOrderRealizationSnapshot, error) {
	if expected.ID <= 0 || expected.WarehouseID <= 0 || !sabyOrderRealizationUUID.MatchString(expected.UUID) {
		return SabyOrderRealizationSnapshot{}, errors.New("Для проверки реализации СБИС нужны сохранённые ID, UUID и склад")
	}
	if !client.Configured() {
		return SabyOrderRealizationSnapshot{}, errors.New("Подключение СБИС не настроено")
	}
	var raw json.RawMessage
	if err := client.rpc(ctx, "ДокОтгрИсх.Прочитать", map[string]any{"ИдО": expected.ID, "ИмяМетода": "ДокОтгрИсх.Список"}, &raw); err != nil {
		return SabyOrderRealizationSnapshot{}, err
	}
	record, err := sabyOrderRealizationRecord(raw, "@Документ")
	if err != nil {
		return SabyOrderRealizationSnapshot{}, err
	}
	var snapshot SabyOrderRealizationSnapshot
	if snapshot.ID, err = sabyInt64Field(record, "@Документ"); err != nil || snapshot.ID != expected.ID {
		return SabyOrderRealizationSnapshot{}, errors.New("СБИС вернул другую реализацию")
	}
	snapshot.UUID, _ = sabyRecordField(record, "ИдентификаторДокумента").(string)
	if !sabyOrderRealizationUUID.MatchString(snapshot.UUID) || !strings.EqualFold(snapshot.UUID, expected.UUID) {
		return SabyOrderRealizationSnapshot{}, errors.New("UUID реализации СБИС не совпал")
	}
	deleted, ok := sabyRecordField(record, "Удален").(bool)
	if !ok || deleted {
		return SabyOrderRealizationSnapshot{}, errors.New("Реализация СБИС удалена или её состояние удаления неизвестно")
	}
	if snapshot.Type, err = sabyInt64Field(record, "ТипДокумента"); err != nil || snapshot.Type != 226 {
		return SabyOrderRealizationSnapshot{}, errors.New("СБИС вернул другой тип складского документа")
	}
	snapshot.TypeName, _ = sabyRecordField(record, "ТипДокумента.ТипДокумента").(string)
	if snapshot.TypeName != "ДокОтгрИсх" {
		return SabyOrderRealizationSnapshot{}, errors.New("СБИС не подтвердил исходящую реализацию")
	}
	if snapshot.WarehouseID, err = sabyInt64Field(record, "СкладскойДокумент.Склад"); err != nil || snapshot.WarehouseID != expected.WarehouseID {
		return SabyOrderRealizationSnapshot{}, errors.New("Склад реализации СБИС не совпал")
	}
	snapshot.NativeName, _ = sabyRecordField(record, "Документ_").(string)
	snapshot.PublicUUID, _ = sabyRecordField(record, "ДокументРасширение.ИдентификаторВИ").(string)
	if snapshot.PublicUUID != "" && !sabyOrderRealizationUUID.MatchString(snapshot.PublicUUID) {
		return SabyOrderRealizationSnapshot{}, errors.New("СБИС вернул некорректный UUID документооборота")
	}
	for _, field := range []struct {
		name string
		id   *int64
	}{{"ДокументНашаОрганизация", &snapshot.OrganizationID}, {"Лицо1", &snapshot.BuyerID}, {"Регламент", &snapshot.RegulationID}} {
		if value := sabyRecordField(record, field.name); value != nil {
			if *field.id, err = sabyInt64Field(record, field.name); err != nil {
				return SabyOrderRealizationSnapshot{}, errors.New("СБИС вернул неверные реквизиты реализации")
			}
		}
	}
	flags, err := sabyOrderRealizationFlags(record, "ДокументРасширение.Состояние")
	if err != nil {
		return SabyOrderRealizationSnapshot{}, err
	}
	known := true
	for _, flag := range []struct {
		name  string
		value *bool
	}{{"Проведен", &snapshot.Posted}, {"ЕстьПроводки", &snapshot.HasEntries}, {"ОшибкиПроведения", &snapshot.PostingErrors}, {"НеактуальныеПроводки", &snapshot.StaleEntries}, {"ВОчередиНаПроведение", &snapshot.PostingQueued}, {"ВОчередиНаРасчетПроводок", &snapshot.EntriesQueued}} {
		value, exists := flags[flag.name]
		if !exists {
			return SabyOrderRealizationSnapshot{}, errors.New("СБИС не вернул полный складской статус реализации")
		}
		if value == nil {
			known = false
		} else {
			*flag.value = *value
		}
	}
	snapshot.PostingStateKnown = known
	snapshot.HeaderPosted = known && snapshot.Posted && snapshot.HasEntries && !snapshot.PostingErrors && !snapshot.StaleEntries && !snapshot.PostingQueued && !snapshot.EntriesQueued
	snapshot.document = record
	return snapshot, nil
}

// ReadOrderRealizationWithLines additionally reads the document-bound native
// goods list. No Create/Update/Post or payment method is reachable here.
func (client *SabyClient) ReadOrderRealizationWithLines(ctx context.Context, expected SabyOrderRealizationIdentity) (SabyOrderRealizationSnapshot, error) {
	if expected.MovementWarehouseID <= 0 {
		return SabyOrderRealizationSnapshot{}, errors.New("Для проверки движения СБИС нужен подтверждённый склад складского учёта")
	}
	snapshot, err := client.ReadOrderRealization(ctx, expected)
	if err != nil {
		return SabyOrderRealizationSnapshot{}, err
	}
	seen := map[int64]bool{}
	closed := snapshot.HeaderPosted
	snapshot.MovementWarehouseID = expected.MovementWarehouseID
	for page := int64(0); page < 100; page++ {
		var raw json.RawMessage
		// Тип=1 was verified read-only for the outgoing DocNomList contract.
		// Each returned movement still has its own independently checked Тип.
		filter := map[string]any{"_type": "record", "s": []any{
			map[string]any{"n": "СкладскойДокумент", "t": "Число целое"},
			map[string]any{"n": "Тип", "t": "Число целое"},
			map[string]any{"n": "ЭтоРасходы", "t": "Логическое"},
			map[string]any{"n": "Раздел", "t": "Число целое"},
		}, "d": []any{expected.ID, int64(1), false, nil}}
		err = client.rpc(ctx, "РеалИсх.DocNomList", map[string]any{
			"Фильтр": filter, "Сортировка": nil,
			"Навигация": sabyRecord(map[string]any{"ЕстьЕще": true, "Страница": page, "РазмерСтраницы": int64(100)}),
			"ДопПоля":   []any{},
		}, &raw)
		if err != nil {
			return SabyOrderRealizationSnapshot{}, err
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var recordSet map[string]any
		if decoder.Decode(&recordSet) != nil || decoder.Decode(new(any)) != io.EOF || recordSet["_type"] != "recordset" {
			return SabyOrderRealizationSnapshot{}, errors.New("СБИС вернул неверный список строк реализации")
		}
		fields, fieldOK := recordSet["s"].([]any)
		rows, rowsOK := recordSet["d"].([]any)
		if !fieldOK || !rowsOK || len(rows) > 100 {
			return SabyOrderRealizationSnapshot{}, errors.New("СБИС вернул неполную страницу строк реализации")
		}
		indexes := map[string]int{}
		for i, value := range fields {
			field, _ := value.(map[string]any)
			name, _ := field["n"].(string)
			if _, exists := indexes[name]; name == "" || exists {
				return SabyOrderRealizationSnapshot{}, errors.New("СБИС вернул неоднозначную схему строк реализации")
			}
			indexes[name] = i
		}
		for _, name := range []string{"@НоменклатураДокумента", "Номенклатура", "СкладскойДокумент", "Склад", "Количество", "Тип", "Закрыто", "Флаги"} {
			if _, exists := indexes[name]; !exists {
				return SabyOrderRealizationSnapshot{}, errors.New("СБИС не вернул обязательные реквизиты строк реализации")
			}
		}
		directionField := fields[indexes["Тип"]].(map[string]any)
		directionFormat, _ := directionField["t"].(map[string]any)
		directionLabels, _ := directionFormat["s"].(map[string]any)
		if directionFormat["n"] != "Перечисляемое" || directionLabels["1"] != "Расход" {
			return SabyOrderRealizationSnapshot{}, errors.New("СБИС не подтвердил схему исходящего складского движения")
		}
		for _, value := range rows {
			row, ok := value.([]any)
			if !ok || len(row) != len(fields) {
				return SabyOrderRealizationSnapshot{}, errors.New("СБИС вернул неполную строку реализации")
			}
			record := map[string]any{"_type": "record", "s": fields, "d": row}
			var line SabyOrderRealizationLine
			for _, target := range []struct {
				field string
				id    *int64
			}{{"@НоменклатураДокумента", &line.ID}, {"Номенклатура", &line.NomenclatureID}, {"СкладскойДокумент", &line.DocumentID}, {"Склад", &line.WarehouseID}} {
				if *target.id, err = sabyInt64Field(record, target.field); err != nil {
					return SabyOrderRealizationSnapshot{}, errors.New("СБИС вернул неверную привязку строки реализации")
				}
			}
			if line.DocumentID != expected.ID || line.WarehouseID != expected.MovementWarehouseID || seen[line.ID] {
				return SabyOrderRealizationSnapshot{}, errors.New("СБИС вернул чужую или повторную строку реализации")
			}
			seen[line.ID] = true
			direction, ok := row[indexes["Тип"]].(json.Number)
			if !ok {
				return SabyOrderRealizationSnapshot{}, errors.New("СБИС не подтвердил направление строки реализации")
			}
			if line.Direction, err = direction.Int64(); err != nil || snapshot.HeaderPosted && line.Direction != 1 {
				return SabyOrderRealizationSnapshot{}, errors.New("СБИС вернул строку другого складского движения")
			}
			line.Quantity, ok = row[indexes["Количество"]].(json.Number)
			quantity, valid := new(big.Rat).SetString(string(line.Quantity))
			if !ok || !valid || quantity.Sign() <= 0 {
				return SabyOrderRealizationSnapshot{}, errors.New("СБИС вернул неверное количество в реализации")
			}
			line.Closed, ok = row[indexes["Закрыто"]].(bool)
			if !ok {
				return SabyOrderRealizationSnapshot{}, errors.New("СБИС не подтвердил состояние строки реализации")
			}
			flags, e := sabyOrderRealizationFlags(record, "Флаги")
			if e != nil {
				return SabyOrderRealizationSnapshot{}, e
			}
			deleted, exists := flags["Удален"]
			if !exists || deleted != nil && *deleted {
				return SabyOrderRealizationSnapshot{}, errors.New("СБИС вернул удалённую строку реализации")
			}
			uncalculated, exists := flags["Непросчитано"]
			if !exists {
				return SabyOrderRealizationSnapshot{}, errors.New("СБИС не подтвердил расчёт строки реализации")
			}
			closed = closed && line.Closed && line.Direction == 1 && !(uncalculated != nil && *uncalculated)
			snapshot.Lines = append(snapshot.Lines, line)
		}
		if len(rows) < 100 {
			snapshot.ClosedOutgoingLines = closed && len(snapshot.Lines) > 0
			return snapshot, nil
		}
	}
	return SabyOrderRealizationSnapshot{}, errors.New("Превышен предел чтения строк реализации СБИС")
}

func sabyOrderRealizationRecord(raw []byte, key string) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var data any
	if err := decoder.Decode(&data); err != nil {
		return nil, errors.New("СБИС вернул неверный формат реализации")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("СБИС вернул несколько результатов реализации")
	}
	record := findSabyRecord(data, key)
	if record == nil || record["_type"] != "record" {
		return nil, errors.New("СБИС не вернул нативную запись реализации")
	}
	fields, fieldOK := record["s"].([]any)
	values, valueOK := record["d"].([]any)
	if !fieldOK || !valueOK || len(fields) != len(values) {
		return nil, errors.New("СБИС вернул неполную запись реализации")
	}
	seen := map[string]bool{}
	for _, raw := range fields {
		field, _ := raw.(map[string]any)
		name, _ := field["n"].(string)
		if name == "" || seen[name] {
			return nil, errors.New("СБИС вернул неоднозначную схему реализации")
		}
		seen[name] = true
	}
	return record, nil
}

// Wasaby Flags serializes values by dictionary ordinal, not by the numeric
// dictionary key. Sorting schema keys also handles sparse/reordered formats.
func sabyOrderRealizationFlags(record map[string]any, name string) (map[string]*bool, error) {
	fields, _ := record["s"].([]any)
	data, _ := record["d"].([]any)
	for i, raw := range fields {
		field, _ := raw.(map[string]any)
		if field["n"] != name || i >= len(data) {
			continue
		}
		format, _ := field["t"].(map[string]any)
		labels, labelsOK := format["s"].(map[string]any)
		values, valuesOK := data[i].([]any)
		if format["n"] != "Флаги" || !labelsOK || !valuesOK || len(labels) != len(values) {
			return nil, errors.New("СБИС вернул неполные флаги реализации")
		}
		keys := make([]int, 0, len(labels))
		for key := range labels {
			n, err := strconv.Atoi(key)
			if err != nil || n < 0 || strconv.Itoa(n) != key {
				return nil, errors.New("СБИС вернул неверную схему флагов реализации")
			}
			keys = append(keys, n)
		}
		sort.Ints(keys)
		result := map[string]*bool{}
		for ordinal, key := range keys {
			label, _ := labels[strconv.Itoa(key)].(string)
			if label == "" {
				return nil, errors.New("СБИС вернул флаг реализации без названия")
			}
			if _, duplicate := result[label]; duplicate {
				return nil, errors.New("СБИС вернул неоднозначные флаги реализации")
			}
			if values[ordinal] == nil {
				result[label] = nil
				continue
			}
			value, ok := values[ordinal].(bool)
			if !ok {
				return nil, fmt.Errorf("СБИС вернул неверное значение флага реализации %s", label)
			}
			result[label] = &value
		}
		return result, nil
	}
	return nil, errors.New("СБИС не вернул складские флаги реализации")
}
