package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/ordersale"
)

// SabyOrderRealizationProvider is a live-only adapter. The caller owns the
// durable lease/write-intent boundary; a read/reconcile stage never writes.
type SabyOrderRealizationProvider struct {
	Client               *SabyClient
	PublicRegulationUUID string
	Now                  func() time.Time
}

type sabyRealizationProviderReceipt struct {
	Version         int                              `json:"version"`
	RequestHash     string                           `json:"requestHash"`
	CreatePayload   json.RawMessage                  `json:"createPayload"`
	Phase           string                           `json:"phase"`
	LineIndex       int                              `json:"lineIndex"`
	Prepared        *SabyOrderRealizationPreparedRow `json:"prepared,omitempty"`
	KnownLines      []SabyOrderRealizationPricedLine `json:"knownLines,omitempty"`
	SavedRowID      int64                            `json:"savedRowId,omitempty"`
	Polls           int                              `json:"polls"`
	LastReceipt     json.RawMessage                  `json:"lastReceipt,omitempty"`
	PostingVerified bool                             `json:"postingVerified,omitempty"`
	CancelSawPosted bool                             `json:"cancelSawPosted,omitempty"`
	CancelPolls     int                              `json:"cancelPolls,omitempty"`
}

func (p *SabyOrderRealizationProvider) Step(ctx context.Context, request ordersale.Request, progress ordersale.Progress) (ordersale.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	result := ordersale.Result{Progress: progress, State: "queued"}
	attention := func(message string) (ordersale.Result, error) {
		result.State = "needs_attention"
		result.Message = message
		return result, nil
	}
	seenOwned := map[int64]bool{}
	for _, id := range progress.OwnedRowIDs {
		if id <= 0 || seenOwned[id] || len(progress.OwnedRowIDs) > 4096 {
			return attention("Сохранённые идентификаторы строк повреждены; требуется сверка")
		}
		seenOwned[id] = true
	}
	if p == nil || !p.Client.Configured() || request.ExecutionMode != "live" || !sabyOrderRealizationUUID.MatchString(p.PublicRegulationUUID) ||
		request.IntentID <= 0 || request.Event.OrderID <= 0 || !sabyRealizationFingerprint.MatchString(request.Event.Fingerprint) || !request.Event.CanPrepare || request.SiteWarehouseID <= 0 || request.NativeWarehouseID <= 0 || request.MovementWarehouseID <= 0 || request.OrganizationID <= 0 || request.RegulationID <= 0 || request.BuyerID <= 0 || !sabyOrderRealizationUUID.MatchString(request.OperationKey) {
		return attention("Не подтверждены настройки, покупатель или состав живой реализации СБИС")
	}
	plan, err := sabyRealizationProviderPlan(request.Event.Lines)
	if err != nil {
		return attention("Не подтверждены числовые товары, цены или количества реализации СБИС")
	}
	raw, _ := json.Marshal(request)
	sum := sha256.Sum256(raw)
	requestHash := hex.EncodeToString(sum[:])
	var receipt sabyRealizationProviderReceipt
	if len(progress.Native) > 0 {
		if json.Unmarshal(progress.Native, &receipt) != nil || receipt.Version != 1 || receipt.RequestHash != requestHash || len(receipt.CreatePayload) == 0 || receipt.Polls < 0 || receipt.Polls > 10 || receipt.CancelPolls < 0 || receipt.CancelPolls > 10 || receipt.LineIndex < 0 || receipt.LineIndex > len(plan) {
			return attention("Сохранённое подтверждение реализации повреждено или относится к другому заказу")
		}
		if !sabyRealizationProviderPreparedCreateMatches(receipt.CreatePayload, request) {
			return attention("Сохранённый запрос создания не совпал с операцией; запись не выполнялась")
		}
	} else if progress.DesiredState == "cancelled" && !progress.CreateAttempted && !progress.PostAttempted && progress.DocumentID == "" && progress.DocumentUUID == "" && len(progress.OwnedRowIDs) == 0 {
		return sabyRealizationProviderCancelled(result, receipt, "no_remote", nil)
	} else if progress.Stage != "inspect" || progress.DocumentID != "" || progress.Unknown {
		return attention("Отсутствует сохранённое подтверждение этапа; повторная запись не выполнялась")
	}
	if progress.DesiredState == "cancelled" {
		return p.cancelStep(ctx, request, result, receipt, plan)
	}
	commit := func(stage, phase, message string) (ordersale.Result, error) {
		receipt.Phase = phase
		receipt.Polls = 0
		result.Progress.Stage = stage
		result.Progress.Native, _ = json.Marshal(receipt)
		result.Message = message
		return result, nil
	}
	wait := func(message string) (ordersale.Result, error) {
		if receipt.Polls < 10 {
			receipt.Polls++
		}
		result.RetryAfter = 5 * time.Second
		result.Progress.Native, _ = json.Marshal(receipt)
		result.Message = message
		if receipt.Polls >= 10 {
			result.State = "needs_attention"
			result.Message = message + "; автоматическая проверка остановлена после 10 попыток"
		}
		return result, nil
	}
	if progress.Stage == "inspect" {
		payload, prepareErr := p.prepareCreate(ctx, request)
		if prepareErr != nil {
			return attention(prepareErr.Error())
		}
		receipt = sabyRealizationProviderReceipt{Version: 1, RequestHash: requestHash, CreatePayload: payload}
		public, exists, readErr := p.readPublic(ctx, request.OperationKey)
		if readErr != nil {
			return attention("Не удалось проверить отсутствие реализации; новый документ не создавался")
		}
		if exists {
			if !p.publicMatches(public, request, payload) {
				return attention("UUID операции уже принадлежит другому документу СБИС")
			}
			return commit("discover", "discover", "Документ уже существует; проверяем нативную привязку без создания")
		}
		return commit("create", "create", "Подготовлен единственный запрос создания реализации")
	}
	if progress.Stage == "create" {
		if receipt.Phase != "create" || progress.DocumentID != "" || progress.Unknown {
			return attention("Повторное создание реализации запрещено; нужна сверка")
		}
		reply, writeErr := p.writePublicCreateOnce(ctx, receipt.CreatePayload)
		receipt.LastReceipt = reply
		if writeErr != nil {
			result.Progress.Unknown = true
			result.Progress.Native, _ = json.Marshal(receipt)
			return attention("Результат создания неизвестен; новый документ повторно не создаётся")
		}
		public, decodeErr := sabyFillDecode(reply)
		if decodeErr != nil || !strings.EqualFold(sabyValue(public["Идентификатор"]), request.OperationKey) {
			result.Progress.Unknown = true
			result.Progress.Native, _ = json.Marshal(receipt)
			return attention("СБИС не подтвердил UUID созданной реализации; нужна сверка")
		}
		return commit("discover", "discover", "Создан черновик; проверяем точный нативный документ")
	}
	if progress.Stage == "discover" || progress.Stage == "reconcile-create" {
		public, exists, readErr := p.readPublic(ctx, request.OperationKey)
		if readErr != nil || !exists {
			return wait("Ожидаем чтение созданного документа СБИС")
		}
		if !p.publicMatches(public, request, receipt.CreatePayload) {
			return attention("Публичная привязка реализации не совпала; обмен остановлен")
		}
		snapshot, found, findErr := p.discover(ctx, request, receipt.CreatePayload)
		if findErr != nil {
			return wait("Не удалось однозначно проверить нативную реализацию СБИС")
		}
		if !found {
			return wait("Созданный документ ещё не найден в нативном реестре СБИС")
		}
		result.Progress.DocumentID = strconv.FormatInt(snapshot.ID, 10)
		result.Progress.DocumentUUID = snapshot.UUID
		result.Progress.Unknown = false
		return commit("verify-fill", "bind", "Нативная привязка найдена; проверяем покупателя и строки")
	}
	identity, err := sabyRealizationProviderIdentity(request, progress)
	if err != nil {
		return attention("Нет точной нативной привязки реализации; запись не выполнялась")
	}
	read := func() (SabyOrderRealizationSnapshot, error) {
		snapshot, e := p.Client.ReadOrderRealization(ctx, identity)
		if e == nil {
			e = p.validateHeader(snapshot, request, receipt.CreatePayload)
		}
		return snapshot, e
	}
	if progress.Stage == "verify-fill" || progress.Stage == "reconcile-fill" {
		snapshot, readErr := read()
		if readErr != nil {
			return wait("Не удалось проверить реквизиты реализации СБИС")
		}
		if snapshot.BuyerID != 0 && snapshot.BuyerID != request.BuyerID {
			return attention("В реализации выбран другой покупатель; обмен остановлен")
		}
		if snapshot.BuyerID == 0 {
			if progress.Stage == "reconcile-fill" || receipt.Phase == "bound" {
				return wait("Проверяем результат привязки покупателя; повторная запись не выполняется")
			}
			return commit("fill", "bind", "Подготовлена привязка подтверждённого покупателя")
		}
		if progress.Stage == "reconcile-fill" && receipt.Phase == "prepare" {
			return attention("Ответ подготовки строки потерян; повторная подготовка не выполняется")
		}
		lines, readErr := p.Client.ReadOrderRealizationPricedLines(ctx, identity)
		if readErr != nil {
			return wait("Не удалось проверить товары и цены реализации СБИС")
		}
		if !sabyRealizationProviderRetainsLines(receipt.KnownLines, lines) {
			return attention("Ранее проверенные строки реализации были удалены или изменены; требуется сверка")
		}
		next, matchErr := sabyRealizationProviderNext(plan, lines)
		if matchErr != nil {
			return attention("В реализации есть лишние или изменённые товары, цены или количества")
		}
		if (receipt.Phase == "saved" || receipt.Phase == "save") && receipt.LineIndex >= len(plan) {
			return attention("Неверный номер сохранённой строки реализации; требуется сверка")
		}
		if receipt.Phase == "saved" && !sabyRealizationProviderAcknowledgedRow(receipt.SavedRowID, plan[receipt.LineIndex], lines) {
			return wait("Ожидаем именно подтверждённую строку СБИС; повторное добавление и проведение запрещены")
		}
		if (receipt.Phase == "saved" || progress.Stage == "reconcile-fill" && receipt.Phase == "save") && !sabyRealizationProviderHasLine(plan[receipt.LineIndex], lines) {
			return wait("Проверяем результат сохранения строки; повторное добавление запрещено")
		}
		if receipt.Phase == "save" && progress.Stage == "verify-fill" {
			if next != receipt.LineIndex || receipt.Prepared == nil {
				return attention("Состав изменился после подготовки строки; повторная запись не выполняется")
			}
			receipt.KnownLines = lines
			return commit("fill", "save", "Подготовленная строка проверена; ожидает единственного сохранения")
		}
		result.Progress.Unknown = false
		receipt.Prepared = nil
		receipt.SavedRowID = 0
		receipt.LineIndex = next
		receipt.KnownLines = lines
		for _, line := range lines {
			result.Progress.OwnedRowIDs = sabyRealizationProviderOwnIDs(result.Progress.OwnedRowIDs, line.ID)
		}
		if next == len(plan) {
			return commit("post", "post", "Все товары и цены проверены; ожидает проведения")
		}
		return commit("fill", "prepare", "Подготовлена следующая товарная строка")
	}
	if progress.Stage == "fill" {
		snapshot, readErr := read()
		if readErr != nil {
			return attention("Не удалось проверить реализацию перед записью; изменение не выполнялось")
		}
		if snapshot.Posted || snapshot.HasEntries || snapshot.PostingQueued || snapshot.EntriesQueued || snapshot.PostingErrors || snapshot.StaleEntries {
			return attention("Складское состояние изменилось; запись товаров остановлена")
		}
		if receipt.Phase == "bind" {
			if snapshot.BuyerID == request.BuyerID {
				return commit("verify-fill", "verify", "Покупатель уже связан; повторная запись не выполнялась")
			}
			if snapshot.BuyerID != 0 {
				return attention("В реализации уже выбран другой покупатель")
			}
			bind := map[string]any{"_type": "record", "s": []any{map[string]any{"n": "@Документ", "t": "Число целое"}, map[string]any{"n": "Лицо1", "t": "Связь"}}, "d": []any{identity.ID, request.BuyerID}}
			reply, _, bindErr := p.Client.sabyOrderRealizationWriteRPCOnce(ctx, "РеалИсх.Записать", map[string]any{"Запись": bind})
			receipt.LastReceipt = reply
			var returned json.Number
			if bindErr != nil || json.Unmarshal(reply, &returned) != nil || string(returned) != progress.DocumentID {
				result.Progress.Unknown = true
				result.Progress.Native, _ = json.Marshal(receipt)
				return commitUnknown(result, receipt, "reconcile-fill", "Результат привязки покупателя неизвестен; проверяем без повторной записи")
			}
			return commit("verify-fill", "bound", "Покупатель записан; проверяем документ")
		}
		if receipt.LineIndex >= len(plan) {
			return attention("Неверный номер подготавливаемой строки реализации")
		}
		fill := SabyOrderRealizationFillRequest{Identity: identity, OrganizationID: request.OrganizationID, RegulationID: request.RegulationID, BuyerID: request.BuyerID, Lines: plan, Next: plan[receipt.LineIndex]}
		if receipt.Phase == "prepare" {
			prepared, prepareErr := p.Client.PrepareOrderRealizationRow(ctx, fill)
			receipt.LastReceipt = prepared.Receipt
			if prepareErr != nil {
				if prepared.Outcome == "unknown" {
					return commitUnknown(result, receipt, "reconcile-fill", "Неизвестен результат подготовки строки; повторная запись запрещена")
				}
				return attention("Не удалось безопасно подготовить товарную строку СБИС")
			}
			receipt.Prepared = prepared.Prepared
			return commit("verify-fill", "save", "Шаблон строки сохранён; проверяем перед единственной записью")
		}
		if receipt.Phase == "save" && receipt.Prepared != nil {
			saved, saveErr := p.Client.SavePreparedOrderRealizationRow(ctx, fill, *receipt.Prepared)
			receipt.LastReceipt = saved.Receipt
			if saveErr != nil {
				if saved.Outcome == "unknown" {
					return commitUnknown(result, receipt, "reconcile-fill", "Неизвестен результат сохранения строки; проверяем без повторного добавления")
				}
				return attention("Сохранённая строка не прошла повторную проверку; добавление не выполнялось")
			}
			receipt.SavedRowID = saved.RowID
			result.Progress.OwnedRowIDs = sabyRealizationProviderOwnIDs(result.Progress.OwnedRowIDs, saved.RowID)
			return commit("verify-fill", "saved", "Строка записана; проверяем фактический состав")
		}
		return attention("Неизвестен сохранённый этап заполнения реализации")
	}
	if progress.Stage == "post" {
		if _, readErr := read(); readErr != nil {
			return attention("Публичная и нативная привязки реализации изменились перед проведением")
		}
		lines, readErr := p.Client.ReadOrderRealizationPricedLines(ctx, identity)
		if readErr != nil || len(receipt.KnownLines) != len(lines) || !sabyRealizationProviderRetainsLines(receipt.KnownLines, lines) {
			return attention("Подтверждённые строки реализации изменились перед проведением; требуется сверка")
		}
		postLines := make([]SabyOrderRealizationPostLine, 0, len(plan))
		for _, line := range plan {
			postLines = append(postLines, SabyOrderRealizationPostLine{NomenclatureID: line.NomenclatureID, Quantity: line.Quantity, UnitPrice: line.UnitPrice})
		}
		posted, postErr := p.Client.PostOrderRealization(ctx, SabyOrderRealizationPostRequest{Identity: identity, OrganizationID: request.OrganizationID, RegulationID: request.RegulationID, BuyerID: request.BuyerID, Lines: postLines})
		receipt.LastReceipt = posted.Receipt
		if postErr != nil {
			if posted.Outcome == "unknown" {
				return commitUnknown(result, receipt, "verify-post", "Ответ проведения неизвестен; проверяем документ без повторного проведения")
			}
			result.Progress.Native, _ = json.Marshal(receipt)
			return attention("Проведение не подтверждено: проверьте товары, покупателя и ошибки документа СБИС")
		}
		return commit("verify-post", "verify-post", "Запрос проведения завершён; проверяем складское движение")
	}
	if progress.Stage == "verify-post" || progress.Stage == "reconcile-post" {
		snapshot, readErr := p.Client.ReadOrderRealizationWithLines(ctx, identity)
		if readErr != nil {
			return wait("Не удалось проверить складское движение реализации")
		}
		if err = p.validateHeader(snapshot, request, receipt.CreatePayload); err != nil || snapshot.BuyerID != request.BuyerID {
			return attention("Реквизиты проведённой реализации не совпали; требуется сверка")
		}
		if snapshot.PostingErrors || snapshot.StaleEntries {
			return attention("СБИС сообщил об ошибке или неактуальных проводках; списание не подтверждено")
		}
		if !snapshot.HeaderPosted || !snapshot.ClosedOutgoingLines {
			return wait("Ожидаем подтверждение проведения и закрытия складских строк СБИС")
		}
		prices, readErr := p.Client.ReadOrderRealizationPricedLines(ctx, identity)
		if readErr != nil {
			return wait("Не удалось проверить цены и количества проведённой реализации")
		}
		next, matchErr := sabyRealizationProviderNext(plan, prices)
		if matchErr != nil || next != len(plan) {
			return attention("Проведённые товары, цены или количества не совпали с заказом")
		}
		if !sabyRealizationProviderSameLineIDs(snapshot.Lines, prices) {
			return attention("Складские строки изменились между проверками; требуется сверка")
		}
		if len(receipt.KnownLines) != len(prices) || !sabyRealizationProviderRetainsLines(receipt.KnownLines, prices) {
			return attention("Подтверждённые строки реализации изменились после запроса проведения; требуется сверка")
		}
		proof := ordersale.StockProof{Version: 1, ExecutionMode: "live", IntentID: request.IntentID, EventKey: request.Event.Key, EventFingerprint: request.Event.Fingerprint, OperationKey: request.OperationKey, DocumentID: progress.DocumentID, DocumentUUID: progress.DocumentUUID, SiteWarehouseID: request.SiteWarehouseID, NativeWarehouseID: request.NativeWarehouseID, MovementWarehouseID: request.MovementWarehouseID, OrganizationID: request.OrganizationID, RegulationID: request.RegulationID, BuyerID: request.BuyerID, Posted: true, HasEntries: true, Lines: []ordersale.StockProofLine{}}
		for _, line := range prices {
			proof.Lines = append(proof.Lines, ordersale.StockProofLine{ID: line.ID, DocumentID: progress.DocumentID, NomenclatureID: strconv.FormatInt(line.NomenclatureID, 10), WarehouseID: line.WarehouseID, Direction: line.Direction, Quantity: line.Quantity, UnitPrice: line.UnitPrice, LineSum: line.LineSum, Closed: line.Closed})
		}
		result.Progress.Unknown = false
		result.Progress.Stage = "verified"
		receipt.PostingVerified = true
		result.Progress.Native, _ = json.Marshal(receipt)
		result.State = "posted"
		result.StockVerified = true
		result.StockProof, _ = json.Marshal(proof)
		result.Message = "Реализация проведена; закрытое складское движение подтверждено"
		return result, nil
	}
	return attention("Неизвестный этап живого обмена СБИС; запись не выполнялась")
}

