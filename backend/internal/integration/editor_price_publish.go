package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	"github.com/avpavlo8/ficusin-store/backend/internal/money"
	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
)

// EditorPublisher keeps price-only operations in the editor's gated publication
// process without giving them permission to create or rebind a remote card.
type EditorPublisher struct {
	Cards  ce.Publisher
	Prices *EditorPricePublisher
}

func (p *EditorPublisher) Advance(ctx context.Context, ch string, r ce.Request, s ce.Progress) (ce.Result, error) {
	if r.Action == "price" {
		return p.Prices.Advance(ctx, ch, r, s)
	}
	return p.Cards.Advance(ctx, ch, r, s)
}

type editorAvitoPrices interface {
	Configured() bool
	UpdatePrice(context.Context, string, float64) error
	ReadPrice(context.Context, string) (int64, error)
}
type EditorPricePublisher struct {
	Marketplaces *MarketplaceExecutor
	Saby         *SabyClient
	Avito        editorAvitoPrices
	Mock         bool
}
type editorPriceState struct {
	OperationID  string          `json:"operationId,omitempty"`
	PreparedData json.RawMessage `json:"preparedData,omitempty"`
	PreparedHash string          `json:"preparedHash,omitempty"`
	BaselineHash string          `json:"baselineHash,omitempty"`
	RequestHash  string          `json:"requestHash,omitempty"`
}

