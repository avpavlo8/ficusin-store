package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
)

// CatalogPublisher owns card creation only. The normal pricing executor never
// acquires this capability. Constructed disabled by default in cmd/api.
type CatalogPublisher struct {
	Executor *MarketplaceExecutor
	Mock     bool
}

func catalogPrice(raw string) (float64, error) {
	p, e := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
	if e != nil || math.IsNaN(p) || math.IsInf(p, 0) || p <= 0 {
		return 0, errors.New("нужна положительная цена площадки")
	}
	return p, nil
}
func catalogContent(r ce.Request) (string, string) {
	n, d := r.Marketplace.Name, r.Marketplace.Description
	if strings.TrimSpace(n) == "" && !r.Marketplace.ContentFromMarketplace {
		n = r.Name
	}
	if strings.TrimSpace(d) == "" && !r.Marketplace.ContentFromMarketplace {
		d = r.Description
	}
	return strings.TrimSpace(n), strings.TrimSpace(d)
}
func validateCatalogRequest(ch string, r ce.Request, allowLocal bool) error {
	if r.Channel.Mode == "link" {
		if strings.TrimSpace(r.Channel.ExistingID) == "" {
			return errors.New("укажите идентификатор существующего товара")
		}
		return nil
	}
	n, d := catalogContent(r)
	if n == "" || d == "" && r.Action != "updating" || strings.TrimSpace(r.SellerArticle(ch)) == "" {
		return errors.New("заполните название, описание и артикул продавца")
	}
	if ch == "wb" && len([]rune(n)) > 60 {
		return errors.New("название Wildberries должно быть не длиннее 60 символов")
	}
	if ch == "ozon" && r.Channel.Category.TypeID <= 0 {
		return errors.New("выберите категорию и тип Ozon")
	}
	if r.Action != "updating" {
		if err := validateCatalogPhotos(ch, r.Images, allowLocal); err != nil {
			return err
		}
	}
	p := r.Packaging
	if p.Length <= 0 || p.Width <= 0 || p.Height <= 0 || p.Weight <= 0 {
		return errors.New("заполните габариты и вес упаковки")
	}
	for _, x := range []float64{p.Length, p.Width, p.Height, p.Weight} {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return errors.New("некорректные габариты")
		}
	}
	if len(r.Schema) == 0 {
		return errors.New("загрузите характеристики выбранной категории")
	}
	return nil
}
func validateCatalogPhotos(ch string, images []string, allowLocal bool) error {
	if len(images) == 0 {
		return errors.New("добавьте фотографию")
	}
	max := 30
	if ch == "ozon" {
		max = 15
	}
	if len(images) > max {
		return fmt.Errorf("для %s допустимо максимум %d фото", ch, max)
	}
	for _, image := range images {
		u, e := url.Parse(image)
		if e != nil || u.Host == "" || u.User != nil || u.Scheme != "https" && !(allowLocal && u.Scheme == "http" && isLoopbackHost(u.Hostname())) {
			return errors.New("для площадки нужны прямые публичные HTTPS-ссылки на фото")
		}
		ip := net.ParseIP(u.Hostname())
		if !allowLocal && (isLoopbackHost(u.Hostname()) || strings.HasSuffix(u.Hostname(), ".local") || ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified())) {
			return errors.New("локальные фото недоступны площадке")
		}
	}
	return nil
}