func sabyRealizationProviderAcknowledgedRow(id int64, wanted SabyOrderRealizationFillLine, lines []SabyOrderRealizationPricedLine) bool {
	if id <= 0 {
		return false
	}
	q, qe := sabyFillNumber(wanted.Quantity, false)
	p, pe := sabyFillNumber(wanted.UnitPrice, true)
	if qe != nil || pe != nil {
		return false
	}
	for _, line := range lines {
		if line.ID == id {
			actualQ, e := sabyFillNumber(line.Quantity, false)
			actualP, f := sabyFillNumber(line.UnitPrice, true)
			return e == nil && f == nil && line.NomenclatureID == wanted.NomenclatureID && actualQ.Cmp(q) == 0 && actualP.Cmp(p) == 0
		}
	}
	return false
}

func commitUnknown(result ordersale.Result, receipt sabyRealizationProviderReceipt, stage, message string) (ordersale.Result, error) {
	result.Progress.Unknown = true
	result.Progress.Stage = stage
	receipt.Polls = 0
	result.Progress.Native, _ = json.Marshal(receipt)
	result.State = "queued"
	result.Message = message
	return result, nil
}

func sabyRealizationProviderPlan(lines []ordersale.Line) ([]SabyOrderRealizationFillLine, error) {
	if len(lines) == 0 || len(lines) > 1000 {
		return nil, errors.New("invalid lines")
	}
	groups := map[string]SabyOrderRealizationFillLine{}
	for _, line := range lines {
		id, e := strconv.ParseInt(line.SabyID, 10, 64)
		price, f := sabyFillNumber(json.Number(line.UnitPrice), true)
		if e != nil || id <= 0 || strconv.FormatInt(id, 10) != line.SabyID || line.Quantity <= 0 || f != nil {
			return nil, errors.New("invalid canonical line")
		}
		key := line.SabyID + ":" + price.RatString()
		old := groups[key]
		old.NomenclatureID = id
		old.UnitPrice = json.Number(price.FloatString(2))
		q := int64(line.Quantity)
		if old.Quantity != "" {
			previous, err := strconv.ParseInt(string(old.Quantity), 10, 64)
			if err != nil || previous > int64(^uint64(0)>>1)-q {
				return nil, errors.New("quantity overflow")
			}
			q += previous
		}
		old.Quantity = json.Number(strconv.FormatInt(q, 10))
		groups[key] = old
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	plan := make([]SabyOrderRealizationFillLine, 0, len(keys))
	for _, k := range keys {
		plan = append(plan, groups[k])
	}
	return plan, nil
}

func sabyRealizationProviderIdentity(r ordersale.Request, p ordersale.Progress) (SabyOrderRealizationIdentity, error) {
	id, e := strconv.ParseInt(p.DocumentID, 10, 64)
	if e != nil || id <= 0 || strconv.FormatInt(id, 10) != p.DocumentID || !sabyOrderRealizationUUID.MatchString(p.DocumentUUID) {
		return SabyOrderRealizationIdentity{}, errors.New("invalid native identity")
	}
	return SabyOrderRealizationIdentity{ID: id, UUID: p.DocumentUUID, WarehouseID: r.NativeWarehouseID, MovementWarehouseID: r.MovementWarehouseID}, nil
}

func sabyRealizationProviderNext(plan []SabyOrderRealizationFillLine, lines []SabyOrderRealizationPricedLine) (int, error) {
	actualLines := make([]SabyOrderRealizationFillLine, 0, len(lines))
	for _, l := range lines {
		actualLines = append(actualLines, SabyOrderRealizationFillLine{NomenclatureID: l.NomenclatureID, Quantity: l.Quantity, UnitPrice: l.UnitPrice})
	}
	actual, e := sabyFillTotals(actualLines)
	wanted, f := sabyFillTotals(plan)
	if e != nil || f != nil {
		return 0, errors.New("invalid composition")
	}
	for key, q := range actual {
		if wanted[key] == nil || wanted[key].Cmp(q) != 0 {
			return 0, errors.New("changed composition")
		}
	}
	next := len(plan)
	for i, l := range plan {
		p, _ := sabyFillNumber(l.UnitPrice, true)
		key := strconv.FormatInt(l.NomenclatureID, 10) + ":" + p.RatString()
		if actual[key] == nil && next == len(plan) {
			next = i
		}
	}
	return next, nil
}

func sabyRealizationProviderSameLineIDs(base []SabyOrderRealizationLine, priced []SabyOrderRealizationPricedLine) bool {
	if len(base) != len(priced) {
		return false
	}
	seen := map[int64]SabyOrderRealizationLine{}
	for _, l := range base {
		seen[l.ID] = l
	}
	for _, l := range priced {
		old, ok := seen[l.ID]
		a, av := new(big.Rat).SetString(string(l.Quantity))
		b, bv := new(big.Rat).SetString(string(old.Quantity))
		if !ok || !av || !bv || a.Cmp(b) != 0 || l.NomenclatureID != old.NomenclatureID || !l.Closed || !old.Closed {
			return false
		}
		delete(seen, l.ID)
	}
	return len(seen) == 0
}

func (p *SabyOrderRealizationProvider) validateHeader(s SabyOrderRealizationSnapshot, r ordersale.Request, payload json.RawMessage) error {
	var doc map[string]any
	if json.Unmarshal(payload, &doc) != nil {
		return errors.New("invalid prepared create")
	}
	date, _ := doc["Дата"].(string)
	parsed, e := time.Parse("02.01.2006", date)
	if e != nil || s.OrganizationID != r.OrganizationID || s.RegulationID != r.RegulationID || !strings.EqualFold(s.PublicUUID, r.OperationKey) || sabyRecordField(s.document, "Дата") != parsed.Format("2006-01-02") {
		return errors.New("realization identity changed")
	}
	return nil
}

var sabyRealizationINN = regexp.MustCompile(`^[0-9]{12}$`)
var sabyRealizationFingerprint = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (p *SabyOrderRealizationProvider) prepareCreate(ctx context.Context, r ordersale.Request) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := p.Client.rpc(ctx, "sabyWarehouse.List", map[string]any{"filter": map[string]any{"limit": 100, "page": 0}}, &raw); err != nil {
		return nil, errors.New("Не удалось проверить организацию выбранного склада СБИС")
	}
	data, err := sabyFillDecode(raw)
	if err != nil {
		return nil, err
	}
	warehouses, _ := data["warehouses"].([]any)
	var inn string
	matches := 0
	for _, v := range warehouses {
		wh, _ := v.(map[string]any)
		id, _ := wh["warehouseId"].(json.Number)
		n, _ := id.Int64()
		if n == r.NativeWarehouseID {
			org, _ := wh["organization"].(map[string]any)
			inn, _ = org["inn"].(string)
			matches++
		}
	}
	if matches != 1 || !sabyRealizationINN.MatchString(inn) {
		return nil, errors.New("Не подтверждена единственная организация выбранного склада; документ не создавался")
	}
	now := time.Now()
	if p.Now != nil {
		now = p.Now()
	}
	number := fmt.Sprintf("FCS-%d", r.IntentID)
	return json.Marshal(map[string]any{"Идентификатор": r.OperationKey, "Тип": "ДокОтгрИсх", "Регламент": map[string]any{"Название": "Реализация"}, "Дата": now.In(time.FixedZone("MSK", 3*60*60)).Format("02.01.2006"), "Номер": number, "Название": "Фикусин: реализация заказа " + r.Event.OrderNumber, "Примечание": "FICUSIN-SALE " + r.OperationKey + " " + r.Event.Fingerprint, "НашаОрганизация": map[string]any{"СвФЛ": map[string]any{"ИНН": inn}}})
}

