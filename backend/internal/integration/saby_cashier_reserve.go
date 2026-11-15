package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strconv"

	"github.com/avpavlo8/ficusin-store/backend/internal/cashierstock"
)

type cashierInvoiceIdentity struct {
	ID     int64               `json:"id"`
	UUID   string              `json:"uuid"`
	Config cashierstock.Config `json:"config"`
}
type cashierInvoiceRow struct {
	ID   int64             `json:"id"`
	Line cashierstock.Line `json:"line"`
}
type cashierInvoiceGuard struct {
	Identity     cashierInvoiceIdentity `json:"identity"`
	Rows         []cashierInvoiceRow    `json:"rows"`
	Number       string                 `json:"number"`
	Marker       string                 `json:"marker"`
	TargetMarker string                 `json:"targetMarker"`
	TargetNumber string                 `json:"targetNumber"`
	NextLine     *cashierstock.Line     `json:"nextLine,omitempty"`
}
type cashierNativeParams struct {
	Guard cashierInvoiceGuard `json:"guard"`
	RPC   json.RawMessage     `json:"rpc"`
}

var errCashierRemoteRead = errors.New("Не удалось прочитать кассовый резерв СБИС")

func (c *SabyClient) cashierReadRPC(ctx context.Context, method string, params any) (json.RawMessage, error) {
	switch method {
	case "СчетСкИсх.ПрочитатьДляУчастника", "СчетСкИсх.DocNomList", "Reserve.ListByDn", "sabyWarehouse.List":
	default:
		return nil, errors.New("Неизвестное чтение кассового резерва")
	}
	var reply sabyRPCResponse
	err := c.authorizedJSON(ctx, http.MethodPost, c.serviceURL, map[string]any{"jsonrpc": "2.0", "protocol": 7, "method": method, "params": params, "id": 1}, &reply)
	if err != nil || reply.Error != nil || len(reply.Result) == 0 || string(reply.Result) == "null" {
		return nil, errCashierRemoteRead
	}
	return reply.Result, nil
}

func cashierHeader(raw json.RawMessage, expected cashierInvoiceIdentity) (map[string]any, cashierInvoiceIdentity, error) {
	record, err := sabyOrderRealizationRecord(raw, "@Документ")
	if err != nil {
		return nil, expected, err
	}
	id, e := sabyInt64Field(record, "@Документ")
	if e != nil || id <= 0 {
		return nil, expected, errors.New("Не подтверждён ID резервного счёта")
	}
	uuid, _ := sabyRecordField(record, "ИдентификаторДокумента").(string)
	if !sabyOrderRealizationUUID.MatchString(uuid) || expected.ID > 0 && (expected.ID != id || expected.UUID != uuid) {
		return nil, expected, errors.New("Изменилась привязка резервного счёта")
	}
	for name, wanted := range map[string]int64{"ТипДокумента": 231, "ДокументНашаОрганизация": expected.Config.OrganizationID, "СкладскойДокумент.Склад": expected.Config.WarehouseID, "Регламент": expected.Config.RegulationID} {
		got, e := sabyInt64Field(record, name)
		if e != nil || got != wanted || wanted <= 0 {
			return nil, expected, errors.New("Не совпали реквизиты резервного счёта")
		}
	}
	if sabyRecordField(record, "ТипДокумента.ТипДокумента") != "СчетИсх" || sabyRecordField(record, "Удален") != false {
		return nil, expected, errors.New("СБИС вернул удалённый или другой документ")
	}
	flags, err := sabyOrderRealizationFlags(record, "ДокументРасширение.Состояние")
	if err != nil {
		return nil, expected, err
	}
	for _, name := range []string{"Проведен", "ЕстьПроводки", "ОшибкиПроведения", "НеактуальныеПроводки", "ВОчередиНаПроведение", "ВОчередиНаРасчетПроводок"} {
		value, exists := flags[name]
		if !exists || value != nil && *value {
			return nil, expected, errors.New("Резервный счёт имеет складские проводки или неопределённую обработку")
		}
	}
	expected.ID, expected.UUID = id, uuid
	return record, expected, nil
}

func (c *SabyClient) cashierHeader(ctx context.Context, id cashierInvoiceIdentity) (map[string]any, error) {
	raw, err := c.cashierReadRPC(ctx, "СчетСкИсх.ПрочитатьДляУчастника", map[string]any{"ИдО": id.ID, "ИмяМетода": "СчетСкИсх.ListLite", "ДопПоля": map[string]any{"ИмяОбъекта": "СчетСкИсх"}})
	if err != nil {
		return nil, err
	}
	record, _, err := cashierHeader(raw, id)
	return record, err
}

