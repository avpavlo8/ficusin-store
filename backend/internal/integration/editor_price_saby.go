package integration

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	"github.com/avpavlo8/ficusin-store/backend/internal/money"
)

func (p *EditorPricePublisher) advanceSabyPrice(ctx context.Context, r ce.Request, s ce.Progress, state editorPriceState) (ce.Result, error) {
	result := func(status, message string) (ce.Result, error) {
		return ce.Result{Progress: s, Status: status, Message: message}, nil
	}
	if p.Saby == nil || !p.Saby.Configured() {
		return priceUnavailable(s, "Подключение СБИС не настроено")
	}
	target := r.SabyID
	if target == "" {
		target = r.Price.TargetID
	}
	id, err := strconv.ParseInt(target, 10, 64)
	if err != nil || id <= 0 {
		return result("needs_attention", "Для цены СБИС нужна существующая числовая связь")
	}
	card, err := p.Saby.readSabyEditorNativeCard(ctx, id)
	if err != nil {
		return ce.Result{Progress: s}, err
	}
	identity := ce.Request{SabyID: target, SabyUUID: r.SabyUUID}
	if r.SabyID != "" {
		identity.Saby.ExistingCode = r.Price.TargetID
	}
	if err = validateSabyEditorNativeIdentity(card, identity, ce.RemoteIDs{ProductID: target, UUID: r.SabyUUID}); err != nil {
		return result("needs_attention", err.Error())
	}
	var draft bool
	if json.Unmarshal(card.value("IsDraft"), &draft) != nil || draft {
		return result("needs_attention", "Карточка СБИС ещё не опубликована. Сначала завершите её создание; изменение цены не публикует черновик")
	}
	requestJSON, _ := json.Marshal(r.Price)
	if s.Stage == "price-send" {
		prices, e := sabyEditorSetPrice(card.value("PriceData"), p.Saby.priceListID, r.Price.PriceMinor)
		if e != nil {
			return ce.Result{Progress: s}, e
		}
		state.PreparedData, e = card.selectFields(map[string]json.RawMessage{"PriceData": prices})
		if e != nil {
			return ce.Result{Progress: s}, e
		}
		state.BaselineHash, e = sabyEditorBaselineHash(card, state.PreparedData)
		if e != nil {
			return ce.Result{Progress: s}, e
		}
		state.RequestHash = sabyEditorPayloadHash(requestJSON)
		state.PreparedHash = sabyEditorPayloadHash(state.PreparedData)
		s.Native, _ = json.Marshal(state)
		s.Stage = "price-native-send"
		return result("queued", "Подготовлен точный прайс СБИС; остальные поля карточки не меняются")
	}
	if s.Stage == "price-native-send" {
		if len(state.PreparedData) == 0 || state.RequestHash != sabyEditorPayloadHash(requestJSON) || state.PreparedHash != sabyEditorPayloadHash(state.PreparedData) {
			return result("needs_attention", "Подтверждение запроса цены СБИС повреждено. Отправка остановлена")
		}
		baseline, e := sabyEditorBaselineHash(card, state.PreparedData)
		if e != nil || baseline != state.BaselineHash {
			return result("needs_attention", "Цена СБИС изменилась после подготовки. Проверьте прайс")
		}
		s.Stage = "price-verify"
		if err = p.Saby.sabyEditorWriteRPCOnce(ctx, "Nomenclature.Update", map[string]any{"Id": id, "Data": state.PreparedData, "Option": sabyRecord(map[string]any{})}, nil); err != nil {
			s.Ambiguous = true
			return result("queued", "Ответ СБИС не получен. Проверяем цену без повторного Update")
		}
		return result("queued", "Цена передана в СБИС. Проверяем выбранный прайс")
	}
	matched, e := sabyEditorPriceMatches(card, p.Saby.priceListID, r.Price.PriceMinor)
	if e == nil && matched {
		s.Stage = "done"
		s.Ambiguous = false
		return result("synced", "Цена СБИС подтверждена чтением прайса")
	}
	s.Checks++
	if s.Checks >= 90 {
		return result("needs_attention", "СБИС не подтвердил цену. Повторный Update не отправлялся")
	}
	return result("queued", "Ожидаем подтверждения цены СБИС")
}
func sabyEditorPriceMatches(card *sabyEditorNativeRecord, listID, minor int64) (bool, error) {
	prices, err := parseSabyEditorNativeRecordSet(card.value("PriceData"))
	if err != nil {
		return false, err
	}
	count := 0
	matches := false
	for i := range prices.rows {
		list, e := sabyEditorNativeInt(prices.cell(i, "PriceList"))
		if e != nil || list != listID {
			continue
		}
		count++
		var n json.Number
		if json.Unmarshal(prices.cell(i, "Price"), &n) != nil {
			return false, errors.New("СБИС не вернул цену")
		}
		got, e := money.Parse(string(n))
		matches = e == nil && got == minor
	}
	return count == 1 && matches, nil
}
