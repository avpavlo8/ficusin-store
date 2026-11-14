package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"sort"
	"strconv"
	"time"
)

type SabyOrderRealizationFillLine struct {
	NomenclatureID int64       `json:"nomenclatureId"`
	Quantity       json.Number `json:"quantity"`
	UnitPrice      json.Number `json:"unitPrice"`
}
type SabyOrderRealizationFillRequest struct {
	Identity       SabyOrderRealizationIdentity   `json:"identity"`
	OrganizationID int64                          `json:"organizationId"`
	RegulationID   int64                          `json:"regulationId"`
	BuyerID        int64                          `json:"buyerId"`
	Lines          []SabyOrderRealizationFillLine `json:"lines"`
	Next           SabyOrderRealizationFillLine   `json:"next"`
}
type SabyOrderRealizationPreparedRow struct {
	Version      int             `json:"version"`
	RequestHash  string          `json:"requestHash"`
	BaselineHash string          `json:"baselineHash"`
	RowHash      string          `json:"rowHash"`
	Row          json.RawMessage `json:"row"`
}
type SabyOrderRealizationFillResult struct {
	Outcome    string
	HTTPStatus int
	RowID      int64
	Prepared   *SabyOrderRealizationPreparedRow
	Receipt    json.RawMessage
}
type SabyOrderRealizationPricedLine struct {
	ID, NomenclatureID, DocumentID, WarehouseID, Direction int64
	Quantity, UnitPrice, LineSum                           json.Number
	Closed                                                 bool
}

// PrepareOrderRealizationRow obtains one unsaved row template. Even this
// helper mutation is sent once; a returned template must be durably retained
// before SavePreparedOrderRealizationRow is called.
func (client *SabyClient) PrepareOrderRealizationRow(ctx context.Context, expected SabyOrderRealizationFillRequest) (SabyOrderRealizationFillResult, error) {
	result := SabyOrderRealizationFillResult{Outcome: "not_sent"}
	baseline, err := client.sabyFillPreflight(ctx, expected)
	if err != nil {
		return result, err
	}
	filter, err := sabyFillDecode([]byte(sabyOrderFillCreateFilterJSON))
	if err != nil {
		return result, err
	}
	setSabyRecordField(filter, "Номенклатура", expected.Next.NomenclatureID)
	setSabyRecordField(filter, "СкладскойДокумент", expected.Identity.ID)
	setSabyRecordField(filter, "Количество", expected.Next.Quantity)
	// Цена is an explicitly captured string field of DocNomCreate's filter.
	// The returned money fields must match it; no guessed manual-price flags.
	setSabyRecordField(filter, "Цена", string(expected.Next.UnitPrice))
	result.Outcome = "unknown"
	result.Receipt, result.HTTPStatus, err = client.sabyOrderRealizationWriteRPCOnce(ctx, "РеалИсх.DocNomCreate", map[string]any{"Фильтр": filter, "ИмяМетода": nil})
	if err != nil {
		return result, err
	}
	record, err := sabyFillDecode(result.Receipt)
	if err != nil {
		return result, err
	}
	if _, err = sabyFillParseLine(record, expected.Identity, true); err != nil {
		return result, err
	}
	if !sabyFillMatchesNext(record, expected.Next) {
		return result, errors.New("СБИС подготовил другие товар, количество или цену; строка не сохранялась")
	}
	row, err := json.Marshal(record)
	if err != nil {
		return result, err
	}
	requestHash, err := sabyFillHashValue(expected)
	if err != nil {
		return result, err
	}
	rowHash, err := sabyFillHash(row)
	if err != nil {
		return result, err
	}
	result.Prepared = &SabyOrderRealizationPreparedRow{Version: 1, RequestHash: requestHash, BaselineHash: baseline, RowHash: rowHash, Row: row}
	result.Outcome = "prepared"
	return result, nil
}