func (p *SabyOrderRealizationProvider) publicMatches(doc map[string]any, r ordersale.Request, payload json.RawMessage) bool {
	var expected map[string]any
	if json.Unmarshal(payload, &expected) != nil {
		return false
	}
	reg, _ := doc["Регламент"].(map[string]any)
	return strings.EqualFold(sabyValue(doc["Идентификатор"]), r.OperationKey) && doc["Тип"] == "ДокОтгрИсх" && strings.EqualFold(sabyValue(reg["Идентификатор"]), p.PublicRegulationUUID) && reg["Название"] == "Реализация" && doc["Номер"] == expected["Номер"] && doc["Примечание"] == expected["Примечание"]
}

func sabyRealizationProviderPreparedCreateMatches(payload json.RawMessage, r ordersale.Request) bool {
	var doc map[string]any
	if len(payload) > 64<<10 || json.Unmarshal(payload, &doc) != nil || len(doc) != 8 || doc["Идентификатор"] != r.OperationKey || doc["Тип"] != "ДокОтгрИсх" || doc["Номер"] != fmt.Sprintf("FCS-%d", r.IntentID) || doc["Примечание"] != "FICUSIN-SALE "+r.OperationKey+" "+r.Event.Fingerprint || doc["Название"] != "Фикусин: реализация заказа "+r.Event.OrderNumber {
		return false
	}
	reg, ok := doc["Регламент"].(map[string]any)
	if !ok || len(reg) != 1 || reg["Название"] != "Реализация" {
		return false
	}
	date, ok := doc["Дата"].(string)
	if !ok {
		return false
	}
	if _, err := time.Parse("02.01.2006", date); err != nil {
		return false
	}
	org, ok := doc["НашаОрганизация"].(map[string]any)
	if !ok || len(org) != 1 {
		return false
	}
	person, ok := org["СвФЛ"].(map[string]any)
	if !ok || len(person) != 1 {
		return false
	}
	inn, ok := person["ИНН"].(string)
	return ok && sabyRealizationINN.MatchString(inn)
}