func (p *EditorPricePublisher) Advance(ctx context.Context, ch string, r ce.Request, s ce.Progress) (ce.Result, error) {
	result := func(status, message string) (ce.Result, error) {
		return ce.Result{Progress: s, Status: status, Message: message}, nil
	}
	if p == nil || r.Action != "price" || r.Price == nil || r.Price.TargetID == "" || r.Price.PriceMinor <= 0 {
		return ce.Result{}, errors.New("Нет подтверждённой цены или существующей связи площадки")
	}
	if ch != "wb" && ch != "ozon" && ch != "saby" && ch != "avito" {
		return ce.Result{}, errors.New("Неподдерживаемый канал цены")
	}
	if p.Mock && (ch == "saby" || ch == "avito") {
		s.Stage = "done"
		return result("synced", "Локальная проверка цены; внешняя площадка не изменялась")
	}
	var state editorPriceState
	if len(s.Native) > 0 && json.Unmarshal(s.Native, &state) != nil {
		return result("needs_attention", "Повреждено подтверждение цены. Повторная отправка запрещена")
	}
	save := func() { s.Native, _ = json.Marshal(state) }
	if s.Stage == "" {
		s.Stage = "price-send"
		return result("queued", "Цена подготовлена. Ожидает отправки после сохранения задания")
	}
	if s.Stage == "done" {
		return result("synced", "Цена площадки проверена")
	}
	if ch == "saby" {
		return p.advanceSabyPrice(ctx, r, s, state)
	}
	if ch == "avito" {
		if p.Avito == nil || !p.Avito.Configured() {
			return priceUnavailable(s, "Ключи Avito не настроены")
		}
		if s.Stage == "price-send" {
			s.Stage = "price-verify"
			if err := p.Avito.UpdatePrice(ctx, r.Price.TargetID, money.Rubles(r.Price.PriceMinor)); err != nil {
				s.Ambiguous = true
				return result("queued", "Ответ Avito не подтверждён. Проверяем цену без повторной отправки")
			}
			return result("queued", "Цена Avito отправлена. Проверяем объявление")
		}
		got, err := p.Avito.ReadPrice(ctx, r.Price.TargetID)
		if err == nil && got == ((r.Price.PriceMinor+50)/100)*100 {
			s.Stage = "done"
			s.Ambiguous = false
			return result("synced", "Цена объявления Avito подтверждена")
		}
		s.Checks++
		if s.Checks >= 90 {
			return result("needs_attention", "Avito не подтвердил цену. Повторная отправка не выполнялась")
		}
		return result("queued", "Ожидаем подтверждения цены Avito")
	}
	if p.Marketplaces == nil || !p.Marketplaces.Configured(ch) {
		return priceUnavailable(s, "Ключи площадки не настроены")
	}
	item := procurement.ActionItem{Channel: ch, ExternalArticle: r.Price.TargetID, NewValue: money.Rubles(r.Price.PriceMinor), Attempts: s.Checks}
	if r.Price.CompareAtMinor != nil {
		v := money.Rubles(*r.Price.CompareAtMinor)
		item.CompareAtValue = &v
	}
	if s.Stage == "price-send" {
		execution, err := p.Marketplaces.Execute(ctx, item)
		state.OperationID = execution.ExternalOperationID
		if state.OperationID == "" {
			state.OperationID = "readback"
		}
		save()
		s.Stage = "price-verify"
		if err != nil {
			var remote *remoteError
			if errors.As(err, &remote) && remote.Status >= 400 && remote.Status < 500 {
				s.Stage = "price-rejected"
				state.OperationID = ""
				save()
				return ce.Result{Progress: s, Status: "failed", Message: safeRemoteMessage(err.Error())}, nil
			}
			s.Ambiguous = true
		}
		return result("queued", "Цена отправлена. Проверяем цену и скидку без повторной отправки")
	}
	// Always reconcile directly against the seller's actual tuple. An accepted
	// task or equal effective price alone cannot prove the discount/old_price.
	ok, err := p.verifyMarketplacePrice(ctx, ch, item)
	if err == nil && ok {
		s.Stage = "done"
		s.Ambiguous = false
		return result("synced", "Цена и скидка площадки подтверждены")
	}
	s.Checks++
	if s.Checks >= 90 {
		return result("needs_attention", "Площадка не подтвердила цену и скидку. Повторная отправка запрещена")
	}
	return result("queued", "Ожидаем применения цены и скидки площадки")
}
func wbEditorPrice(item procurement.ActionItem) (int64, int64) {
	base := int64(item.NewValue)
	discount := int64(0)
	if item.CompareAtValue != nil && *item.CompareAtValue > item.NewValue {
		base = int64(*item.CompareAtValue)
		discount = int64((1 - item.NewValue/float64(base)) * 100)
	}
	return base, discount
}
func (p *EditorPricePublisher) verifyMarketplacePrice(ctx context.Context, ch string, item procurement.ActionItem) (bool, error) {
	if ch == "wb" {
		id, e := strconv.ParseInt(item.ExternalArticle, 10, 64)
		if e != nil || id <= 0 {
			return false, errors.New("Нужен точный nmID Wildberries")
		}
		var out struct {
			Data struct {
				ListGoods []struct {
					ID       int64 `json:"nmID"`
					Discount int64 `json:"discount"`
					Sizes    []struct {
						Price      marketplaceNumber `json:"price"`
						Discounted marketplaceNumber `json:"discountedPrice"`
					} `json:"sizes"`
				} `json:"listGoods"`
			} `json:"data"`
		}
		endpoint := p.Marketplaces.wbBase + "/api/v2/list/goods/filter?" + url.Values{"limit": {"10"}, "offset": {"0"}, "filterNmID": {item.ExternalArticle}}.Encode()
		if e = p.Marketplaces.requestRead(ctx, http.MethodGet, endpoint, nil, map[string]string{"Authorization": p.Marketplaces.wbToken}, &out); e != nil {
			return false, e
		}
		base, discount := wbEditorPrice(item)
		for _, g := range out.Data.ListGoods {
			if g.ID != id {
				continue
			}
			if g.Discount != discount || len(g.Sizes) == 0 {
				return false, nil
			}
			for _, size := range g.Sizes {
				if math.Abs(float64(size.Price)-float64(base)) > .01 || math.Abs(float64(size.Discounted)-float64(base)*(100-float64(discount))/100) > 1.01 {
					return false, nil
				}
			}
			return true, nil
		}
		return false, nil
	}
	price, old, e := p.Marketplaces.readEditorOzonPrice(ctx, item.ExternalArticle)
	if e != nil {
		return false, e
	}
	expectedOld := float64(0)
	if item.CompareAtValue != nil && *item.CompareAtValue > item.NewValue {
		expectedOld = *item.CompareAtValue
	}
	return math.Abs(price-item.NewValue) < .01 && math.Abs(old-float64(expectedOld)) < .01, nil
}
func (e *MarketplaceExecutor) readEditorOzonPrice(ctx context.Context, offer string) (float64, float64, error) {
	var out struct {
		Items []struct {
			Offer string `json:"offer_id"`
			Price struct {
				Price marketplaceNumber `json:"price"`
				Old   marketplaceNumber `json:"old_price"`
			} `json:"price"`
		} `json:"items"`
	}
	body := map[string]any{"filter": map[string]any{"offer_id": []string{offer}, "visibility": "ALL"}, "limit": 100, "cursor": ""}
	if err := e.requestRead(ctx, http.MethodPost, e.ozonBase+"/v5/product/info/prices", body, map[string]string{"Client-Id": e.ozonClientID, "Api-Key": e.ozonAPIKey}, &out); err != nil {
		return 0, 0, err
	}
	for _, item := range out.Items {
		if item.Offer == offer && float64(item.Price.Price) > 0 {
			return float64(item.Price.Price), float64(item.Price.Old), nil
		}
	}
	return 0, 0, fmt.Errorf("Не найдена цена существующего артикула Ozon")
}

// A missing capability before HTTP is a known rejection. Keep an already
// ambiguous receipt read-only when credentials disappear between deployments.
func priceUnavailable(s ce.Progress, message string) (ce.Result, error) {
	if s.Stage == "price-verify" || s.Ambiguous {
		return ce.Result{Progress: s}, errors.New(message)
	}
	s.Stage = "price-rejected"
	return ce.Result{Progress: s, Status: "failed", Message: message}, nil
}