// SavePreparedOrderRealizationRow sends exactly the retained native template.
// A lost, malformed or mismatched reply remains unknown, never an auto-retry.
// A saved receipt is not posting/stock proof; independent readback follows.
func (client *SabyClient) SavePreparedOrderRealizationRow(ctx context.Context, expected SabyOrderRealizationFillRequest, prepared SabyOrderRealizationPreparedRow) (SabyOrderRealizationFillResult, error) {
	result := SabyOrderRealizationFillResult{Outcome: "not_sent"}
	requestHash, err := sabyFillHashValue(expected)
	if err != nil {
		return result, err
	}
	rowHash, err := sabyFillHash(prepared.Row)
	if err != nil || prepared.Version != 1 || prepared.RequestHash != requestHash || prepared.RowHash != rowHash || prepared.BaselineHash == "" {
		return result, errors.New("Сохранённая строка реализации повреждена или относится к другому запросу")
	}
	record, err := sabyFillDecode(prepared.Row)
	if err != nil {
		return result, err
	}
	if _, err = sabyFillParseLine(record, expected.Identity, true); err != nil || !sabyFillMatchesNext(record, expected.Next) {
		return result, errors.New("Сохранённая строка не совпала с подтверждённым товаром и ценой")
	}
	baseline, err := client.sabyFillPreflight(ctx, expected)
	if err != nil {
		return result, err
	}
	if baseline != prepared.BaselineHash {
		return result, errors.New("Состав или реквизиты реализации изменились после подготовки; повторное добавление не выполнялось")
	}
	additional, err := sabyFillDecode([]byte(sabyOrderFillAdditionalJSON))
	if err != nil {
		return result, err
	}
	additional["СкладскойДокумент"] = expected.Identity.ID
	result.Outcome = "unknown"
	result.Receipt, result.HTTPStatus, err = client.sabyOrderRealizationWriteRPCOnce(ctx, "РеалИсх.CustomDocNomUpdate", map[string]any{"Запись": record, "ДопПоля": additional})
	if err != nil {
		return result, err
	}
	wrapper, err := sabyFillDecode(result.Receipt)
	if err != nil {
		return result, err
	}
	fields, err := sabyFillSchema(wrapper)
	if err != nil || len(fields) != 9 {
		return result, errors.New("Неизвестная схема подтверждения сохранения строки")
	}
	for name, kind := range map[string]string{"result": "Число целое", "details": "JSON-объект", "nav_res": "Строка", "new_rs": "Выборка", "upd_rs": "Выборка", "del_rs": "Выборка", "missed_rs": "Выборка", "data": "Запись", "total_rec": "Запись"} {
		if fields[name] != kind {
			return result, errors.New("Неполное подтверждение сохранения строки")
		}
	}
	rowID, err := sabyInt64Field(wrapper, "result")
	if err != nil || rowID <= 0 {
		return result, errors.New("СБИС не подтвердил идентификатор сохранённой строки")
	}
	for _, name := range []string{"del_rs", "missed_rs", "data"} {
		if sabyRecordField(wrapper, name) != nil {
			return result, errors.New("СБИС вернул неожиданные изменения строк реализации")
		}
	}
	for _, name := range []string{"details", "upd_rs"} {
		value := sabyRecordField(wrapper, name)
		object, ok := value.(map[string]any)
		if !ok || name == "details" && len(object) != 0 {
			return result, errors.New("СБИС вернул непроверенные изменения при сохранении строки")
		}
		if name == "upd_rs" {
			rows, err := sabyFillSaveUpdatedRows(object, sabyRecordField(wrapper, "new_rs"))
			if err != nil || len(rows) != 0 {
				return result, errors.New("СБИС изменил другие строки при сохранении")
			}
		}
	}
	rows, err := sabyFillRecordset(sabyRecordField(wrapper, "new_rs"))
	if err != nil || len(rows) != 1 {
		return result, errors.New("СБИС не подтвердил ровно одну новую строку")
	}
	line, err := sabyFillParseLine(rows[0], expected.Identity, false)
	if err != nil || line.ID != rowID || line.Closed || !sabyFillMatchesNext(rows[0], expected.Next) {
		return result, errors.New("Сохранённая строка СБИС не совпала с ожидаемой")
	}
	result.RowID, result.Outcome = rowID, "saved"
	return result, nil
}