func cashierParseRow(record map[string]any, id cashierInvoiceIdentity, prepared bool) (cashierInvoiceRow, error) {
	var row cashierInvoiceRow
	if prepared {
		if sabyRecordField(record, "@НоменклатураДокумента") != nil {
			return row, errors.New("Новая строка уже имеет ID")
		}
		if closed := sabyRecordField(record, "Закрыто"); closed != nil && closed != false {
			return row, errors.New("Новая строка закрыта")
		}
	} else {
		var err error
		row.ID, err = sabyInt64Field(record, "@НоменклатураДокумента")
		if err != nil || row.ID <= 0 || sabyRecordField(record, "Закрыто") != false {
			return row, errors.New("Не подтверждена открытая строка резервного счёта")
		}
	}
	for name, wanted := range map[string]int64{"СкладскойДокумент": id.ID, "Склад": id.Config.MovementWarehouseID, "Тип": 4} {
		got, e := sabyInt64Field(record, name)
		if e != nil || got != wanted {
			return row, errors.New("Строка принадлежит другому документу, складу или виду движения")
		}
	}
	var err error
	row.Line.NomenclatureID, err = sabyInt64Field(record, "Номенклатура")
	if err != nil || row.Line.NomenclatureID <= 0 {
		return row, errors.New("Не подтверждён товар резервной строки")
	}
	quantity, valid := new(big.Rat).SetString(sabyValue(sabyRecordField(record, "Количество")))
	if !valid || !quantity.IsInt() || !quantity.Num().IsInt64() || quantity.Sign() <= 0 || quantity.Cmp(big.NewRat(1000000, 1)) > 0 {
		return row, errors.New("Не подтверждено количество резервной строки")
	}
	row.Line.Quantity = quantity.Num().Int64()
	price, valid := new(big.Rat).SetString(sabyValue(sabyRecordField(record, "Цена")))
	sum, validSum := new(big.Rat).SetString(sabyValue(sabyRecordField(record, "СуммаЦен")))
	if !valid || !validSum || price.Sign() < 0 || sum.Cmp(new(big.Rat).Mul(price, quantity)) != 0 {
		return row, errors.New("Не совпали цена и сумма резервной строки")
	}
	row.Line.UnitPrice = price.FloatString(2)
	if exact, _ := new(big.Rat).SetString(row.Line.UnitPrice); exact.Cmp(price) != 0 {
		return row, errors.New("Цена резервной строки содержит доли копейки")
	}
	return row, nil
}

func (c *SabyClient) cashierRows(ctx context.Context, id cashierInvoiceIdentity) ([]cashierInvoiceRow, error) {
	var result []cashierInvoiceRow
	seen := map[int64]bool{}
	for page := int64(0); page < 6; page++ {
		raw, err := c.cashierReadRPC(ctx, "СчетСкИсх.DocNomList", map[string]any{"Фильтр": sabyRecord(map[string]any{"СкладскойДокумент": id.ID, "ЭтоРасходы": false, "Раздел": nil}), "Сортировка": nil, "Навигация": sabyRecord(map[string]any{"ЕстьЕще": true, "Страница": page, "РазмерСтраницы": int64(1000)}), "ДопПоля": []any{}})
		if err != nil {
			return nil, err
		}
		value, err := sabyFillDecode(raw)
		if err != nil {
			return nil, err
		}
		records, err := sabyFillRecordset(value)
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			row, e := cashierParseRow(record, id, false)
			if e != nil {
				return nil, e
			}
			if seen[row.ID] {
				return nil, errors.New("СБИС повторил строку резервного счёта")
			}
			seen[row.ID] = true
			result = append(result, row)
		}
		if len(result) > 1000 {
			return nil, errors.New("Резервный счёт содержит слишком много строк")
		}
		if len(records) < 1000 {
			return result, nil
		}
	}
	return nil, errors.New("Не удалось полностью прочитать резервный счёт")
}

func cashierSameRows(wanted, actual []cashierInvoiceRow) bool {
	if len(wanted) != len(actual) {
		return false
	}
	byID := map[int64]cashierstock.Line{}
	for _, row := range wanted {
		if row.ID <= 0 {
			return false
		}
		if _, exists := byID[row.ID]; exists {
			return false
		}
		byID[row.ID] = row.Line
	}
	for _, row := range actual {
		line, exists := byID[row.ID]
		if !exists || line != row.Line {
			return false
		}
		delete(byID, row.ID)
	}
	return len(byID) == 0
}

func (c *SabyClient) cashierCheckGuard(ctx context.Context, guard cashierInvoiceGuard) error {
	header, err := c.cashierHeader(ctx, guard.Identity)
	if err != nil {
		return err
	}
	if sabyValue(sabyRecordField(header, "Номер")) != guard.Number || sabyValue(sabyRecordField(header, "ДокументРасширение.Название")) != guard.Marker {
		return errors.New("Резервный счёт изменён вне сайта")
	}
	rows, err := c.cashierRows(ctx, guard.Identity)
	if err != nil {
		return err
	}
	if !cashierSameRows(guard.Rows, rows) {
		return errors.New("Состав резервного счёта изменён вне сайта")
	}
	return nil
}

