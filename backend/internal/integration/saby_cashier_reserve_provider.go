package integration

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/cashierstock"
)

type SabyCashierReserveProvider struct{ Client *SabyClient }
type cashierReserveReceipt struct {
	Version     int                    `json:"version"`
	RequestHash string                 `json:"requestHash"`
	Identity    cashierInvoiceIdentity `json:"identity"`
	Rows        []cashierInvoiceRow    `json:"rows"`
	Number      string                 `json:"number"`
	Marker      string                 `json:"marker"`
	Phase       string                 `json:"phase"`
	Index       int                    `json:"index"`
	Polls       int                    `json:"polls"`
	Prepared    json.RawMessage        `json:"prepared,omitempty"`
	Pending     *cashierstock.Command  `json:"pending,omitempty"`
}

func cashierCommandHash(c *cashierstock.Command) string {
	if c == nil {
		return ""
	}
	return cashierstock.Fingerprint(struct {
		Kind   string
		Params json.RawMessage
	}{c.Kind, c.Params})
}

// Next only reads Saby. Every mutation returned here must be committed by the
// worker before Execute. Unknown create/save replies never cause a new write.
func (p *SabyCashierReserveProvider) Next(ctx context.Context, r cashierstock.Request, progress cashierstock.Progress) (cashierstock.Plan, error) {
	var state cashierReserveReceipt
	if len(progress.Native) > 0 && string(progress.Native) != "{}" {
		if json.Unmarshal(progress.Native, &state) != nil || state.Version != 1 {
			return cashierstock.Plan{Attention: true, Message: "Повреждено подтверждение резервного счёта"}, nil
		}
	} else {
		state = cashierReserveReceipt{Version: 1, Identity: cashierInvoiceIdentity{Config: r.Config}, Rows: []cashierInvoiceRow{}, Phase: "start"}
	}
	finish := func(done, attention bool, message string) (cashierstock.Plan, error) {
		raw, _ := json.Marshal(state)
		return cashierstock.Plan{Native: raw, Done: done, Attention: attention, Message: message}, nil
	}
	attention := func(message string) (cashierstock.Plan, error) { return finish(false, true, message) }
	advance := func(phase string) (cashierstock.Plan, error) {
		state.Phase = phase
		state.Polls = 0
		state.Pending = nil
		return finish(false, false, "")
	}
	wait := func(message string) (cashierstock.Plan, error) {
		state.Polls++
		if state.Polls >= 10 {
			return attention(message + "; требуется сверка без повторной записи")
		}
		plan, err := finish(false, false, message)
		plan.RetryAfter = 5 * time.Second
		return plan, err
	}
	prepare := func(kind string, params any, guard cashierInvoiceGuard) (cashierstock.Plan, error) {
		command, err := cashierCommand(kind, params, guard)
		if err != nil {
			return cashierstock.Plan{}, err
		}
		state.Pending = command
		state.Polls = 0
		raw, _ := json.Marshal(state)
		return cashierstock.Plan{Native: raw, Command: command}, nil
	}
	guard := func() cashierInvoiceGuard {
		return cashierInvoiceGuard{Identity: state.Identity, Rows: state.Rows, Number: state.Number, Marker: state.Marker}
	}
	if p == nil || !p.Client.Configured() || r.ReservationID <= 0 || r.OrderID <= 0 || r.OrderNumber == "" || r.Config.WarehouseID <= 0 || r.Config.MovementWarehouseID <= 0 || r.Config.OrganizationID <= 0 || r.Config.RegulationID <= 0 || r.Terminal != "" && r.Terminal != "cancelled" && r.Terminal != "completed" {
		return attention("Не подтверждены настройки или заказ для резерва СБИС")
	}
	if state.Index < 0 || state.Index > 1000 || len(state.Rows) > 1000 || state.Identity.Config != r.Config {
		return attention("Изменились настройки или повреждён этап резерва СБИС")
	}
	for _, line := range r.Lines {
		if !cashierValidLine(line) {
			return attention("Неверные товар, цена или количество резервируемого заказа")
		}
	}
	wantedHash := cashierstock.Fingerprint(r)
	if state.RequestHash != wantedHash {
		if state.RequestHash != "" && (state.Phase != "done" || state.Pending != nil || progress.Command != nil) {
			return attention("Нельзя заменить состав незавершённой операции резерва")
		}
		state.RequestHash = wantedHash
		state.Index = 0
		state.Polls = 0
		if state.Identity.ID > 0 {
			state.Phase = "release"
		} else {
			state.Phase = "start"
		}
	}
	if progress.Command != nil {
		if !progress.Attempted || state.Pending == nil || cashierCommandHash(progress.Command) != cashierCommandHash(state.Pending) {
			return attention("Команда резерва не совпала с сохранённым этапом")
		}
		switch state.Phase {
		case "start":
			if progress.Command.Kind != "create" || progress.Error != "" || len(progress.Reply) == 0 || string(progress.Reply) == "null" {
				return attention("Результат создания счёта неизвестен; новый счёт повторно не создаётся")
			}
			header, id, err := cashierHeader(progress.Reply, state.Identity)
			if err != nil {
				return attention("СБИС не подтвердил реквизиты созданного резервного счёта")
			}
			state.Identity = id
			state.Number = sabyValue(sabyRecordField(header, "Номер"))
			state.Marker = sabyValue(sabyRecordField(header, "ДокументРасширение.Название"))
			return advance("header")
		case "header", "terminal-header":
			if progress.Command.Kind != "header" {
				return attention("Неверный этап записи резервного счёта")
			}
			if state.Phase == "terminal-header" {
				return advance("verify-terminal-header")
			}
			return advance("verify-header")
		case "fill":
			if progress.Command.Kind != "prepare-row" || state.Index >= len(r.Lines) || progress.Error != "" || len(progress.Reply) == 0 {
				return attention("Не подтверждён шаблон строки; повторное добавление не выполнялось")
			}
			record, err := sabyFillDecode(progress.Reply)
			if err != nil {
				return attention("СБИС не подтвердил шаблон строки резерва")
			}
			row, err := cashierParseRow(record, state.Identity, true)
			if err != nil || row.Line != r.Lines[state.Index] {
				return attention("СБИС подготовил другой товар, цену или количество; строка не сохранялась")
			}
			state.Prepared = append(json.RawMessage(nil), progress.Reply...)
			return advance("save")
		case "save":
			if progress.Command.Kind != "save-row" || progress.Error != "" || len(progress.Reply) == 0 || state.Index >= len(r.Lines) {
				return attention("Неизвестен ID сохранённой строки; повторное добавление запрещено")
			}
			record, err := sabyFillDecode(progress.Reply)
			if err != nil {
				return attention("СБИС не подтвердил сохранение строки")
			}
			id, err := sabyInt64Field(record, "result")
			if err != nil || id <= 0 {
				return attention("СБИС не вернул ID сохранённой строки; нужна сверка")
			}
			for _, row := range state.Rows {
				if row.ID == id {
					return attention("СБИС повторил ID сохранённой строки")
				}
			}
			state.Rows = append(state.Rows, cashierInvoiceRow{ID: id, Line: r.Lines[state.Index]})
			state.Prepared = nil
			return advance("verify-save")
		case "release":
			if progress.Command.Kind != "remove" || state.Index >= len(state.Rows) {
				return attention("Неверный этап снятия резерва")
			}
			return advance("verify-remove")
		case "delete":
			if progress.Command.Kind != "delete-row" || len(state.Rows) == 0 {
				return attention("Неверный этап замены состава счёта")
			}
			return advance("verify-delete")
		case "apply":
			if progress.Command.Kind != "apply" || state.Index >= len(state.Rows) {
				return attention("Неверный этап постановки резерва")
			}
			return advance("verify-apply")
		default:
			return attention("Неизвестный этап выполненной команды резерва")
		}
	}
	if state.Pending != nil {
		return attention("Потеряно подтверждение подготовленной команды; повторная запись запрещена")
	}
	if state.Phase == "done" {
		return finish(true, false, "")
	}
	if state.Phase == "start" {
		if r.Terminal != "" {
			state.Phase = "done"
			return finish(true, false, "")
		}
		if len(r.Lines) == 0 || len(r.Lines) > 1000 {
			return attention("Не указан состав резервируемого заказа")
		}
		g := guard()
		g.TargetMarker = cashierMarker(r)
		return prepare("create", cashierCreateParams(r, g.TargetMarker), g)
	}
	if state.Identity.ID <= 0 || !sabyOrderRealizationUUID.MatchString(state.Identity.UUID) {
		return attention("Не сохранена точная привязка резервного счёта")
	}
	if state.Phase == "verify-header" || state.Phase == "verify-terminal-header" {
		header, err := p.Client.cashierHeader(ctx, state.Identity)
		if err != nil {
			return cashierstock.Plan{}, err
		}
		if sabyValue(sabyRecordField(header, "Номер")) != cashierNumber(r.ReservationID) || sabyValue(sabyRecordField(header, "ДокументРасширение.Название")) != cashierMarker(r) {
			return wait("СБИС не подтвердил название резервного счёта")
		}
		rows, err := p.Client.cashierRows(ctx, state.Identity)
		if err != nil {
			return cashierstock.Plan{}, err
		}
		if !cashierSameRows(state.Rows, rows) {
			return attention("Состав резервного счёта изменился во время записи заголовка")
		}
		state.Number = cashierNumber(r.ReservationID)
		state.Marker = cashierMarker(r)
		if state.Phase == "verify-terminal-header" {
			return advance("done")
		}
		state.Index = 0
		return advance("fill")
	}
	if state.Phase == "verify-delete" {
		if len(state.Rows) == 0 {
			return attention("Не сохранён ID удаляемой строки")
		}
		header, err := p.Client.cashierHeader(ctx, state.Identity)
		if err != nil {
			return cashierstock.Plan{}, err
		}
		if sabyValue(sabyRecordField(header, "Номер")) != state.Number || sabyValue(sabyRecordField(header, "ДокументРасширение.Название")) != state.Marker {
			return attention("Резервный счёт изменён вне сайта")
		}
		rows, err := p.Client.cashierRows(ctx, state.Identity)
		if err != nil {
			return cashierstock.Plan{}, err
		}
		for _, row := range rows {
			if row.ID == state.Rows[0].ID {
				return wait("СБИС не подтвердил удаление прежней строки")
			}
		}
		if !cashierSameRows(state.Rows[1:], rows) {
			return attention("Состав счёта изменился во время замены; дальнейшее удаление остановлено")
		}
		state.Rows = state.Rows[1:]
		return advance("delete")
	}
	if state.Phase == "verify-save" {
		rows, err := p.Client.cashierRows(ctx, state.Identity)
		if err != nil {
			return cashierstock.Plan{}, err
		}
		if !cashierSameRows(state.Rows, rows) {
			wanted := map[int64]bool{}
			for _, row := range state.Rows {
				wanted[row.ID] = true
			}
			for _, row := range rows {
				if !wanted[row.ID] {
					return attention("В резервном счёте обнаружена чужая строка")
				}
			}
			return wait("Ожидается точное чтение сохранённой строки СБИС")
		}
		state.Index++
		return advance("fill")
	}
	if err := p.Client.cashierCheckGuard(ctx, guard()); err != nil {
		if errors.Is(err, errCashierRemoteRead) {
			return cashierstock.Plan{}, err
		}
		return attention("Реквизиты или состав резервного счёта изменились; обмен остановлен для сверки")
	}
	if state.Phase == "verify-remove" || state.Phase == "verify-apply" {
		if state.Index >= len(state.Rows) {
			return attention("Не сохранён проверяемый резерв")
		}
		remote, err := p.Client.cashierReserveState(ctx, state.Identity, state.Rows[state.Index])
		if err != nil {
			return cashierstock.Plan{}, err
		}
		if state.Phase == "verify-remove" {
			if remote != "empty" {
				return wait("Не подтверждено снятие резерва СБИС")
			}
			state.Index++
			return advance("release")
		}
		if remote != "matching" {
			return wait("СБИС не подтвердил точное количество резерва")
		}
		state.Index++
		return advance("apply")
	}
	switch state.Phase {
	case "header", "terminal-header":
		g := guard()
		g.TargetMarker = cashierMarker(r)
		g.TargetNumber = cashierNumber(r.ReservationID)
		return prepare("header", cashierHeaderParams(state.Identity, cashierNumber(r.ReservationID), g.TargetMarker), g)
	case "release":
		if state.Index >= len(state.Rows) {
			state.Index = 0
			if r.Terminal != "" {
				return advance("terminal-header")
			}
			return advance("delete")
		}
		row := state.Rows[state.Index]
		remote, err := p.Client.cashierReserveState(ctx, state.Identity, row)
		if err != nil {
			return cashierstock.Plan{}, err
		}
		if remote == "empty" {
			state.Index++
			return advance("release")
		}
		if remote != "matching" {
			return attention("Резерв изменён вне сайта; автоматическое снятие остановлено")
		}
		return prepare("remove", map[string]any{"DocId": state.Identity.ID, "DocNomId": row.ID}, guard())
	case "delete":
		if len(state.Rows) == 0 {
			return advance("header")
		}
		return prepare("delete-row", map[string]any{"ИдО": []string{strconv.FormatInt(state.Rows[0].ID, 10)}, "ДопПоля": map[string]any{"СкладскойДокумент": state.Identity.ID}}, guard())
	case "fill":
		if state.Index >= len(r.Lines) {
			state.Index = 0
			return advance("apply")
		}
		g := guard()
		line := r.Lines[state.Index]
		g.NextLine = &line
		return prepare("prepare-row", cashierPrepareRowParams(state.Identity, line), g)
	case "save":
		if len(state.Prepared) == 0 || state.Index >= len(r.Lines) {
			return attention("Не сохранён точный шаблон строки")
		}
		var row any
		if json.Unmarshal(state.Prepared, &row) != nil {
			return attention("Повреждён шаблон строки резерва")
		}
		g := guard()
		line := r.Lines[state.Index]
		g.NextLine = &line
		return prepare("save-row", map[string]any{"Запись": row, "ДопПоля": map[string]any{"СкладскойДокумент": state.Identity.ID}}, g)
	case "apply":
		if state.Index >= len(state.Rows) {
			state.Phase = "done"
			return finish(true, false, "")
		}
		row := state.Rows[state.Index]
		remote, err := p.Client.cashierReserveState(ctx, state.Identity, row)
		if err != nil {
			return cashierstock.Plan{}, err
		}
		if remote == "matching" {
			state.Index++
			return advance("apply")
		}
		if remote != "empty" {
			return attention("СБИС вернул другой резерв; запись остановлена")
		}
		return prepare("apply", map[string]any{"DocId": state.Identity.ID, "DocNomId": row.ID, "Count": row.Line.Quantity, "PlannedDate": nil}, guard())
	default:
		return attention("Неизвестный этап обработки резерва")
	}
}