// Protocol 7 may omit the schema of an empty upd_rs and refer to the complete
// sibling new_rs format instead (verified native save response 43560173398).
// Resolve only that exact empty compact shape. Nonempty updates, an unknown
// reference, or a malformed sibling remain unknown and must never establish IDs.
func sabyFillSaveUpdatedRows(updated map[string]any, created any) ([]map[string]any, error) {
	if _, hasSchema := updated["s"]; hasSchema {
		return sabyFillRecordset(updated)
	}
	rows, rowsOK := updated["d"].([]any)
	format, formatOK := updated["f"].(json.Number)
	formatID, formatErr := format.Int64()
	sibling, siblingOK := created.(map[string]any)
	siblingFormat, siblingFormatOK := sibling["f"].(json.Number)
	if updated["_type"] != "recordset" || len(updated) != 3 || !rowsOK || len(rows) != 0 || !formatOK || formatErr != nil || formatID < 0 || !siblingOK || !siblingFormatOK || siblingFormat != format {
		return nil, errors.New("Нет подтверждённого формата пустого списка изменённых строк")
	}
	if _, err := sabyFillRecordset(sibling); err != nil {
		return nil, err
	}
	return []map[string]any{}, nil
}

// ReadOrderRealizationPricedLines is a strictly document-bound, read-only
// price/quantity source. Its caller independently verifies the native header.
func (client *SabyClient) ReadOrderRealizationPricedLines(ctx context.Context, expected SabyOrderRealizationIdentity) ([]SabyOrderRealizationPricedLine, error) {
	if expected.ID <= 0 || expected.WarehouseID <= 0 || expected.MovementWarehouseID <= 0 || !sabyOrderRealizationUUID.MatchString(expected.UUID) {
		return nil, errors.New("Не подтверждена привязка строк к реализации и складу")
	}
	lines := []SabyOrderRealizationPricedLine{}
	seen := map[int64]bool{}
	for page := int64(0); page < 100; page++ {
		filter := sabyRecord(map[string]any{"СкладскойДокумент": expected.ID, "Тип": int64(1), "ЭтоРасходы": false, "Раздел": nil})
		// Preserve the verified nullable type, rather than sabyRecord's default.
		for _, field := range filter["s"].([]any) {
			if field.(map[string]any)["n"] == "Раздел" {
				field.(map[string]any)["t"] = "Число целое"
			}
		}
		var raw json.RawMessage
		if err := client.rpc(ctx, "РеалИсх.DocNomList", map[string]any{"Фильтр": filter, "Сортировка": nil, "Навигация": sabyRecord(map[string]any{"ЕстьЕще": true, "Страница": page, "РазмерСтраницы": int64(100)}), "ДопПоля": []any{}}, &raw); err != nil {
			return nil, err
		}
		set, err := sabyFillDecode(raw)
		if err != nil {
			return nil, err
		}
		rows, err := sabyFillRecordset(set)
		if err != nil || len(rows) > 100 {
			return nil, errors.New("СБИС вернул неверную страницу строк с ценами")
		}
		for _, row := range rows {
			line, err := sabyFillParseLine(row, expected, false)
			if err != nil {
				return nil, err
			}
			if seen[line.ID] {
				return nil, errors.New("СБИС повторил строку реализации")
			}
			seen[line.ID] = true
			lines = append(lines, line)
		}
		if len(rows) < 100 {
			return lines, nil
		}
	}
	return nil, errors.New("Список строк реализации не завершён")
}

