package integration

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	"github.com/avpavlo8/ficusin-store/backend/internal/money"
)

var sabyEditorUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func validateSabyEditorNativeIdentity(card *sabyEditorNativeRecord, r ce.Request, ids ce.RemoteIDs) error {
	uuid, e := card.scalarString("Identifier")
	if e != nil || !sabyEditorUUID.MatchString(uuid) {
		return errors.New("СБИС не вернул UUID карточки для проверки связи")
	}
	for _, expected := range []string{r.SabyUUID, ids.UUID} {
		if expected != "" && !strings.EqualFold(uuid, expected) {
			return errors.New("UUID карточки СБИС не совпадает с сохранённой связью")
		}
	}
	number, e := card.scalarString("Number")
	if e != nil || number == "" {
		return errors.New("СБИС не вернул код карточки для проверки связи")
	}
	expectedCode := ""
	if r.Saby.Mode == "link" || r.Action == "update" {
		expectedCode = r.Saby.ExistingCode
	}
	for _, expected := range []string{expectedCode, ids.OfferID} {
		if expected != "" && number != expected {
			return errors.New("Код карточки СБИС не совпадает с сохранённой связью")
		}
	}
	return nil
}
func (p *SabyEditorPublisher) buildSabyEditorFields(card *sabyEditorNativeRecord, r ce.Request, state sabyEditorPublishState) (map[string]json.RawMessage, []string, error) {
	fields := map[string]json.RawMessage{}
	set := func(key string, value any) { fields[key], _ = json.Marshal(value) }
	set("Name", strings.TrimSpace(r.Name))
	set("NameBase", strings.TrimSpace(r.Name))
	category, _ := strconv.ParseInt(r.Saby.Category.ID, 10, 64)
	set("Category", category)
	if state.Created {
		folder, _ := strconv.ParseInt(r.Saby.Folder.ID, 10, 64)
		set("Folder", folder)
	}
	description, err := sabyEditorDescription(r.Description)
	if err != nil {
		return nil, nil, err
	}
	fields["Description"] = description
	fields["CodeList"], err = sabyEditorAddEAN13(card.value("CodeList"), r.Barcode)
	if err != nil {
		return nil, nil, err
	}
	// Existing-card content saves do not approve a price change. Omit the
	// unmanaged price list so only the explicit price publication can alter it.
	if state.Created {
		if state.BasePriceIncluded {
			fields["PriceData"], err = sabyEditorSetPricePair(card.value("PriceData"), p.Client.priceListID, r.PriceMinor)
		} else {
			fields["PriceData"], err = sabyEditorSetPrice(card.value("PriceData"), p.Client.priceListID, r.PriceMinor)
		}
		if err != nil {
			return nil, nil, err
		}
	}
	if state.Gallery != nil && !state.Gallery.changed() {
		return fields, nil, nil
	}
	images, ids, err := sabyEditorImageDataWithDelta(card.value("ImageData"), state.Images, state.Gallery)
	if err != nil {
		return nil, nil, err
	}
	fields["ImageData"] = images
	return fields, ids, nil
}
func sabyEditorImageData(raw json.RawMessage, uploads []sabyEditorUploadedImage) (json.RawMessage, []string, error) {
	return sabyEditorImageDataWithDelta(raw, uploads, nil)
}