func (p *SabyOrderRealizationProvider) readPublic(ctx context.Context, key string) (map[string]any, bool, error) {
	var reply struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    json.Number `json:"code"`
			Message string      `json:"message"`
			Details string      `json:"details"`
			Data    struct {
				ClassID   string      `json:"classid"`
				ErrorCode json.Number `json:"error_code"`
			} `json:"data"`
		} `json:"error"`
	}
	request := map[string]any{"jsonrpc": "2.0", "protocol": 3, "method": "СБИС.ПрочитатьДокумент", "params": map[string]any{"Документ": map[string]any{"Идентификатор": key}}, "id": 1}
	if err := p.Client.authorizedJSON(ctx, http.MethodPost, p.Client.serviceURL, request, &reply); err != nil {
		return nil, false, err
	}
	if reply.Error != nil {
		message := strings.ToLower(strings.TrimSpace(reply.Error.Message + " " + reply.Error.Details))
		knownClass := reply.Error.Data.ClassID == "" || strings.EqualFold(reply.Error.Data.ClassID, "{00000000-0000-0000-0000-1fa000010007}")
		knownErrorCode := reply.Error.Data.ErrorCode == "" || reply.Error.Data.ErrorCode == "-1"
		if reply.Error.Code == "-32000" && knownClass && knownErrorCode && strings.HasPrefix(message, "не найден документ с идентификатором ") && strings.Contains(message, strings.ToLower(key)) && strings.Contains(message, "https://saby.ru/help/integration/api/error_solution/not_doc") {
			return nil, false, nil
		}
		return nil, false, errors.New("Не удалось проверить существующий документ СБИС")
	}
	doc, err := sabyFillDecode(reply.Result)
	return doc, err == nil, err
}