func (client *SabyClient) sabyFillPreflight(ctx context.Context, expected SabyOrderRealizationFillRequest) (string, error) {
	wanted, err := sabyFillTotals(expected.Lines)
	if err != nil || len(wanted) == 0 || expected.OrganizationID <= 0 || expected.RegulationID <= 0 || expected.BuyerID <= 0 {
		return "", errors.New("Не подтверждены покупатель, организация или полный состав реализации")
	}
	next, err := sabyFillTotals([]SabyOrderRealizationFillLine{expected.Next})
	if err != nil {
		return "", err
	}
	q, ok := new(big.Rat).SetString(string(expected.Next.Quantity))
	if !ok || !q.IsInt() {
		return "", errors.New("Подготовка строки поддерживает целое количество товара")
	}
	snapshot, err := client.ReadOrderRealization(ctx, expected.Identity)
	if err != nil {
		return "", err
	}
	buyer, e := sabyInt64Field(snapshot.document, "Лицо1")
	face, f := sabyInt64Field(snapshot.document, "Лицо3")
	if snapshot.OrganizationID != expected.OrganizationID || snapshot.RegulationID != expected.RegulationID || e != nil || buyer != expected.BuyerID || f != nil || face != expected.OrganizationID {
		return "", errors.New("Реквизиты реализации СБИС не совпали")
	}
	date, ok := sabyRecordField(snapshot.document, "Дата").(string)
	if _, err := time.Parse("2006-01-02", date); !ok || err != nil {
		return "", errors.New("Не подтверждена дата реализации")
	}
	flags, err := sabyOrderRealizationFlags(snapshot.document, "ДокументРасширение.Состояние")
	if err != nil {
		return "", err
	}
	for _, name := range []string{"Проведен", "ЕстьПроводки", "ОшибкиПроведения", "НеактуальныеПроводки", "ВОчередиНаПроведение", "ВОчередиНаРасчетПроводок"} {
		value, exists := flags[name]
		if !exists || value != nil && *value {
			return "", errors.New("Документ уже проведён, обрабатывается или требует сверки")
		}
	}
	lines, err := client.ReadOrderRealizationPricedLines(ctx, expected.Identity)
	if err != nil {
		return "", err
	}
	actualInput := []SabyOrderRealizationFillLine{}
	for _, line := range lines {
		if line.Closed {
			return "", errors.New("В непроведённом документе обнаружена закрытая строка")
		}
		actualInput = append(actualInput, SabyOrderRealizationFillLine{NomenclatureID: line.NomenclatureID, Quantity: line.Quantity, UnitPrice: line.UnitPrice})
	}
	actual, err := sabyFillTotals(actualInput)
	if err != nil && len(lines) != 0 {
		return "", err
	}
	// Existing groups must be complete and exact. Only this whole next group
	// may be added: no silently topping up, replacing, merging or deleting rows.
	for key, quantity := range actual {
		if wanted[key] == nil || wanted[key].Cmp(quantity) != 0 {
			return "", errors.New("В документе есть лишние или изменённые товары и цены")
		}
	}
	for key, quantity := range next {
		if wanted[key] == nil || wanted[key].Cmp(quantity) != 0 || actual[key] != nil {
			return "", errors.New("Следующая строка уже есть или не совпадает с полным составом заказа")
		}
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].ID < lines[j].ID })
	return sabyFillHashValue(struct {
		Identity                        SabyOrderRealizationIdentity
		Organization, Regulation, Buyer int64
		Date                            any
		Lines                           []SabyOrderRealizationPricedLine
	}{expected.Identity, expected.OrganizationID, expected.RegulationID, expected.BuyerID, sabyRecordField(snapshot.document, "Дата"), lines})
}