// cashierReserveState returns empty, matching or conflicting. A matching
// reserve must be on the independently verified reserve warehouse, unshipped.
func (c *SabyClient) cashierReserveState(ctx context.Context, id cashierInvoiceIdentity, row cashierInvoiceRow) (string, error) {
	raw, err := c.cashierReadRPC(ctx, "Reserve.ListByDn", map[string]any{"DocNomIds": []int64{row.ID}})
	if err != nil {
		return "", err
	}
	value, err := sabyFillDecode(raw)
	if err != nil {
		return "", err
	}
	records, err := sabyFillRecordset(value)
	if err != nil {
		return "", err
	}
	if len(records) == 0 {
		return "empty", nil
	}
	if len(records) != 1 {
		return "conflicting", nil
	}
	record := records[0]
	for name, wanted := range map[string]int64{"@НоменклатураДокумента": row.ID, "Склад": id.Config.WarehouseID} {
		got, e := sabyInt64Field(record, name)
		if e != nil || got != wanted {
			return "conflicting", nil
		}
	}
	q, valid := new(big.Rat).SetString(sabyValue(sabyRecordField(record, "Количество")))
	shipped, validShipped := new(big.Rat).SetString(sabyValue(sabyRecordField(record, "КоличествоОтгружено")))
	if !valid || q.Cmp(big.NewRat(row.Line.Quantity, 1)) != 0 || !validShipped || shipped.Sign() != 0 || sabyRecordField(record, "Активен") != true || sabyRecordField(record, "ЕстьСписаниеОтгрузкой") != false || sabyRecordField(record, "ByBaseWh") != true {
		return "conflicting", nil
	}
	return "matching", nil
}

func cashierCommand(kind string, params any, guard cashierInvoiceGuard) (*cashierstock.Command, error) {
	rpc, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(cashierNativeParams{Guard: guard, RPC: rpc})
	if err != nil {
		return nil, err
	}
	return &cashierstock.Command{Kind: kind, Params: body}, nil
}

func cashierTyped(fields ...struct {
	name, kind string
	value      any
}) map[string]any {
	schema := make([]any, 0, len(fields))
	data := make([]any, 0, len(fields))
	for _, f := range fields {
		schema = append(schema, map[string]any{"n": f.name, "t": f.kind})
		data = append(data, f.value)
	}
	return map[string]any{"_type": "record", "s": schema, "d": data}
}
func cashierField(name, kind string, value any) struct {
	name, kind string
	value      any
} {
	return struct {
		name, kind string
		value      any
	}{name, kind, value}
}

func cashierCreateParams(r cashierstock.Request, marker string) any {
	return map[string]any{"Фильтр": cashierTyped(cashierField("ВызовИзБраузера", "Логическое", true), cashierField("ИмяОбъекта", "Строка", "СчетСкИсх"), cashierField("ТипДокумента", "Число целое", int64(231)), cashierField("Регламент", "Число целое", r.Config.RegulationID), cashierField("ДокументНашаОрганизация", "Связь", r.Config.OrganizationID), cashierField("СкладскойДокумент.Склад", "Связь", r.Config.WarehouseID), cashierField("ДокументРасширение.Название", "Строка", marker)), "ИмяМетода": "СчетСкИсх.ListLite"}
}
func cashierHeaderParams(id cashierInvoiceIdentity, number, marker string) any {
	return map[string]any{"Запись": cashierTyped(cashierField("@Документ", "Число целое", id.ID), cashierField("ИдентификаторДокумента", "UUID", id.UUID), cashierField("Номер", "Строка", number), cashierField("ДокументРасширение.Название", "Строка", marker)), "ДопПоля": map[string]any{"ИмяОбъекта": "СчетСкИсх"}}
}
func cashierPrepareRowParams(id cashierInvoiceIdentity, line cashierstock.Line) any {
	return map[string]any{"Фильтр": cashierTyped(cashierField("ВызовИзБраузера", "Логическое", true), cashierField("СкладскойДокумент", "Число целое", id.ID), cashierField("Номенклатура", "Число целое", line.NomenclatureID), cashierField("Количество", "Число целое", line.Quantity), cashierField("Цена", "Строка", line.UnitPrice)), "ИмяМетода": nil}
}

func cashierNumber(reservationID int64) string { return "FCR-" + strconv.FormatInt(reservationID, 10) }
func cashierMarker(r cashierstock.Request) string {
	state := "Резерв"
	if r.Terminal == "completed" {
		state = "Выдан кассой"
	}
	if r.Terminal == "cancelled" {
		state = "Отменён"
	}
	return fmt.Sprintf("Фикусин: %s, заказ %s. FICUSIN-CASHIER %d %s", state, r.OrderNumber, r.ReservationID, cashierstock.Fingerprint(r))
}

func cashierValidLine(line cashierstock.Line) bool {
	if line.NomenclatureID <= 0 || line.Quantity <= 0 || line.Quantity > 1000000 || len(line.UnitPrice) > 40 {
		return false
	}
	price, ok := new(big.Rat).SetString(line.UnitPrice)
	if !ok || price.Sign() < 0 {
		return false
	}
	return price.FloatString(2) == line.UnitPrice
}