func (p *SabyCashierReserveProvider) Execute(ctx context.Context, command cashierstock.Command) (json.RawMessage, error) {
	if p == nil || !p.Client.Configured() {
		return nil, errors.New("Подключение резервов СБИС не настроено")
	}
	var prepared cashierNativeParams
	if json.Unmarshal(command.Params, &prepared) != nil {
		return nil, errors.New("Повреждена команда резерва")
	}
	guard := prepared.Guard
	var params map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(prepared.RPC)))
	decoder.UseNumber()
	if decoder.Decode(&params) != nil {
		return nil, errors.New("Повреждены параметры резерва")
	}
	method := ""
	if command.Kind == "create" {
		if guard.Identity.ID != 0 || guard.Identity.UUID != "" || len(guard.Rows) != 0 || guard.TargetMarker == "" {
			return nil, errors.New("Неверная область создания резервного счёта")
		}
		filter, ok := params["Фильтр"].(map[string]any)
		if !ok || params["ИмяМетода"] != "СчетСкИсх.ListLite" {
			return nil, errors.New("Неверный запрос создания счёта")
		}
		for name, wanted := range map[string]int64{"ТипДокумента": 231, "Регламент": guard.Identity.Config.RegulationID, "ДокументНашаОрганизация": guard.Identity.Config.OrganizationID, "СкладскойДокумент.Склад": guard.Identity.Config.WarehouseID} {
			got, e := sabyInt64Field(filter, name)
			if e != nil || got != wanted || wanted <= 0 {
				return nil, errors.New("Не подтверждены реквизиты создания резервного счёта")
			}
		}
		if sabyRecordField(filter, "ДокументРасширение.Название") != guard.TargetMarker {
			return nil, errors.New("Не совпала принадлежность нового счёта")
		}
		if cashierstock.Fingerprint(params) != cashierstock.Fingerprint(cashierCreateParams(cashierstock.Request{Config: guard.Identity.Config}, guard.TargetMarker)) {
			return nil, errors.New("Изменён сохранённый запрос создания счёта")
		}
		method = "СчетСкИсх.Создать"
	} else {
		if err := p.Client.cashierCheckGuard(ctx, guard); err != nil {
			return nil, err
		}
		additionalDoc := func() bool {
			a, ok := params["ДопПоля"].(map[string]any)
			return ok && sabyValue(a["СкладскойДокумент"]) == strconv.FormatInt(guard.Identity.ID, 10)
		}
		ownedRow := func(id int64) (cashierInvoiceRow, bool) {
			for _, row := range guard.Rows {
				if row.ID == id {
					return row, true
				}
			}
			return cashierInvoiceRow{}, false
		}
		switch command.Kind {
		case "header":
			record, ok := params["Запись"].(map[string]any)
			if !ok {
				return nil, errors.New("Не указана запись счёта")
			}
			id, e := sabyInt64Field(record, "@Документ")
			if e != nil || id != guard.Identity.ID || sabyRecordField(record, "ИдентификаторДокумента") != guard.Identity.UUID || sabyRecordField(record, "ДокументРасширение.Название") != guard.TargetMarker || guard.TargetMarker == "" {
				return nil, errors.New("Изменение адресовано другому счёту")
			}
			if guard.TargetNumber == "" || cashierstock.Fingerprint(params) != cashierstock.Fingerprint(cashierHeaderParams(guard.Identity, guard.TargetNumber, guard.TargetMarker)) {
				return nil, errors.New("Изменён сохранённый запрос записи счёта")
			}
			method = "СчетСкИсх.WriteForParticipant"
		case "prepare-row":
			if guard.NextLine == nil || !cashierValidLine(*guard.NextLine) {
				return nil, errors.New("Не сохранён следующий товар")
			}
			if cashierstock.Fingerprint(params) != cashierstock.Fingerprint(cashierPrepareRowParams(guard.Identity, *guard.NextLine)) {
				return nil, errors.New("Изменён запрос подготовки строки")
			}
			method = "СчетСкИсх.DocNomCreate"
		case "save-row":
			record, ok := params["Запись"].(map[string]any)
			if !ok || guard.NextLine == nil || !cashierValidLine(*guard.NextLine) || !additionalDoc() || len(params) != 2 {
				return nil, errors.New("Неверная область сохранения строки")
			}
			row, e := cashierParseRow(record, guard.Identity, true)
			if e != nil || row.Line != *guard.NextLine {
				return nil, errors.New("Сохранённый шаблон не совпал с товаром")
			}
			method = "СчетСкИсх.CustomDocNomUpdate"
		case "delete-row":
			ids, ok := params["ИдО"].([]any)
			if !ok || len(ids) != 1 || !additionalDoc() {
				return nil, errors.New("Неверная область удаления строки")
			}
			id, e := strconv.ParseInt(sabyValue(ids[0]), 10, 64)
			row, owned := ownedRow(id)
			if e != nil || !owned {
				return nil, errors.New("Удаляемая строка не принадлежит сайту")
			}
			if cashierstock.Fingerprint(params) != cashierstock.Fingerprint(map[string]any{"ИдО": []string{strconv.FormatInt(id, 10)}, "ДопПоля": map[string]any{"СкладскойДокумент": guard.Identity.ID}}) {
				return nil, errors.New("Изменена область удаления строки")
			}
			remote, e := p.Client.cashierReserveState(ctx, guard.Identity, row)
			if e != nil || remote != "empty" {
				return nil, errors.New("Перед заменой строки её резерв должен быть снят")
			}
			method = "СчетСкИсх.CustomDocNomDelete"
		case "apply", "remove":
			id, e := strconv.ParseInt(sabyValue(params["DocNomId"]), 10, 64)
			row, owned := ownedRow(id)
			if e != nil || !owned || sabyValue(params["DocId"]) != strconv.FormatInt(guard.Identity.ID, 10) {
				return nil, errors.New("Резерв адресован чужой строке")
			}
			remote, e := p.Client.cashierReserveState(ctx, guard.Identity, row)
			if e != nil {
				return nil, e
			}
			if command.Kind == "apply" {
				if len(params) != 4 || sabyValue(params["Count"]) != strconv.FormatInt(row.Line.Quantity, 10) || params["PlannedDate"] != nil {
					return nil, errors.New("Изменено количество или срок резерва")
				}
				if remote == "matching" {
					return json.RawMessage(`true`), nil
				}
				if remote != "empty" {
					return nil, errors.New("Резерв уже изменён вне сайта")
				}
				method = "Reserve.Apply"
			} else {
				if len(params) != 2 {
					return nil, errors.New("Изменена область снятия резерва")
				}
				if remote == "empty" {
					return json.RawMessage(`true`), nil
				}
				if remote != "matching" {
					return nil, errors.New("Нельзя снять изменённый вне сайта резерв")
				}
				method = "Reserve.Remove"
			}
		default:
			return nil, errors.New("Кассовый обработчик не поддерживает эту операцию")
		}
	}
	// This allowlist deliberately has no Post, CancelPosting, payment or KKT.
	reply, _, err := p.Client.writeWarehouseRPCOnce(ctx, method, params)
	return reply, err
}