func sabyEditorImageDataWithDelta(raw json.RawMessage, uploads []sabyEditorUploadedImage, delta *sabyEditorGalleryDelta) (json.RawMessage, []string, error) {
	set, err := parseSabyEditorNativeRecordSet(raw)
	if err != nil {
		return nil, nil, err
	}
	for _, field := range []string{"id", "url", "fullUrl", "isMaster"} {
		if _, ok := set.fields[field]; !ok {
			return nil, nil, errors.New("СБИС не вернул схему фотографий карточки")
		}
	}
	protected := [][]json.RawMessage{}
	if delta != nil {
		protected, err = delta.retainedRows(set)
		if err != nil {
			return nil, nil, err
		}
	} else {
		for i, row := range set.rows {
			if !sabyEditorImageEditable(set, i) {
				var isMaster bool
				_ = json.Unmarshal(set.cell(i, "isMaster"), &isMaster)
				if isMaster {
					return nil, nil, errors.New("Главное фото СБИС недоступно для изменения: сначала проверьте обложку на площадке")
				}
				protected = append(protected, row)
			}
		}
	}
	set.rows = nil
	ids := []string{}
	seen := map[string]bool{}
	for i, upload := range uploads {
		record, e := parseSabyEditorNativeRecord(upload.Result)
		if e != nil {
			return nil, nil, e
		}
		value := func(names ...string) string {
			for _, name := range names {
				v, _ := record.scalarString(name)
				if v != "" {
					return v
				}
			}
			return ""
		}
		id := value("UUID", "md")
		if id == "" || seen[id] {
			return nil, nil, errors.New("СБИС вернул повторяющийся идентификатор фотографии")
		}
		seen[id] = true
		relative, full := value("UrlRelative", "public_href_cstorage"), value("Url", "href")
		if relative == "" && full == "" {
			return nil, nil, errors.New("СБИС не вернул адрес загруженной фотографии")
		}
		name := value("Name", "name")
		if name == "" {
			name = "ficusin-photo"
		}
		row := make([]json.RawMessage, len(set.schema))
		for n := range row {
			row[n] = json.RawMessage("null")
		}
		set.rows = append(set.rows, row)
		values := map[string]any{"id": id, "fileName": name, "title": name, "isMaster": i == 0, "canDelete": true, "canEdit": true, "size": nil, "url": relative, "UrlVideo": value("UrlVideo", "url_video"), "fullUrl": full, "isMarketImage": false, "marketImageId": nil, "coords": "{}", "readFromDisk": true}
		if auto := record.value("AutoPlay"); len(auto) > 0 {
			var enabled bool
			if json.Unmarshal(auto, &enabled) == nil {
				values["autoPlay"] = enabled
			}
		}
		for field, value := range values {
			if _, ok := set.fields[field]; ok {
				if e = set.set(i, field, value); e != nil {
					return nil, nil, e
				}
			}
		}
		ids = append(ids, id)
	}
	for _, row := range protected {
		var id string
		if json.Unmarshal(row[set.fields["id"]], &id) != nil || id == "" || seen[id] {
			return nil, nil, errors.New("СБИС вернул неоднозначное защищённое фото")
		}
		seen[id] = true
		set.rows = append(set.rows, row)
		ids = append(ids, id)
	}
	if delta != nil {
		if len(ids) == 0 || len(ids) > 30 {
			return nil, nil, errors.New("СБИС допускает от 1 до 30 фото. Галерея площадки сохранена")
		}
		// Existing rows keep their order, cover and metadata. A newly uploaded
		// image is appended; a replacement cover is needed only after deletion.
		set.rows = append(append([][]json.RawMessage{}, set.rows[len(uploads):]...), set.rows[:len(uploads)]...)
		ids = append(append([]string{}, ids[len(uploads):]...), ids[:len(uploads)]...)
		hasMaster := false
		for i := range protected {
			var master bool
			_ = json.Unmarshal(set.cell(i, "isMaster"), &master)
			hasMaster = hasMaster || master
		}
		for i := range set.rows {
			if i >= len(protected) || !hasMaster && i == 0 {
				if err := set.set(i, "isMaster", !hasMaster && i == 0); err != nil {
					return nil, nil, err
				}
			}
		}
	}
	encoded, e := set.marshal()
	return encoded, ids, e
}
func (p *SabyEditorPublisher) verifySabyEditorFields(card *sabyEditorNativeRecord, r ce.Request, state sabyEditorPublishState) error {
	for field, want := range map[string]string{"Name": strings.TrimSpace(r.Name), "Category": r.Saby.Category.ID, "Folder": r.Saby.Folder.ID} {
		got, e := card.scalarString(field)
		if e != nil || got != want {
			return fmt.Errorf("СБИС ещё не подтвердил поле %s. Повторное создание не выполнялось", field)
		}
	}
	var draft bool
	if json.Unmarshal(card.value("IsDraft"), &draft) != nil || draft {
		return errors.New("СБИС ещё не подтвердил окончательное сохранение карточки")
	}
	expected, err := sabyEditorDescription(r.Description)
	if err != nil {
		return err
	}
	if !sabyEditorJSONEqual(card.value("Description"), expected) {
		return errors.New("СБИС не подтвердил сохранённое описание")
	}
	codes, err := parseSabyEditorNativeRecordSet(card.value("CodeList"))
	if err != nil {
		return err
	}
	barcodeFound := false
	for i := range codes.rows {
		var code string
		json.Unmarshal(codes.cell(i, "Code"), &code)
		typ, e := sabyEditorNativeInt(codes.cell(i, "Type"))
		if code == r.Barcode && e == nil && typ == 2 {
			barcodeFound = true
		}
	}
	if !barcodeFound {
		return errors.New("СБИС не подтвердил штрихкод EAN-13")
	}
	if state.Created && state.BasePriceIncluded {
		matched, err := sabyEditorBasePriceMatches(card, r.PriceMinor)
		if err != nil || !matched {
			return errors.New("Основная цена СБИС не совпала с ценой сайта")
		}
	}
	if state.Created {
		prices, err := parseSabyEditorNativeRecordSet(card.value("PriceData"))
		if err != nil {
			return err
		}
		priceFound := false
		for i := range prices.rows {
			list, e := sabyEditorNativeInt(prices.cell(i, "PriceList"))
			if e == nil && list == p.Client.priceListID {
				var n json.Number
				if json.Unmarshal(prices.cell(i, "Price"), &n) != nil {
					return errors.New("СБИС не вернул сохранённую цену")
				}
				minor, e := money.Parse(string(n))
				if e != nil || minor != r.PriceMinor {
					return errors.New("Цена СБИС не совпала с ценой сайта")
				}
				if priceFound {
					return errors.New("СБИС вернул неоднозначную цену")
				}
				priceFound = true
			}
		}
		if !priceFound {
			return errors.New("СБИС не подтвердил цену выбранного прайс-листа")
		}
	}
	if state.Gallery != nil && !state.Gallery.changed() {
		return nil
	}
	images, err := parseSabyEditorNativeRecordSet(card.value("ImageData"))
	if err != nil {
		return err
	}
	if len(images.rows) != len(state.ImageIDs) || state.Gallery == nil && len(state.ImageIDs) < len(r.Images) {
		return errors.New("СБИС не подтвердил все фотографии")
	}
	var expectedPhotos *sabyEditorNativeRecordSet
	if state.Gallery != nil {
		prepared, err := parseSabyEditorNativeRecord(state.PreparedData)
		if err != nil {
			return err
		}
		expectedPhotos, err = parseSabyEditorNativeRecordSet(prepared.value("ImageData"))
		if err != nil {
			return err
		}
	}
	for i, want := range state.ImageIDs {
		var id string
		json.Unmarshal(images.cell(i, "id"), &id)
		var master bool
		json.Unmarshal(images.cell(i, "isMaster"), &master)
		wantMaster := i == 0
		if expectedPhotos != nil {
			_ = json.Unmarshal(expectedPhotos.cell(i, "isMaster"), &wantMaster)
		}
		if id != want || master != wantMaster {
			return errors.New("СБИС не подтвердил порядок фотографий и обложку")
		}
	}
	return nil
}
func sabyEditorJSONEqual(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	left, _ := json.Marshal(x)
	right, _ := json.Marshal(y)
	return string(left) == string(right)
}