func catalogAttributes(ch string, r ce.Request) ([]map[string]any, error) {
	result := []map[string]any{}
	name, description := catalogContent(r)
	for _, a := range r.Schema {
		role := a.Role(ch)
		if role == "vat" && ch == "ozon" || ch == "wb" && (role == "brand" || role == "name" || role == "description" || role == "article") {
			continue
		}
		raw := r.Channel.Attributes[strconv.FormatInt(a.Key(), 10)]
		if code := ce.SiteAttributeCode(a); code != "" {
			value := r.SiteAttributes[code]
			if r.ProcurementMeasurements {
				value = procurementMarketplaceMeasurement(a, value, ch)
			}
			if value != nil || r.Action != "updating" {
				raw = value
			}
		}
		if role != "" {
			raw = ce.CommonValue(a, r.Marketplace, ch, name, description)
		}
		if r.Action == "updating" {
			a.Required = false
			a.IsRequired = false
		}
		values, e := ce.AttributeValues(a, raw, ch)
		if e != nil {
			return nil, e
		}
		if len(values) == 0 {
			continue
		}
		if ch == "ozon" {
			vv := []map[string]any{}
			for _, v := range values {
				item := map[string]any{"value": v.Value}
				if a.DictionaryID > 0 || role == "type" {
					item["dictionary_value_id"] = v.DictionaryID
				}
				vv = append(vv, item)
			}
			result = append(result, map[string]any{"id": a.Key(), "complex_id": a.ComplexID, "values": vv})
		} else {
			var value any
			if a.Numeric(ch) {
				n, _ := strconv.ParseFloat(strings.ReplaceAll(values[0].Value, ",", "."), 64)
				value = n
			} else {
				texts := []string{}
				for _, v := range values {
					texts = append(texts, v.Value)
				}
				value = texts
			}
			result = append(result, map[string]any{"id": a.Key(), "value": value})
		}
	}
	return result, nil
}
func BuildOzonCard(r ce.Request, allowLocal bool) (map[string]any, error) {
	if e := validateCatalogRequest("ozon", r, allowLocal); e != nil {
		return nil, e
	}
	if r.Action == "updating" {
		if err := validateCatalogPhotos("ozon", r.Images, allowLocal); err != nil {
			return nil, err
		}
	}
	price, e := catalogPrice(r.OzonPrice)
	if e != nil {
		return nil, e
	}
	attrs, e := catalogAttributes("ozon", r)
	if e != nil {
		return nil, e
	}
	category, e := strconv.ParseInt(r.Channel.Category.ID, 10, 64)
	if e != nil || category <= 0 {
		return nil, errors.New("некорректная категория Ozon")
	}
	name, _ := catalogContent(r)
	plain := []map[string]any{}
	groups := map[int64][]map[string]any{}
	complex := []map[string]any{}
	for _, a := range attrs {
		if id, _ := a["complex_id"].(int64); id > 0 {
			groups[id] = append(groups[id], a)
		} else {
			plain = append(plain, a)
		}
	}
	for _, group := range groups {
		complex = append(complex, map[string]any{"attributes": group})
	}
	return map[string]any{"items": []any{map[string]any{"offer_id": r.SellerArticle("ozon"), "name": name, "description_category_id": category, "type_id": r.Channel.Category.TypeID, "attributes": plain, "complex_attributes": complex, "barcode": r.Barcode, "price": strconv.FormatFloat(price, 'f', 2, 64), "currency_code": "RUB", "vat": "0", "depth": int64(math.Ceil(r.Packaging.Length * 10)), "width": int64(math.Ceil(r.Packaging.Width * 10)), "height": int64(math.Ceil(r.Packaging.Height * 10)), "dimension_unit": "mm", "weight": int64(math.Ceil(r.Packaging.Weight)), "weight_unit": "g", "primary_image": r.Images[0], "images": r.Images[1:]}}}, nil
}
func BuildWBCard(r ce.Request, allowLocal bool) ([]map[string]any, error) {
	if e := validateCatalogRequest("wb", r, allowLocal); e != nil {
		return nil, e
	}
	price, e := catalogPrice(r.WBPrice)
	if e != nil {
		return nil, e
	}
	attrs, e := catalogAttributes("wb", r)
	if e != nil {
		return nil, e
	}
	category, e := strconv.ParseInt(r.Channel.Category.ID, 10, 64)
	if e != nil || category <= 0 {
		return nil, errors.New("некорректная категория Wildberries")
	}
	name, description := catalogContent(r)
	return []map[string]any{{"subjectID": category, "variants": []any{map[string]any{"vendorCode": r.SellerArticle("wb"), "title": name, "description": description, "brand": r.Marketplace.Brand, "dimensions": map[string]any{"length": int64(math.Ceil(r.Packaging.Length)), "width": int64(math.Ceil(r.Packaging.Width)), "height": int64(math.Ceil(r.Packaging.Height)), "weightBrutto": r.Packaging.Weight / 1000}, "characteristics": attrs, "sizes": []any{map[string]any{"techSize": "0", "price": int64(math.Ceil(price)), "skus": []string{r.Barcode}}}}}}}, nil
}
func (p *CatalogPublisher) read(ctx context.Context, ch, path string, payload any, target any) error {
	e := p.Executor
	headers := map[string]string{"Client-Id": e.ozonClientID, "Api-Key": e.ozonAPIKey}
	base := e.ozonBase
	if ch == "wb" {
		headers = map[string]string{"Authorization": e.wbToken}
		base = e.wbContentBase
	}
	return e.requestRead(ctx, http.MethodPost, base+path, payload, headers, target)
}
func (p *CatalogPublisher) write(ctx context.Context, ch, path string, payload any, target any) error {
	e := p.Executor
	headers := map[string]string{"Client-Id": e.ozonClientID, "Api-Key": e.ozonAPIKey}
	base := e.ozonBase
	if ch == "wb" {
		headers = map[string]string{"Authorization": e.wbToken}
		base = e.wbContentBase
	}
	return e.request(ctx, http.MethodPost, base+path, payload, headers, target)
}
func (p *CatalogPublisher) lookup(ctx context.Context, ch, identifier string, byArticle bool) (ce.RemoteIDs, error) {
	if ch == "ozon" {
		body := map[string]any{"offer_id": []string{}, "product_id": []int64{}, "sku": []int64{}}
		if !byArticle {
			if strings.HasPrefix(identifier, "sku:") {
				id, e := strconv.ParseInt(strings.TrimPrefix(identifier, "sku:"), 10, 64)
				if e != nil {
					return ce.RemoteIDs{}, errors.New("некорректный SKU Ozon")
				}
				body["sku"] = []int64{id}
			} else if strings.HasPrefix(identifier, "id:") {
				id, e := strconv.ParseInt(strings.TrimPrefix(identifier, "id:"), 10, 64)
				if e != nil {
					return ce.RemoteIDs{}, errors.New("некорректный product_id Ozon")
				}
				body["product_id"] = []int64{id}
			} else {
				body["offer_id"] = []string{strings.TrimPrefix(identifier, "offer:")}
			}
		} else {
			body["offer_id"] = []string{identifier}
		}
		var out struct {
			Items []struct {
				ID           int64    `json:"id"`
				OfferID      string   `json:"offer_id"`
				Barcodes     []string `json:"barcodes"`
				Images       any      `json:"images"`
				PrimaryImage any      `json:"primary_image"`
				Statuses     struct {
					StatusName string `json:"status_name"`
					IsCreated  *bool  `json:"is_created"`
				} `json:"statuses"`
				Sources []struct {
					SKU int64 `json:"sku"`
				} `json:"sources"`
			} `json:"items"`
		}
		if e := p.read(ctx, ch, "/v3/product/info/list", body, &out); e != nil {
			return ce.RemoteIDs{}, e
		}
		if len(out.Items) == 0 {
			return ce.RemoteIDs{}, nil
		}
		if len(out.Items) != 1 {
			return ce.RemoteIDs{}, errors.New("найдено несколько товаров Ozon, уточните идентификатор")
		}
		v := out.Items[0]
		if byArticle && v.OfferID != identifier {
			return ce.RemoteIDs{}, errors.New("Ozon вернул товар с другим артикулом")
		}
		images, imagesOK := catalogPhotoURLs(v.Images)
		primary, primaryOK := catalogPhotoURLs(v.PrimaryImage)
		cover := slices.Clone(primary)
		for _, photo := range images {
			if index := slices.Index(cover, photo); index >= 0 {
				cover = slices.Delete(cover, index, index+1)
				continue
			}
			primary = append(primary, photo)
		}
		ids := ce.RemoteIDs{ProductID: strconv.FormatInt(v.ID, 10), OfferID: v.OfferID,
			ImageCount: len(primary), Gallery: &ce.RemoteGallery{Photos: primary, Incomplete: !imagesOK || !primaryOK},
			RemoteStatus: v.Statuses.StatusName, IsCreated: v.Statuses.IsCreated}
		if len(v.Barcodes) > 0 {
			ids.Barcode = v.Barcodes[0]
		}
		if len(v.Sources) > 0 {
			ids.SKU = strconv.FormatInt(v.Sources[0].SKU, 10)
		}
		return ids, nil
	}
	// WB search is partial; accept an exact nmID or vendorCode only.
	var out struct {
		Cards []struct {
			ID      int64  `json:"nmID"`
			Article string `json:"vendorCode"`
			Photos  any    `json:"photos"`
			Video   string `json:"video"`
			Sizes   []struct {
				SKUs []string `json:"skus"`
			} `json:"sizes"`
		} `json:"cards"`
		Cursor struct {
			UpdatedAt string `json:"updatedAt"`
			NmID      int64  `json:"nmID"`
			Total     int    `json:"total"`
		} `json:"cursor"`
	}
	cursor := map[string]any{"limit": 100}
	var found []ce.RemoteIDs
	for page := 0; page < 100; page++ {
		if e := p.read(ctx, ch, "/content/v2/get/cards/list", map[string]any{"settings": map[string]any{"cursor": cursor, "filter": map[string]any{"textSearch": identifier, "withPhoto": -1}}}, &out); e != nil {
			return ce.RemoteIDs{}, e
		}
		for _, v := range out.Cards {
			if v.Article != identifier && (byArticle || strconv.FormatInt(v.ID, 10) != identifier) {
				continue
			}
			photos, ok := catalogPhotoURLs(v.Photos)
			ids := ce.RemoteIDs{ProductID: strconv.FormatInt(v.ID, 10), OfferID: v.Article,
				ImageCount: len(photos), Gallery: &ce.RemoteGallery{Photos: photos, Incomplete: !ok, Video: v.Video}}
			if len(v.Sizes) > 0 && len(v.Sizes[0].SKUs) > 0 {
				ids.Barcode = v.Sizes[0].SKUs[0]
			}
			found = append(found, ids)
		}
		if len(out.Cards) < 100 || out.Cursor.Total < 100 {
			break
		}
		if out.Cursor.UpdatedAt == "" {
			return ce.RemoteIDs{}, errors.New("WB не вернул курсор поиска")
		}
		cursor = map[string]any{"limit": 100, "updatedAt": out.Cursor.UpdatedAt, "nmID": out.Cursor.NmID}
		if page == 99 {
			return ce.RemoteIDs{}, errors.New("слишком много совпадений WB; уточните артикул")
		}
	}
	if len(found) > 1 {
		return ce.RemoteIDs{}, errors.New("найдено несколько товаров WB; укажите точный артикул WB")
	}
	if len(found) == 1 {
		return found[0], nil
	}
	return ce.RemoteIDs{}, nil
}
func (p *CatalogPublisher) Advance(ctx context.Context, ch string, r ce.Request, s ce.Progress) (ce.Result, error) {
	if ch != "wb" && ch != "ozon" {
		return ce.Result{}, errors.New("неизвестная площадка")
	}
	if s.Stage == "gallery-update-unknown" {
		return ce.Result{Progress: s, Status: "needs_attention", Message: "Площадка не подтвердила сохранение. Проверьте карточку и галерею в кабинете"}, nil
	}
	// Resolve a changed seller article before touching either remote card.
	// Returning after this read lets the worker reserve/check ownership first.
	if r.Action == "resolve" && s.Stage == "" {
		ids, e := p.lookup(ctx, ch, r.SellerArticle(ch), true)
		if e != nil {
			return ce.Result{}, e
		}
		if ids.ProductID != "" && ids.ProductID != "0" {
			r.ExistingID = ids.OfferID
			if ch == "wb" {
				r.ExistingID = ids.ProductID
			}
			hydrated, found, err := p.hydrateCatalogUpdate(ctx, ch, r)
			if err != nil {
				return ce.Result{}, err
			}
			s.UpdateRequest = &hydrated
			s.IDs = found
			s.Stage = "update"
			s.CreateAttempted = true
			return ce.Result{Progress: s, Status: "queued", Message: "Новая связь проверена. Подготовлено обновление найденной карточки."}, nil
		}
		cat, _ := strconv.ParseInt(r.Channel.Category.ID, 10, 64)
		raw, err := p.Executor.ReadEditorReference(ctx, ch, "attributes", cat, r.Channel.Category.TypeID, 0, "")
		if err != nil {
			return ce.Result{}, err
		}
		var schema struct {
			Result []ce.Attribute `json:"result"`
			Data   []ce.Attribute `json:"data"`
		}
		if err = json.Unmarshal(raw, &schema); err != nil {
			return ce.Result{}, err
		}
		r.Schema = append(schema.Result, schema.Data...)
		r.Action = "creating"
		s.UpdateRequest = &r
		s.Stage = "check"
		return ce.Result{Progress: s, Status: "queued", Message: "Артикул свободен. Подготовлено создание новой карточки."}, nil
	}
	if r.Action == "resolve" && s.UpdateRequest != nil {
		r = *s.UpdateRequest
	}
	if r.Action == "update" {
		if s.UpdateRequest == nil {
			hydrated, ids, e := p.hydrateCatalogUpdate(ctx, ch, r)
			if e != nil {
				return ce.Result{}, e
			}
			if r.ProcurementMeasurements && !hasProcurementMarketplaceMeasurement(hydrated, ch) {
				return ce.Result{Progress: s, Status: "needs_attention", Message: "В категории площадки не найдено поле для подтверждённого размера растения. Проверьте карточку."}, nil
			}
			s.UpdateRequest = &hydrated
			s.IDs = ids
			s.Stage = "update"
			s.CreateAttempted = true
			return ce.Result{Progress: s, Status: "queued", Message: "Обновляем связанную карточку. Создание новой не требуется."}, nil
		}
		r = *s.UpdateRequest
	}
	if e := validateCatalogRequest(ch, r, p.Mock); e != nil {
		if s.ReapplyPayload {
			s.Stage = "reconcile"
			return ce.Result{Progress: s}, e
		}
		return ce.Result{Progress: ce.Progress{Stage: "check", SitePhotos: s.SitePhotos}}, e
	}
	result := func(status, message string) (ce.Result, error) {
		return ce.Result{Progress: s, Status: status, Message: message}, nil
	}
	if r.Channel.Mode == "link" {
		ids, e := p.lookup(ctx, ch, strings.TrimSpace(r.Channel.ExistingID), false)
		if e != nil {
			return ce.Result{}, e
		}
		if ids.ProductID == "" || ids.ProductID == "0" {
			return ce.Result{}, errors.New("товар не найден в кабинете продавца")
		}
		s.IDs = ids
		return result("linked", "Товар найден и связан. Данные площадки не изменялись.")
	}
	if s.Stage == "" {
		s.Stage = "check"
	}
	if ch == "wb" && r.Action == "updating" && (s.Stage == "price" || s.Stage == "price-readback") {
		ids, err := p.lookup(ctx, ch, r.SellerArticle(ch), true)
		if err != nil {
			return ce.Result{Progress: s}, err
		}
		if ids.ProductID != s.IDs.ProductID {
			return result("needs_attention", "Связь WB изменилась. Цена не отправлялась")
		}
		matcher := newCatalogPhotoMatcher(nil)
		matcher.wildberries = true
		legacyPhotoWrite := !s.WBPhotoMatching && s.PhotoCount != nil && s.SitePhotos != nil
		check := s
		if legacyPhotoWrite {
			count := len(ids.Gallery.Photos)
			check.PhotoCount = &count
		}
		confirmed, err := matcher.confirms(ctx, ids.Gallery.Photos, check)
		if err != nil {
			return ce.Result{Progress: s}, err
		}
		if !confirmed || ids.Gallery.Incomplete && s.PhotoCount != nil {
			s.Checks++
			if s.Checks > 90 {
				return result("needs_attention", "WB не подтвердил фотографии обновлённой карточки")
			}
			return result("queued", "Ожидаем фотографии WB. Цена и скидка не менялись")
		}
		if legacyPhotoWrite {
			// An earlier worker may have appended the right photos but missed
			// copies with WB's black bars. Reconcile once against a fresh list.
			changes, err := matcher.delta(ctx, ids.Gallery.Photos, s.SitePhotos, r.Images, s.PhotoWrite)
			if err != nil {
				return ce.Result{Progress: s}, err
			}
			if len(changes.Added) != 0 {
				return result("needs_attention", "Wildberries не подтвердил загрузку фотографий. Проверьте галерею в кабинете")
			}
			if len(changes.Removed) > 0 {
				s.Stage = "media"
				s.Checks = 0
				return result("queued", "Найдены прежние снимки сайта с полями Wildberries. Подготавливаем их удаление")
			}
			s.PhotoCount = check.PhotoCount
			s.WBPhotoMatching = true
		}
		return result("synced", "Контент WB обновлён. Цена и скидка сохранены; изменения цен подтверждаются отдельно")
	}
	switch s.Stage {
	case "check", "reconcile":
		ids, e := p.lookup(ctx, ch, r.SellerArticle(ch), true)
		if e != nil {
			return ce.Result{}, e
		}
		if ids.ProductID != "" && ids.ProductID != "0" {
			if !s.CreateAttempted {
				return ce.Result{}, errors.New("этот артикул уже существует на площадке; выберите «Уже есть»")
			}
			if ids.Barcode != r.Barcode {
				return ce.Result{}, errors.New("артикул найден, но штрихкод не совпадает; нужна проверка связи")
			}
			if s.ReapplyPayload && s.IDs.ProductID != "" && ids.ProductID != s.IDs.ProductID {
				return ce.Result{}, errors.New("идентификатор ранее созданной карточки изменился; нужна проверка связи")
			}
			s.IDs = ids
			if ch == "ozon" && s.ReapplyPayload {
				s.Stage = "update"
				s.Ambiguous = false
				return result("queued", "Созданная этой попыткой карточка проверена. Отправим исправленные данные.")
			} else if ch == "wb" {
				if r.ProcurementMeasurements {
					s.Stage = "measurements-readback"
					return result("queued", "Проверяем размеры растения в карточке Wildberries.")
				}
				s.Stage = "media"
				if r.Action == "updating" && s.SitePhotos != nil {
					removed, added := catalogSitePhotoDelta(s.SitePhotos, r.Images)
					if len(removed) == 0 && len(added) == 0 {
						s.Stage = "price"
					}
				}
			} else {
				s.Stage = "ozon-readback"
			}
			return result("queued", "Карточка найдена. Проверяем результат.")
		}
		if s.Stage == "reconcile" || s.ReapplyPayload {
			s.Stage = "reconcile"
			s.Checks++
			if s.Checks > 90 {
				return result("needs_attention", "Площадка не подтвердила создание. Повторная отправка заблокирована во избежание дубля; проверьте кабинет.")
			}
			if ch == "wb" {
				if e := p.wbCardErrors(ctx, r.SellerArticle(ch)); e != nil {
					return ce.Result{}, e
				}
			}
			return result("queued", "Ожидаем появления карточки на площадке.")
		}
		s.Stage = "create"
		return result("queued", "Проверка пройдена. Карточка ожидает отправки.")
	case "create", "update":
		updating := s.Stage == "update"
		ownedUncreatedUpdate := false
		var live ce.RemoteIDs
		if updating {
			ids, e := p.lookup(ctx, ch, s.IDs.OfferID, true)
			if e != nil {
				return ce.Result{}, e
			}
			if ids.ProductID != s.IDs.ProductID || ids.OfferID != r.SellerArticle(ch) {
				return ce.Result{}, errors.New("связанный товар изменился; артикул созданной карточки менять нельзя")
			}
			if s.ReapplyPayload && (r.Barcode == "" || ids.Barcode != r.Barcode) {
				return ce.Result{}, errors.New("штрихкод ранее созданной карточки не подтверждён; нужна явная проверка связи")
			}
			// A positively identified rejected creation may not have a price-list
			// record yet. Its corrected initial import retains the creation intent.
			// Published cards and unknown vendor states must preserve live prices.
			creationRetry := s.ReapplyPayload && s.CreateAttempted && s.TaskID > 0 && r.Action == "create" && r.Channel.Mode == "create"
			trustedLink := r.Action == "updating" && r.ExistingID != "" && s.UpdateRequest != nil && ids.Barcode == s.IDs.Barcode
			live = ids
			ownedUncreatedUpdate = ch == "ozon" && ids.IsCreated != nil && !*ids.IsCreated && (creationRetry || trustedLink)
		}
		s.CreateAttempted = true
		if ch == "ozon" {
			if updating && !ownedUncreatedUpdate {
				if live.Gallery.Incomplete {
					return result("needs_attention", "Ozon не вернул полный список фотографий. Галерею нужно проверить")
				}
				if r.ProcurementMeasurements {
					r.Images = live.Gallery.Photos
				} else {
					changes, err := catalogGalleryDelta(ctx, nil, live.Gallery.Photos, s.SitePhotos, r.Images)
					if err != nil {
						return ce.Result{Progress: s}, err
					}
					r.Images = changes.apply(live.Gallery.Photos)
					if len(changes.Added)+len(changes.Removed) > 0 {
						count := len(r.Images)
						s.PhotoCount, s.PhotoWrite = &count, changes.Added
					}
				}
			}
			body, e := BuildOzonCard(r, p.Mock)
			if e != nil {
				if s.ReapplyPayload {
					s.Stage = "reconcile"
					return ce.Result{Progress: s}, e
				}
				return ce.Result{Progress: ce.Progress{Stage: "check", SitePhotos: s.SitePhotos}}, e
			}
			if updating && !ownedUncreatedUpdate {
				price, oldPrice, err := p.Executor.readEditorOzonPrice(ctx, s.IDs.OfferID)
				if err != nil {
					return ce.Result{Progress: s}, err
				}
				item := body["items"].([]any)[0].(map[string]any)
				item["price"] = strconv.FormatFloat(price, 'f', 2, 64)
				item["old_price"] = strconv.FormatFloat(oldPrice, 'f', 2, 64)
			}
			var out struct {
				Result struct {
					TaskID int64 `json:"task_id"`
				} `json:"result"`
			}
			e = p.write(ctx, ch, "/v3/product/import", body, &out)
			if e != nil {
				if ambiguousCatalogError(e) {
					s.Stage = "reconcile"
					if r.ProcurementMeasurements {
						s.Stage = "measurements-readback"
					} else if updating {
						s.Stage = "gallery-update-unknown"
						if s.ReapplyPayload {
							s.Stage = "ozon-reapply-unknown"
						}
					}
					s.Ambiguous = true
					return result("queued", "Ozon не подтвердил запись. Проверяем результат отправки.")
				}
				if s.ReapplyPayload {
					s.Stage = "reconcile"
				} else {
					s.CreateAttempted = false
					s.Stage = "check"
				}
				return ce.Result{Progress: s}, e
			}
			if out.Result.TaskID <= 0 {
				s.Stage = "reconcile"
				if r.ProcurementMeasurements {
					s.Stage = "measurements-readback"
				} else if updating {
					s.Stage = "gallery-update-unknown"
					if s.ReapplyPayload {
						s.Stage = "ozon-reapply-unknown"
					}
				}
				s.Ambiguous = true
				return result("queued", "Нет номера задания Ozon; проверяем товар по артикулу.")
			}
			s.TaskID = out.Result.TaskID
			s.Stage = "ozon-poll"
		} else {
			body, e := BuildWBCard(r, p.Mock)
			if e != nil {
				return ce.Result{Progress: ce.Progress{Stage: "check", SitePhotos: s.SitePhotos}}, e
			}
			var out struct {
				Error     bool   `json:"error"`
				ErrorText string `json:"errorText"`
			}
			endpoint := "/content/v2/cards/upload"
			var requestBody any = body
			if updating {
				updated, err := p.wbUpdate(ctx, s.IDs.ProductID, body)
				if err != nil {
					return ce.Result{}, err
				}
				requestBody = updated
				endpoint = "/content/v2/cards/update"
			}
			e = p.write(ctx, ch, endpoint, requestBody, &out)
			if e != nil {
				if ambiguousCatalogError(e) {
					s.Stage = "reconcile"
					if r.ProcurementMeasurements {
						s.Stage = "measurements-readback"
					} else if updating {
						s.Stage = "update"
					}
					s.Ambiguous = true
					return result("queued", "Ответ на создание не получен; ищем карточку без повторной отправки.")
				}
				return ce.Result{Progress: ce.Progress{Stage: "check", SitePhotos: s.SitePhotos}}, e
			}
			if out.Error {
				return ce.Result{Progress: ce.Progress{Stage: "check", SitePhotos: s.SitePhotos}}, errors.New(safeRemoteMessage(out.ErrorText))
			}
			s.Stage = "reconcile"
			if r.ProcurementMeasurements {
				s.Stage = "measurements-readback"
			}
		}
		return result("queued", "Данные отправлены. Площадка обрабатывает карточку.")
	case "ozon-poll":
		var out struct {
			Result struct {
				Items []struct {
					Offer     string `json:"offer_id"`
					ProductID int64  `json:"product_id"`
					Status    string `json:"status"`
					Errors    []struct {
						Code        string `json:"code"`
						Message     string `json:"message"`
						Description string `json:"description"`
						Level       string `json:"level"`
					} `json:"errors"`
				} `json:"items"`
			} `json:"result"`
		}
		if e := p.read(ctx, ch, "/v1/product/import/info", map[string]any{"task_id": s.TaskID}, &out); e != nil {
			return ce.Result{}, e
		}
		for _, v := range out.Result.Items {
			if v.Offer != r.SellerArticle(ch) {
				continue
			}
			for _, e := range v.Errors {
				if e.Level != "warning" {
					return ce.Result{}, fmt.Errorf("Ozon: %s: %s", e.Code, safeRemoteMessage(firstNonEmpty(e.Description, e.Message)))
				}
			}
			if v.Status == "failed" {
				return ce.Result{}, errors.New("Ozon отклонил импорт")
			}
			if v.Status == "imported" && v.ProductID > 0 {
				s.IDs.ProductID = strconv.FormatInt(v.ProductID, 10)
				s.IDs.OfferID = v.Offer
				s.Stage = "ozon-readback"
				if r.ProcurementMeasurements {
					s.Stage = "measurements-readback"
				}
				return result("queued", "Ozon импортировал карточку. Проверяем данные и модерацию.")
			}
		}
		s.Checks++
		if s.Checks > 90 {
			return result("needs_attention", "Ozon долго обрабатывает задание. Повторная отправка не выполнялась.")
		}
		return result("queued", "Ожидаем результат задания Ozon.")
	case "measurements-readback":
		return p.verifyProcurementMarketplaceMeasurements(ctx, ch, r, s)
	case "ozon-reapply-unknown":
		// No task receipt means the corrected import may already be in flight.
		// Read ownership again, but do not claim its content was applied or
		// blindly repeat an import based only on the existence of an old card.
		ids, err := p.lookup(ctx, ch, r.SellerArticle(ch), true)
		if err != nil {
			return ce.Result{}, err
		}
		if ids.ProductID != s.IDs.ProductID || ids.Barcode != r.Barcode || r.Barcode == "" {
			return result("needs_attention", "Результат обновления и связь карточки не подтверждены. Проверьте кабинет Ozon; повтор не отправлен.")
		}
		return result("needs_attention", "Карточка найдена, но результат исправленного импорта неизвестен. Проверьте задание Ozon; повтор не отправлен.")
	case "ozon-readback":
		ids, e := p.lookup(ctx, ch, r.SellerArticle(ch), true)
		if e != nil {
			return ce.Result{}, e
		}
		if ids.ProductID == "" {
			return result("queued", "Ожидаем синхронизацию карточки Ozon.")
		}
		if s.ReapplyPayload && (ids.ProductID != s.IDs.ProductID || r.Barcode == "" || ids.Barcode != r.Barcode) {
			return result("needs_attention", "Ozon не подтвердил идентификаторы обновлённой карточки. Проверьте связь; повтор не отправлен.")
		}
		s.IDs = ids
		confirmed, err := catalogPhotoWriteConfirmed(ctx, nil, ids.Gallery.Photos, s)
		if err != nil {
			return ce.Result{Progress: s}, err
		}
		if ids.ImageCount == 0 || !confirmed || ids.Gallery.Incomplete && s.PhotoCount != nil {
			s.Checks++
			if s.Checks > 90 {
				return result("needs_attention", "Ozon создал карточку, но не подтвердил фотографии. Проверьте кабинет.")
			}
			return result("queued", "Ozon обрабатывает фотографии карточки.")
		}
		if r.Action == "updating" {
			return result("synced", "Данные связанной карточки Ozon обновлены")
		}
		message := "Карточка создана в Ozon. Допуск к продаже зависит от проверки площадки."
		if ids.RemoteStatus != "" {
			message = "Карточка создана. Статус Ozon: " + safeRemoteMessage(ids.RemoteStatus)
		}
		return result("created", message)
	case "media":
		id, _ := strconv.ParseInt(s.IDs.ProductID, 10, 64)
		media := r.Images
		if r.Action == "updating" {
			live, err := p.lookup(ctx, ch, s.IDs.OfferID, true)
			if err != nil {
				return ce.Result{Progress: s}, err
			}
			if live.ProductID != s.IDs.ProductID || live.Gallery.Incomplete {
				return result("needs_attention", "Wildberries не подтвердил связь или полный список фотографий")
			}
			matcher := newCatalogPhotoMatcher(nil)
			matcher.wildberries = true
			changes, err := matcher.delta(ctx, live.Gallery.Photos, s.SitePhotos, r.Images, s.PhotoWrite)
			if err != nil {
				return ce.Result{Progress: s}, err
			}
			if len(changes.Removed)+len(changes.Added) == 0 {
				return result("synced", "Контент Wildberries обновлён. Состав фотографий на площадке сохранён")
			}
			media = changes.apply(live.Gallery.Photos)
			if err := validateCatalogPhotos(ch, media, p.Mock); err != nil {
				return ce.Result{Progress: s}, err
			}
			count := len(media)
			s.PhotoCount = &count
			s.PhotoWrite = append(s.PhotoWrite, changes.Added...)
			s.WBPhotoMatching = true
			if live.Gallery.Video != "" {
				video, err := url.Parse(live.Gallery.Video)
				if err != nil || video.Scheme != "https" || !(strings.HasSuffix(strings.ToLower(video.Path), ".mp4") || strings.HasSuffix(strings.ToLower(video.Path), ".mov")) {
					return result("needs_attention", "Wildberries вернул только ссылку на проигрыватель видео. Измените фотографии в кабинете, чтобы сохранить видео")
				}
				media = append(media, live.Gallery.Video)
			}
		}
		var out struct {
			Error     bool   `json:"error"`
			ErrorText string `json:"errorText"`
		}
		if e := p.write(ctx, ch, "/content/v3/media/save", map[string]any{"nmId": id, "data": media}, &out); e != nil {
			if r.Action == "updating" && ambiguousCatalogError(e) {
				s.Stage, s.Ambiguous = "gallery-update-unknown", true
				return result("needs_attention", "Wildberries не подтвердил запись фотографий. Проверьте галерею в кабинете")
			}
			return ce.Result{Progress: s}, e
		}
		if out.Error {
			return ce.Result{}, errors.New(safeRemoteMessage(out.ErrorText))
		}
		s.Stage = "price"
		if r.Action == "updating" {
			return result("queued", "Изменения фотографий отправлены в Wildberries. Проверяем результат")
		}
		return result("queued", "Фото отправлены в Wildberries. Устанавливаем цену.")
	case "price":
		id, _ := strconv.ParseInt(s.IDs.ProductID, 10, 64)
		price, e := catalogPrice(r.WBPrice)
		if e != nil {
			return ce.Result{}, e
		}
		var out struct {
			Error     bool   `json:"error"`
			ErrorText string `json:"errorText"`
		}
		if e = p.Executor.request(ctx, http.MethodPost, p.Executor.wbBase+"/api/v2/upload/task", map[string]any{"data": []any{map[string]any{"nmID": id, "price": int64(math.Ceil(price)), "discount": 0}}}, map[string]string{"Authorization": p.Executor.wbToken}, &out); e != nil {
			return ce.Result{}, e
		}
		if out.Error {
			return ce.Result{}, errors.New(safeRemoteMessage(out.ErrorText))
		}
		s.Stage = "price-readback"
		return result("queued", "Цена отправлена. Проверяем её применение.")
	case "price-readback":
		prices, e := p.Executor.readWBProductPrices(ctx, s.IDs.ProductID)
		if e != nil {
			return ce.Result{}, e
		}
		wanted, _ := catalogPrice(r.WBPrice)
		if len(prices) > 0 {
			all := true
			for _, price := range prices {
				if math.Abs(price-math.Ceil(wanted)) > .01 {
					all = false
				}
			}
			if all {
				ids, err := p.lookup(ctx, ch, r.SellerArticle(ch), true)
				if err != nil {
					return ce.Result{}, err
				}
				if ids.ImageCount < len(r.Images) {
					s.Checks++
					if s.Checks > 90 {
						return result("needs_attention", "WB создал карточку и применил цену, но не подтвердил все фотографии.")
					}
					return result("queued", "Ожидаем обработки фотографий Wildberries.")
				}
				s.IDs = ids
				return result("created", "Карточка Wildberries создана, фото отправлены, цена подтверждена. Допуск к продаже проверяется площадкой.")
			}
		}
		s.Checks++
		if s.Checks > 90 {
			return result("needs_attention", "Карточка создана, но WB пока не подтвердил цену. Проверьте кабинет.")
		}
		return result("queued", "Ожидаем применения цены Wildberries.")
	}
	return ce.Result{}, errors.New("неизвестный этап публикации")
}
func ambiguousCatalogError(err error) bool {
	var remote *remoteError
	return !errors.As(err, &remote) || remote.Status >= 500
}
func (p *CatalogPublisher) wbCardErrors(ctx context.Context, article string) error {
	cursor := map[string]any{"limit": 100}
	for page := 0; page < 100; page++ {
		var out struct {
			Data json.RawMessage `json:"data"`
		}
		if e := p.read(ctx, "wb", "/content/v2/cards/error/list", map[string]any{"cursor": cursor, "order": map[string]any{"ascending": false}}, &out); e != nil {
			return e
		}
		// Current API returns batches; retain support for older array responses.
		var batches struct {
			Items []struct {
				Errors map[string][]string `json:"errors"`
			} `json:"items"`
			Cursor struct {
				Next      bool   `json:"next"`
				UpdatedAt string `json:"updatedAt"`
				BatchUUID string `json:"batchUUID"`
			} `json:"cursor"`
		}
		if json.Unmarshal(out.Data, &batches) == nil {
			for _, batch := range batches.Items {
				if messages := batch.Errors[article]; len(messages) > 0 {
					return fmt.Errorf("Wildberries: %s", safeRemoteMessage(strings.Join(messages, "; ")))
				}
			}
			if !batches.Cursor.Next {
				return nil
			}
			if batches.Cursor.UpdatedAt == "" || batches.Cursor.BatchUUID == "" {
				return errors.New("WB не вернул курсор ошибок")
			}
			cursor = map[string]any{"limit": 100, "updatedAt": batches.Cursor.UpdatedAt, "batchUUID": batches.Cursor.BatchUUID}
			continue
		}
		var old []struct {
			VendorCode string   `json:"vendorCode"`
			Errors     []string `json:"errors"`
		}
		if e := json.Unmarshal(out.Data, &old); e != nil {
			return e
		}
		for _, v := range old {
			if v.VendorCode == article {
				return fmt.Errorf("Wildberries: %s", safeRemoteMessage(strings.Join(v.Errors, "; ")))
			}
		}
		return nil
	}
	return errors.New("слишком много страниц ошибок WB; повторите проверку позже")
}

func isLoopbackHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// LookupEditorCard only performs seller-owned catalogue reads.
func (e *MarketplaceExecutor) LookupEditorCard(ctx context.Context, channel, identifier string) (ce.RemoteIDs, error) {
	if channel != "ozon" && channel != "wb" {
		return ce.RemoteIDs{}, errors.New("неизвестная площадка")
	}
	return (&CatalogPublisher{Executor: e}).lookup(ctx, channel, strings.TrimSpace(identifier), false)
}

func (p *CatalogPublisher) wbUpdate(ctx context.Context, id string, created []map[string]any) ([]map[string]any, error) {
	var out struct {
		Cards []map[string]any `json:"cards"`
	}
	if e := p.read(ctx, "wb", "/content/v2/get/cards/list", map[string]any{"settings": map[string]any{"cursor": map[string]any{"limit": 100}, "filter": map[string]any{"textSearch": id, "withPhoto": -1}}}, &out); e != nil {
		return nil, e
	}
	for _, card := range out.Cards {
		remoteNumber, ok := card["nmID"].(float64)
		if !ok || strconv.FormatInt(int64(remoteNumber), 10) != id {
			continue
		}
		desired := created[0]["variants"].([]any)[0].(map[string]any)
		update := map[string]any{}
		for _, key := range []string{"nmID", "vendorCode", "title", "description", "brand", "dimensions", "characteristics", "sizes", "documents", "kizMarked", "wholesale", "gtin"} {
			if v, ok := card[key]; ok {
				update[key] = v
			}
		}
		for _, key := range []string{"vendorCode", "title", "description", "brand", "dimensions"} {
			update[key] = desired[key]
		}
		attrs := map[int64]map[string]any{}
		if old, ok := card["characteristics"].([]any); ok {
			for _, item := range old {
				if attr, ok := item.(map[string]any); ok {
					if n, ok := attr["id"].(float64); ok {
						attrs[int64(n)] = attr
					}
				}
			}
		}
		for _, attr := range desired["characteristics"].([]map[string]any) {
			attrs[attr["id"].(int64)] = attr
		}
		merged := []map[string]any{}
		for _, attr := range attrs {
			merged = append(merged, attr)
		}
		update["characteristics"] = merged
		// Existing size IDs, barcodes and documents are carried through verbatim.
		return []map[string]any{update}, nil
	}
	return nil, errors.New("связанная карточка WB не найдена")
}