func (p *SabyOrderRealizationProvider) writePublicCreateOnce(ctx context.Context, payload json.RawMessage) (json.RawMessage, error) {
	token, err := p.Client.accessToken(ctx, false)
	if err != nil {
		return nil, err
	}
	transport := &http.Client{Transport: p.Client.client.Transport, Timeout: p.Client.client.Timeout, Jar: p.Client.client.Jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	writer := &SabyClient{client: transport, requestLimiter: p.Client.requestLimiter}
	var reply sabyRPCResponse
	_, err = writer.requestJSON(ctx, http.MethodPost, p.Client.serviceURL, map[string]any{"jsonrpc": "2.0", "protocol": 3, "method": "СБИС.ЗаписатьДокумент", "params": map[string]any{"Документ": payload}, "id": 1}, token, &reply)
	if err != nil || reply.Error != nil {
		return nil, errors.New("Результат создания СБИС неизвестен")
	}
	if len(reply.Result) > 1<<20 {
		return nil, errors.New("Неверный размер ответа создания СБИС")
	}
	return reply.Result, nil
}

func (p *SabyOrderRealizationProvider) discover(ctx context.Context, r ordersale.Request, payload json.RawMessage) (SabyOrderRealizationSnapshot, bool, error) {
	var create map[string]any
	if json.Unmarshal(payload, &create) != nil {
		return SabyOrderRealizationSnapshot{}, false, errors.New("invalid create payload")
	}
	var found *SabyOrderRealizationSnapshot
	seen := map[int64]bool{}
	for page := 0; page < 5; page++ {
		var raw json.RawMessage
		filter := sabyRecord(map[string]any{"rp_doc": true, "usePages": "full", "НазваниеТипаДокумента": "ДокОтгрИсх", "ФильтрПоЛицуИзСписка": "Нет", "ФильтрПоМаске": create["Номер"]})
		if err := p.Client.rpc(ctx, "ДокОтгрИсх.СписокХраним", map[string]any{"Фильтр": filter, "Сортировка": nil, "Навигация": sabyRecord(map[string]any{"ЕстьЕще": true, "РазмерСтраницы": int64(20), "Страница": int64(page)}), "ДопПоля": []any{}}, &raw); err != nil {
			return SabyOrderRealizationSnapshot{}, false, err
		}
		set, err := sabyFillDecode(raw)
		if err != nil {
			return SabyOrderRealizationSnapshot{}, false, err
		}
		rows, err := sabyFillRecordset(set)
		if err != nil || len(rows) > 20 {
			return SabyOrderRealizationSnapshot{}, false, errors.New("invalid native list")
		}
		for _, row := range rows {
			value := sabyRecordField(row, "@Документ")
			if tuple, ok := value.([]any); ok {
				if len(tuple) != 2 {
					return SabyOrderRealizationSnapshot{}, false, errors.New("invalid composite id")
				}
				value = tuple[0]
			}
			number, ok := value.(json.Number)
			id, e := number.Int64()
			if !ok || e != nil || id <= 0 || seen[id] {
				return SabyOrderRealizationSnapshot{}, false, errors.New("invalid or repeated native id")
			}
			seen[id] = true
			var rawDoc json.RawMessage
			if e = p.Client.rpc(ctx, "ДокОтгрИсх.Прочитать", map[string]any{"ИдО": id, "ИмяМетода": "ДокОтгрИсх.Список"}, &rawDoc); e != nil {
				return SabyOrderRealizationSnapshot{}, false, e
			}
			doc, e := sabyOrderRealizationRecord(rawDoc, "@Документ")
			if e != nil {
				return SabyOrderRealizationSnapshot{}, false, e
			}
			vi, _ := sabyRecordField(doc, "ДокументРасширение.ИдентификаторВИ").(string)
			if !strings.EqualFold(vi, r.OperationKey) {
				continue
			}
			uuid, _ := sabyRecordField(doc, "ИдентификаторДокумента").(string)
			snapshot, e := p.Client.ReadOrderRealization(ctx, SabyOrderRealizationIdentity{ID: id, UUID: uuid, WarehouseID: r.NativeWarehouseID, MovementWarehouseID: r.MovementWarehouseID})
			if e != nil || p.validateHeader(snapshot, r, payload) != nil {
				return SabyOrderRealizationSnapshot{}, false, errors.New("native identity mismatch")
			}
			if found != nil {
				return SabyOrderRealizationSnapshot{}, false, errors.New("ambiguous public UUID")
			}
			found = &snapshot
		}
		if len(rows) < 20 {
			if found == nil {
				return SabyOrderRealizationSnapshot{}, false, nil
			}
			return *found, true, nil
		}
	}
	return SabyOrderRealizationSnapshot{}, false, errors.New("native discovery exceeded safe bound")
}

func sabyRealizationProviderHasLine(wanted SabyOrderRealizationFillLine, lines []SabyOrderRealizationPricedLine) bool {
	actual := []SabyOrderRealizationFillLine{}
	price, _ := sabyFillNumber(wanted.UnitPrice, true)
	for _, l := range lines {
		p, e := sabyFillNumber(l.UnitPrice, true)
		if e == nil && l.NomenclatureID == wanted.NomenclatureID && p.Cmp(price) == 0 {
			actual = append(actual, SabyOrderRealizationFillLine{NomenclatureID: l.NomenclatureID, Quantity: l.Quantity, UnitPrice: l.UnitPrice})
		}
	}
	a, e := sabyFillTotals(actual)
	b, f := sabyFillTotals([]SabyOrderRealizationFillLine{wanted})
	if e != nil || f != nil || len(a) != len(b) {
		return false
	}
	for k, q := range b {
		if a[k] == nil || a[k].Cmp(q) != 0 {
			return false
		}
	}
	return true
}

func sabyRealizationProviderRetainsLines(prior, current []SabyOrderRealizationPricedLine) bool {
	seen := map[int64]SabyOrderRealizationPricedLine{}
	for _, l := range current {
		seen[l.ID] = l
	}
	for _, old := range prior {
		now, ok := seen[old.ID]
		if !ok || old.NomenclatureID != now.NomenclatureID || old.DocumentID != now.DocumentID || old.WarehouseID != now.WarehouseID || old.Direction != now.Direction {
			return false
		}
		for _, pair := range [][2]json.Number{{old.Quantity, now.Quantity}, {old.UnitPrice, now.UnitPrice}, {old.LineSum, now.LineSum}} {
			a, av := new(big.Rat).SetString(string(pair[0]))
			b, bv := new(big.Rat).SetString(string(pair[1]))
			if !av || !bv || a.Cmp(b) != 0 {
				return false
			}
		}
	}
	return true
}

func sabyRealizationProviderOwnIDs(prior []int64, added ...int64) []int64 {
	seen := map[int64]bool{}
	for _, id := range prior {
		if id > 0 {
			seen[id] = true
		}
	}
	for _, id := range added {
		if id > 0 {
			seen[id] = true
		}
	}
	ids := make([]int64, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// Cancellation is a separate desired state injected by the durable worker.
// It never edits the frozen request, resumes a fill, or repeats a post/create.
func (p *SabyOrderRealizationProvider) cancelStep(ctx context.Context, request ordersale.Request, result ordersale.Result, receipt sabyRealizationProviderReceipt, plan []SabyOrderRealizationFillLine) (ordersale.Result, error) {
	attention := func(message string) (ordersale.Result, error) {
		result.State = "needs_attention"
		result.Message = message
		result.Progress.Native, _ = json.Marshal(receipt)
		return result, nil
	}
	wait := func(message string) (ordersale.Result, error) {
		if receipt.CancelPolls < 10 {
			receipt.CancelPolls++
		}
		result.State = "queued"
		result.Message = message
		result.RetryAfter = 5 * time.Second
		result.Progress.Native, _ = json.Marshal(receipt)
		if receipt.CancelPolls >= 10 {
			result.State = "needs_attention"
			result.Message += "; после 10 проверок требуется ручная сверка"
		}
		return result, nil
	}
	queue := func(stage, message string) (ordersale.Result, error) {
		receipt.CancelPolls = 0
		result.Progress.Stage = stage
		result.Progress.Native, _ = json.Marshal(receipt)
		result.State = "queued"
		result.Message = message
		return result, nil
	}
	if !result.Progress.CreateAttempted && !result.Progress.PostAttempted && result.Progress.DocumentID == "" && result.Progress.DocumentUUID == "" && len(result.Progress.OwnedRowIDs) == 0 && len(receipt.KnownLines) == 0 && receipt.SavedRowID == 0 {
		return sabyRealizationProviderCancelled(result, receipt, "no_remote", nil)
	}
	if !result.Progress.CreateAttempted {
		return attention("Сохранённая попытка создания не подтверждена; отмена требует сверки")
	}
	if result.Progress.DocumentID == "" {
		result.Progress.Unknown = true
		result.Progress.Stage = "discover-cancel"
		public, exists, err := p.readPublic(ctx, request.OperationKey)
		if err != nil || !exists {
			return wait("Проверяем неизвестный результат создания; повторное создание и списание запрещены")
		}
		if !p.publicMatches(public, request, receipt.CreatePayload) {
			return attention("UUID отменяемой операции принадлежит другому документу; требуется сверка")
		}
		snapshot, found, err := p.discover(ctx, request, receipt.CreatePayload)
		if err != nil || !found {
			return wait("Ищем точную нативную привязку отменяемой операции; новые документы не создаются")
		}
		result.Progress.DocumentID = strconv.FormatInt(snapshot.ID, 10)
		result.Progress.DocumentUUID = snapshot.UUID
		return queue("inspect-cancel", "Найдена нативная реализация; проверяем необходимость отмены проведения")
	}
	identity, err := sabyRealizationProviderIdentity(request, result.Progress)
	if err != nil {
		return attention("Нет точной нативной привязки для отмены; изменение не выполнялось")
	}
	snapshot, err := p.Client.ReadOrderRealizationWithLines(ctx, identity)
	if err != nil {
		return wait("Не удалось прочитать состояние отменяемой реализации")
	}
	if p.validateHeader(snapshot, request, receipt.CreatePayload) != nil {
		return attention("Реквизиты отменяемой реализации изменились; требуется сверка")
	}
	face, faceErr := sabyInt64Field(snapshot.document, "Лицо3")
	if faceErr != nil || face != request.OrganizationID || snapshot.BuyerID != 0 && snapshot.BuyerID != request.BuyerID {
		return attention("Покупатель или организация отменяемой реализации не совпали")
	}
	prices, err := p.Client.ReadOrderRealizationPricedLines(ctx, identity)
	if err != nil {
		return wait("Не удалось проверить строки и цены отменяемой реализации")
	}
	if !sabyRealizationProviderCancelSameReads(snapshot.Lines, prices) {
		return wait("Строки отменяемой реализации изменились между проверками")
	}
	next, err := sabyRealizationProviderNext(plan, prices)
	if err != nil {
		return attention("В отменяемой реализации обнаружены лишние или изменённые товары и цены")
	}
	owned := append([]int64(nil), result.Progress.OwnedRowIDs...)
	for _, line := range receipt.KnownLines {
		owned = sabyRealizationProviderOwnIDs(owned, line.ID)
	}
	owned = sabyRealizationProviderOwnIDs(owned, receipt.SavedRowID)
	result.Progress.OwnedRowIDs = owned
	currentIDs := map[int64]bool{}
	for _, line := range prices {
		currentIDs[line.ID] = true
	}
	for _, id := range owned {
		if !currentIDs[id] {
			return wait("Ожидаем ранее подтверждённую строку отменяемой реализации; её связь не заменяется")
		}
	}
	if !sabyRealizationProviderRetainsLines(receipt.KnownLines, prices) {
		return attention("Подтверждённые строки отменяемой реализации изменились")
	}
	for _, line := range prices {
		result.Progress.OwnedRowIDs = sabyRealizationProviderOwnIDs(result.Progress.OwnedRowIDs, line.ID)
	}
	if snapshot.PostingErrors || snapshot.StaleEntries {
		return attention("СБИС сообщил об ошибках проводок; обратное движение требует сверки")
	}
	if snapshot.PostingQueued || snapshot.EntriesQueued {
		return wait("Ожидаем завершение неизвестного проведения; повторное списание и отмена не отправляются")
	}
	allOpen := true
	for _, line := range prices {
		allOpen = allOpen && !line.Closed
	}
	if !snapshot.Posted && !snapshot.HasEntries && allOpen {
		if !result.Progress.PostAttempted && !receipt.CancelSawPosted && !receipt.PostingVerified {
			proof := sabyRealizationProviderCancellationDocument(request, result.Progress, snapshot, prices)
			return sabyRealizationProviderCancelled(result, receipt, "unposted_draft", &proof)
		}
		if next == len(plan) && result.Progress.PostAttempted && (receipt.CancelSawPosted || receipt.PostingVerified) && sabyCancelCleared(snapshot, prices) {
			proof := sabyRealizationProviderCancellationDocument(request, result.Progress, snapshot, prices)
			return sabyRealizationProviderCancelled(result, receipt, "unposted_full", &proof)
		}
		return wait("Исходная попытка проведения ещё не разрешена; отсутствие движения пока не считается отменой")
	}
	if !snapshot.HeaderPosted || !snapshot.ClosedOutgoingLines || next != len(plan) || snapshot.BuyerID != request.BuyerID {
		return attention("Не подтверждён полный проведённый состав собственной реализации; отмена не выполнялась")
	}
	if !result.Progress.PostAttempted {
		return attention("Документ проведён вне сохранённой попытки обмена; требуется сверка")
	}
	receipt.CancelSawPosted = true
	if result.Progress.Stage == "verify-cancel" || result.Progress.Stage == "reconcile-cancel" {
		result.Progress.Unknown = true
		return wait("Проверяем неизвестный результат отмены проведения; повторная отмена не отправляется")
	}
	if result.Progress.Stage != "cancel" {
		// This fresh positive proof resolves any older unknown create/post. It
		// does not describe an attempted cancel; the first cancel may now be leased.
		result.Progress.Unknown = false
		return queue("cancel", "Полный проведённый состав проверен; подготовлена единственная отмена проведения")
	}
	// The prior read-only stage committed the exact positive row IDs before this
	// write-stage lease. The low-level adapter repeats all ownership checks.
	postLines := make([]SabyOrderRealizationPostLine, 0, len(plan))
	for _, line := range plan {
		postLines = append(postLines, SabyOrderRealizationPostLine{NomenclatureID: line.NomenclatureID, Quantity: line.Quantity, UnitPrice: line.UnitPrice})
	}
	cancelled, cancelErr := p.Client.CancelOrderRealizationPosting(ctx, SabyOrderRealizationCancelRequest{Identity: identity, PublicUUID: request.OperationKey, OrganizationID: request.OrganizationID, RegulationID: request.RegulationID, BuyerID: request.BuyerID, Lines: postLines, ExpectedRowIDs: result.Progress.OwnedRowIDs})
	receipt.LastReceipt = cancelled.Receipt
	result.Progress.Unknown = true
	if cancelErr != nil {
		return queue("verify-cancel", "Результат отмены проведения проверяется чтением; повторная запись запрещена")
	}
	return queue("verify-cancel", "Отмена проведения отправлена; проверяем открытые складские строки")
}

func sabyRealizationProviderCancelled(result ordersale.Result, receipt sabyRealizationProviderReceipt, kind string, document *ordersale.StockProof) (ordersale.Result, error) {
	result.State = "cancelled"
	result.CancellationVerified = true
	result.StockVerified = false
	result.StockProof = nil
	result.CancellationProof, _ = json.Marshal(ordersale.CancellationProof{Kind: kind, Document: document})
	result.Progress.Stage = "cancelled"
	result.Progress.Unknown = false
	receipt.CancelPolls = 0
	result.Progress.Native, _ = json.Marshal(receipt)
	result.Message = "Списание отменено; подтверждено отсутствие проведённого движения"
	if kind == "no_remote" {
		result.Message = "Отмена подтверждена; запрос создания реализации не отправлялся"
	}
	return result, nil
}

func sabyRealizationProviderCancellationDocument(request ordersale.Request, progress ordersale.Progress, snapshot SabyOrderRealizationSnapshot, lines []SabyOrderRealizationPricedLine) ordersale.StockProof {
	proof := ordersale.StockProof{Version: 1, ExecutionMode: "live", IntentID: request.IntentID, EventKey: request.Event.Key, EventFingerprint: request.Event.Fingerprint, OperationKey: request.OperationKey, DocumentID: progress.DocumentID, DocumentUUID: progress.DocumentUUID, SiteWarehouseID: request.SiteWarehouseID, NativeWarehouseID: snapshot.WarehouseID, MovementWarehouseID: request.MovementWarehouseID, OrganizationID: snapshot.OrganizationID, RegulationID: snapshot.RegulationID, BuyerID: snapshot.BuyerID, Posted: snapshot.Posted, HasEntries: snapshot.HasEntries, PostingErrors: snapshot.PostingErrors, StaleEntries: snapshot.StaleEntries, PostingQueued: snapshot.PostingQueued, EntriesQueued: snapshot.EntriesQueued, Lines: []ordersale.StockProofLine{}}
	for _, line := range lines {
		proof.Lines = append(proof.Lines, ordersale.StockProofLine{ID: line.ID, DocumentID: progress.DocumentID, NomenclatureID: strconv.FormatInt(line.NomenclatureID, 10), WarehouseID: line.WarehouseID, Direction: line.Direction, Quantity: line.Quantity, UnitPrice: line.UnitPrice, LineSum: line.LineSum, Closed: line.Closed})
	}
	return proof
}

func sabyRealizationProviderCancelSameReads(base []SabyOrderRealizationLine, priced []SabyOrderRealizationPricedLine) bool {
	if len(base) != len(priced) {
		return false
	}
	seen := map[int64]SabyOrderRealizationLine{}
	for _, line := range base {
		seen[line.ID] = line
	}
	for _, line := range priced {
		prior, ok := seen[line.ID]
		a, av := new(big.Rat).SetString(string(prior.Quantity))
		b, bv := new(big.Rat).SetString(string(line.Quantity))
		if !ok || !av || !bv || a.Cmp(b) != 0 || prior.NomenclatureID != line.NomenclatureID || prior.DocumentID != line.DocumentID || prior.WarehouseID != line.WarehouseID || prior.Direction != line.Direction || prior.Closed != line.Closed {
			return false
		}
		delete(seen, line.ID)
	}
	return len(seen) == 0
}