func sabyFillTotals(lines []SabyOrderRealizationFillLine) (map[string]*big.Rat, error) {
	if len(lines) > 1000 {
		return nil, errors.New("Слишком большой состав реализации")
	}
	totals := map[string]*big.Rat{}
	for _, line := range lines {
		q, err := sabyFillNumber(line.Quantity, false)
		if err != nil || line.NomenclatureID <= 0 {
			return nil, errors.New("Неверный товар или количество реализации")
		}
		p, err := sabyFillNumber(line.UnitPrice, true)
		if err != nil {
			return nil, err
		}
		key := strconv.FormatInt(line.NomenclatureID, 10) + ":" + p.RatString()
		if totals[key] == nil {
			totals[key] = new(big.Rat)
		}
		totals[key].Add(totals[key], q)
	}
	return totals, nil
}
func sabyFillNumber(n json.Number, money bool) (*big.Rat, error) {
	s := string(n)
	if len(s) == 0 || len(s) > 40 || !sabyOrderRealizationPostQuantity.MatchString(s) {
		return nil, errors.New("СБИС вернул неверное число")
	}
	v, ok := new(big.Rat).SetString(s)
	if !ok || v.Sign() < 0 || !money && v.Sign() == 0 || money && !new(big.Rat).Mul(v, big.NewRat(100, 1)).IsInt() {
		return nil, errors.New("Цена или количество имеют неподдерживаемую точность")
	}
	return v, nil
}
func sabyFillParseLine(record map[string]any, expected SabyOrderRealizationIdentity, unsaved bool) (SabyOrderRealizationPricedLine, error) {
	var line SabyOrderRealizationPricedLine
	fields, err := sabyFillSchema(record)
	if err != nil {
		return line, err
	}
	for name, kind := range map[string]string{"@НоменклатураДокумента": "Число целое", "Количество": "Число вещественное", "Закрыто": "Логическое"} {
		if fields[name] != kind {
			return line, errors.New("Неизвестная схема количества или состояния строки")
		}
	}
	for _, name := range []string{"Номенклатура", "СкладскойДокумент", "Склад"} {
		kind := fields[name]
		if value, ok := kind.(map[string]any); ok {
			kind = value["n"]
		}
		if kind != "Связь" && kind != "Число целое" {
			return line, errors.New("Неизвестная схема привязки строки")
		}
	}
	for _, name := range []string{"Цена", "СуммаЦен"} {
		kind, ok := fields[name].(map[string]any)
		if !ok || kind["n"] != "Деньги" || sabyValue(kind["p"]) != "2" {
			return line, errors.New("Неизвестная денежная схема строки")
		}
	}
	direction, ok := fields["Тип"].(map[string]any)
	labels, _ := direction["s"].(map[string]any)
	if !ok || direction["n"] != "Перечисляемое" || labels["1"] != "Расход" {
		return line, errors.New("Неизвестная схема направления строки")
	}
	if unsaved {
		if sabyRecordField(record, "@НоменклатураДокумента") != nil || sabyRecordField(record, "Закрыто") != nil {
			return line, errors.New("СБИС вернул уже сохранённую строку вместо шаблона")
		}
	} else {
		var err error
		line.ID, err = sabyInt64Field(record, "@НоменклатураДокумента")
		if err != nil || line.ID <= 0 {
			return line, errors.New("Нет идентификатора товарной строки")
		}
		var ok bool
		line.Closed, ok = sabyRecordField(record, "Закрыто").(bool)
		if !ok {
			return line, errors.New("Не подтверждено состояние строки")
		}
	}
	for _, field := range []struct {
		name   string
		target *int64
	}{{"Номенклатура", &line.NomenclatureID}, {"СкладскойДокумент", &line.DocumentID}, {"Склад", &line.WarehouseID}, {"Тип", &line.Direction}} {
		var err error
		*field.target, err = sabyInt64Field(record, field.name)
		if err != nil {
			return line, err
		}
	}
	if line.NomenclatureID <= 0 || line.DocumentID != expected.ID || line.WarehouseID != expected.MovementWarehouseID || line.Direction != 1 {
		return line, errors.New("Строка относится к другому товару, документу или складу")
	}
	for _, field := range []struct {
		name   string
		target *json.Number
		money  bool
	}{{"Количество", &line.Quantity, false}, {"Цена", &line.UnitPrice, true}, {"СуммаЦен", &line.LineSum, true}} {
		value, ok := sabyRecordField(record, field.name).(json.Number)
		if !ok {
			return line, errors.New("Не подтверждены цена и количество строки")
		}
		if _, err := sabyFillNumber(value, field.money); err != nil {
			return line, err
		}
		*field.target = value
	}
	flags, err := sabyOrderRealizationFlags(record, "Флаги")
	if err != nil {
		return line, err
	}
	deleted, exists := flags["Удален"]
	if !exists || deleted != nil && *deleted {
		return line, errors.New("Строка удалена или её схема неизвестна")
	}
	q, _ := sabyFillNumber(line.Quantity, false)
	p, _ := sabyFillNumber(line.UnitPrice, true)
	sum, _ := sabyFillNumber(line.LineSum, true)
	if new(big.Rat).Mul(q, p).Cmp(sum) != 0 {
		return line, errors.New("Сумма строки не совпала с ценой и количеством")
	}
	return line, nil
}
func sabyFillMatchesNext(record map[string]any, line SabyOrderRealizationFillLine) bool {
	id, err := sabyInt64Field(record, "Номенклатура")
	if err != nil || id != line.NomenclatureID {
		return false
	}
	for _, pair := range []struct {
		name   string
		wanted json.Number
		money  bool
	}{{"Количество", line.Quantity, false}, {"Цена", line.UnitPrice, true}} {
		v, ok := sabyRecordField(record, pair.name).(json.Number)
		if !ok {
			return false
		}
		a, e := sabyFillNumber(v, pair.money)
		b, f := sabyFillNumber(pair.wanted, pair.money)
		if e != nil || f != nil || a.Cmp(b) != 0 {
			return false
		}
	}
	return true
}
func sabyFillDecode(raw []byte) (map[string]any, error) {
	if len(raw) == 0 || len(raw) > maxSabyResponse {
		return nil, errors.New("Некорректный размер нативной строки")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value map[string]any
	if d.Decode(&value) != nil || d.Decode(new(any)) != io.EOF || value == nil {
		return nil, errors.New("Некорректный JSON нативной строки")
	}
	return value, nil
}
func sabyFillRecordset(value any) ([]map[string]any, error) {
	set, ok := value.(map[string]any)
	if !ok || set["_type"] != "recordset" {
		return nil, errors.New("Нет нативного списка строк")
	}
	fields, f := set["s"].([]any)
	rows, r := set["d"].([]any)
	if !f || !r {
		return nil, errors.New("Нет полной схемы строк")
	}
	seen := map[string]bool{}
	for _, v := range fields {
		field, ok := v.(map[string]any)
		name, _ := field["n"].(string)
		if !ok || name == "" || seen[name] {
			return nil, errors.New("Неоднозначная схема строк")
		}
		seen[name] = true
	}
	result := []map[string]any{}
	for _, value := range rows {
		row, ok := value.([]any)
		if !ok || len(row) != len(fields) {
			return nil, errors.New("Неполная строка реализации")
		}
		result = append(result, map[string]any{"_type": "record", "s": fields, "d": row})
	}
	return result, nil
}
func sabyFillSchema(record map[string]any) (map[string]any, error) {
	fields, f := record["s"].([]any)
	values, d := record["d"].([]any)
	if record["_type"] != "record" || !f || !d || len(fields) != len(values) {
		return nil, errors.New("Неполная нативная запись")
	}
	result := map[string]any{}
	for _, raw := range fields {
		field, ok := raw.(map[string]any)
		name, _ := field["n"].(string)
		if _, exists := result[name]; !ok || name == "" || exists {
			return nil, errors.New("Неоднозначная схема нативной записи")
		}
		result[name] = field["t"]
	}
	return result, nil
}
func sabyFillHashValue(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return sabyFillHash(raw)
}
func sabyFillHash(raw []byte) (string, error) {
	canonical, err := canonicalSabyEditorJSON(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// Exact UI filter schema/defaults and save calculation flags, captured 13 November 2026.
const sabyOrderFillCreateFilterJSON = `{"d":[null,true,true,true,true,true,true,true,true,true,true,true,true,true,true,true,true,true,true,-1,true,true,true,true,true,true,true,true,true,true,true,true,true,true,true,true,true,true,"WHDNomList/grid:config-v1-columns-OutflowMainList",true,true,true,true,true,true,true,true,true,true,true,true,true,true,true,true,true,true,true,true,true,null,null,true,0,0,"{\"base\": {\"qty\": 1, \"abbr\": \"шт\", \"code\": \"796\", \"name\": \"Штука\"}}",null,0,null],"s":[{"t":"Строка","n":"ParcelCode"},{"t":"Логическое","n":"add_discounts_dn_info"},{"t":"Логическое","n":"add_nom_ipack"},{"t":"Логическое","n":"add_rnpt_data"},{"t":"Логическое","n":"add_rnpt_data_by_state"},{"t":"Логическое","n":"calc_access_editing_price"},{"t":"Логическое","n":"calc_actual"},{"t":"Логическое","n":"calc_auto_excise_info"},{"t":"Логическое","n":"calc_category"},{"t":"Логическое","n":"calc_category_totals"},{"t":"Логическое","n":"calc_char_groups"},{"t":"Логическое","n":"calc_characteristic_params"},{"t":"Логическое","n":"calc_content"},{"t":"Логическое","n":"calc_content_list_add_empty_record"},{"t":"Логическое","n":"calc_exp_date"},{"t":"Логическое","n":"calc_ext_params"},{"t":"Логическое","n":"calc_ext_totals"},{"t":"Логическое","n":"calc_full_sn_totals"},{"t":"Логическое","n":"calc_gift"},{"t":"Число целое","n":"calc_group"},{"t":"Логическое","n":"calc_init_measure"},{"t":"Логическое","n":"calc_is_production_configured"},{"t":"Логическое","n":"calc_item_type"},{"t":"Логическое","n":"calc_modifiers"},{"t":"Логическое","n":"calc_only_manual_parts"},{"t":"Логическое","n":"calc_outflow_diffs"},{"t":"Логическое","n":"calc_outflow_filter_totals"},{"t":"Логическое","n":"calc_outflow_product_id"},{"t":"Логическое","n":"calc_outflow_warehouse_name"},{"t":"Логическое","n":"calc_outflow_wh_names"},{"t":"Логическое","n":"calc_parts"},{"t":"Логическое","n":"calc_pics"},{"t":"Логическое","n":"calc_price_totals"},{"t":"Логическое","n":"calc_producer_info"},{"t":"Логическое","n":"calc_prt_price"},{"t":"Логическое","n":"calc_serial_number"},{"t":"Логическое","n":"calc_serial_number_inv"},{"t":"Логическое","n":"calc_traceability_visible"},{"t":"Строка","n":"column_config_id"},{"t":"Логическое","n":"create_composition_actual"},{"t":"Логическое","n":"create_composition_analogs"},{"t":"Логическое","n":"create_composition_income_settings"},{"t":"Логическое","n":"create_composition_sebest"},{"t":"Логическое","n":"create_composition_service_fields"},{"t":"Логическое","n":"extract_gtin_and_article"},{"t":"Логическое","n":"init_container_info"},{"t":"Логическое","n":"init_cost_vat_wo_recalc"},{"t":"Логическое","n":"init_currency_data"},{"t":"Логическое","n":"init_currency_totals"},{"t":"Логическое","n":"init_mercury_fields"},{"t":"Логическое","n":"init_nom"},{"t":"Логическое","n":"init_nom_product"},{"t":"Логическое","n":"init_recorded_characteristics_sets"},{"t":"Логическое","n":"init_selected_nom_set_data"},{"t":"Логическое","n":"init_vat_in_init_price_force"},{"t":"Логическое","n":"outflow_list"},{"t":"Логическое","n":"recalculate_invariants"},{"t":"Логическое","n":"set_category_in_totals"},{"t":"Логическое","n":"set_default_prices_for_present"},{"t":"Логическое","n":"set_have_expire_date_in_part"},{"t":"Строка","n":"ВходящееНаименование.Наименование"},{"t":"Строка","n":"ВходящееНаименование.НомНомер"},{"t":"Логическое","n":"ВызовИзБраузера"},{"t":"Число целое","n":"Количество"},{"t":"Число целое","n":"Номенклатура"},{"t":"Строка","n":"НоменклатураДокументаРасш.ЕИУ"},{"t":"Строка","n":"Раздел"},{"t":"Число целое","n":"СкладскойДокумент"},{"t":"Строка","n":"Цена"}],"_type":"record","f":0}`
const sabyOrderFillAdditionalJSON = `{"add_nom_ipack":true,"add_rnpt_data":true,"add_rnpt_data_by_state":true,"calc_actual":true,"calc_category":true,"calc_category_totals":true,"calc_ext_totals":true,"init_currency_totals":true,"calc_price_totals":true,"calc_char_groups":true,"calc_characteristic_params":true,"calc_exp_date":true,"calc_ext_params":true,"calc_full_sn_totals":true,"calc_gift":true,"add_discounts_dn_info":true,"calc_init_measure":true,"calc_modifiers":true,"calc_parts":true,"calc_only_manual_parts":true,"calc_producer_info":true,"calc_prt_price":true,"calc_group":-1,"extract_gtin_and_article":true,"init_currency_data":true,"init_mercury_fields":true,"init_recorded_characteristics_sets":true,"init_selected_nom_set_data":true,"init_cost_vat_wo_recalc":true,"init_vat_in_init_price_force":true,"calc_item_type":true,"outflow_list":true,"recalculate_invariants":true,"set_category_in_totals":true,"set_default_prices_for_present":true,"set_have_expire_date_in_part":true,"calc_outflow_product_id":true,"calc_is_production_configured":true,"calc_content":true,"calc_serial_number":true,"calc_serial_number_inv":true,"create_composition_service_fields":true,"init_nom":true,"init_nom_product":true,"calc_outflow_filter_totals":true,"calc_access_editing_price":true,"calc_traceability_visible":true,"calc_outflow_wh_names":true,"init_container_info":true,"calc_auto_excise_info":true,"СкладскойДокумент":0,"column_config_id":"WHDNomList/grid:config-v1-columns-OutflowMainList","init_vat_in_init_price":false,"init_linked_docs_totals":true}`
