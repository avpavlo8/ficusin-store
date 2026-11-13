package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
)

// SabyEditorPublisher is exclusively the catalogue editor's write capability.
// It must only be invoked by the durable worker, whose committed create lease
// distinguishes the first Stage=create call from a resumed Stage=reconcile.
// No method of this provider is used by inbound stock synchronization.
type SabyEditorPublisher struct {
	Client      *SabyClient
	ImageClient *http.Client
}
type sabyEditorUploadedImage struct {
	Source string          `json:"source"`
	Result json.RawMessage `json:"result"`
}
type sabyEditorPublishState struct {
	Created             bool                      `json:"created"`
	Images              []sabyEditorUploadedImage `json:"images,omitempty"`
	ImageIDs            []string                  `json:"imageIds,omitempty"`
	PreparedData        json.RawMessage           `json:"preparedData,omitempty"`
	PreparedHash        string                    `json:"preparedHash,omitempty"`
	PreparedRequestHash string                    `json:"preparedRequestHash,omitempty"`
	PreparedBaseHash    string                    `json:"preparedBaseHash,omitempty"`
	PreparedID          string                    `json:"preparedId,omitempty"`
}

func (p *SabyEditorPublisher) Advance(ctx context.Context, ch string, r ce.Request, s ce.Progress) (ce.Result, error) {
	result := func(status, message string) (ce.Result, error) {
		return ce.Result{Progress: s, Status: status, Message: message}, nil
	}
	if ch != "saby" || p == nil || !p.Client.Configured() {
		return ce.Result{}, errors.New("Подключение публикации СБИС не настроено")
	}
	var native sabyEditorPublishState
	if len(s.Native) > 0 {
		if json.Unmarshal(s.Native, &native) != nil {
			return result("needs_attention", "Повреждён сохранённый результат СБИС. Повторное создание не выполнялось.")
		}
	}
	saveState := func() { s.Native, _ = json.Marshal(native) }
	if s.Stage == "reconcile" && s.IDs.ProductID == "" {
		return result("needs_attention", "Результат создания в СБИС неизвестен. Проверьте карточку вручную: повторное создание запрещено.")
	}
	if s.Stage == "" {
		if r.Action == "update" && r.SabyID == "" && r.ExistingID == "" && s.IDs.ProductID == "" {
			return result("needs_attention", "Для обновления СБИС не найден идентификатор существующей карточки. Создание не выполнялось.")
		}
		if r.SabyID != "" || r.ExistingID != "" || s.IDs.ProductID != "" {
			if s.IDs.ProductID == "" {
				s.IDs.ProductID = r.SabyID
				if s.IDs.ProductID == "" {
					s.IDs.ProductID = r.ExistingID
				}
			}
			s.Stage = "hydrate"
			return result("queued", "Проверяем существующую карточку СБИС.")
		}
		if r.Saby.Mode == "link" {
			return result("needs_attention", "Не найден однозначный числовой идентификатор выбранной карточки СБИС.")
		}
		if err := validateSabyEditorPublishRequest(r); err != nil {
			return ce.Result{Progress: s}, err
		}
		// Resolve both native namespaces before an irreversible server draft exists.
		refs := &SabyEditorHierarchySource{Client: p.Client}
		if _, err := refs.ResolveSabyEditorHierarchy(ctx, "category", r.Saby.Category.ID); err != nil {
			return ce.Result{Progress: s}, err
		}
		if _, err := refs.ResolveSabyEditorHierarchy(ctx, "folder", r.Saby.Folder.ID); err != nil {
			return ce.Result{Progress: s}, err
		}
		s.Stage = "create"
		return result("queued", "Данные проверены. Подготовлено создание карточки СБИС.")
	}
	if s.Stage == "create" {
		if !s.CreateAttempted || s.IDs.ProductID != "" || r.SabyID != "" || r.ExistingID != "" || r.Action == "update" {
			return result("needs_attention", "Создание СБИС остановлено: нет однозначного разрешения новой операции. Проверьте сохранённую связь.")
		}
		if err := validateSabyEditorPublishRequest(r); err != nil {
			return ce.Result{Progress: s}, err
		}
		category, _ := strconv.ParseInt(r.Saby.Category.ID, 10, 64)
		folder, _ := strconv.ParseInt(r.Saby.Folder.ID, 10, 64)
		data := sabyRecord(map[string]any{"Name": strings.TrimSpace(r.Name), "Category": category, "Folder": folder, "IsDraft": true})
		var response json.RawMessage
		if err := p.Client.sabyEditorWriteRPCOnce(ctx, "Nomenclature.Create", map[string]any{"Data": data, "Option": sabyRecord(map[string]any{}), "AdditionalFields": sabyEditorCardFields()}, &response); err != nil {
			s.Stage = "reconcile"
			s.Ambiguous = true
			return result("needs_attention", "СБИС не подтвердил результат создания. Нужна ручная проверка; повторная карточка не отправлялась.")
		}
		card, err := parseSabyEditorSingleNativeRecord(response)
		if err != nil {
			s.Stage = "reconcile"
			s.Ambiguous = true
			return result("needs_attention", "СБИС вернул неизвестный результат создания. Повторное создание запрещено.")
		}
		id, e := card.scalarString("Id")
		n, parseErr := strconv.ParseInt(id, 10, 64)
		if e != nil || parseErr != nil || n <= 0 {
			s.Stage = "reconcile"
			s.Ambiguous = true
			return result("needs_attention", "СБИС не вернул идентификатор созданной карточки. Нужна ручная проверка.")
		}
		s.IDs.ProductID = id
		s.IDs.UUID, _ = card.scalarString("Identifier")
		s.IDs.OfferID, _ = card.scalarString("Number")
		s.IDs.SKU = r.SKU
		native.Created = true
		saveState()
		s.Stage = "hydrate"
		return result("queued", "Черновик создан в СБИС. Идентификатор сохранён; заполняем карточку.")
	}
	id, err := strconv.ParseInt(s.IDs.ProductID, 10, 64)
	if err != nil || id <= 0 {
		return result("needs_attention", "Нет сохранённого идентификатора карточки СБИС. Создание не повторялось.")
	}
	if s.Stage == "hydrate" || s.Stage == "update" || s.Stage == "reconcile" {
		card, err := p.Client.readSabyEditorNativeCard(ctx, id)
		if err != nil {
			return ce.Result{Progress: s}, err
		}
		if err = validateSabyEditorNativeIdentity(card, r, s.IDs); err != nil {
			return result("needs_attention", err.Error())
		}
		s.IDs.UUID, _ = card.scalarString("Identifier")
		s.IDs.OfferID, _ = card.scalarString("Number")
		s.IDs.SKU = r.SKU
		if r.Saby.Mode == "link" && r.Action != "update" && !native.Created {
			s.Stage = "done"
			return result("linked", "Карточка СБИС найдена и связана. Данные площадки не изменялись.")
		}
		if err = validateSabyEditorPublishRequest(r); err != nil {
			return ce.Result{Progress: s}, err
		}
		folder, _ := card.scalarString("Folder")
		if folder != r.Saby.Folder.ID && !native.Created {
			s.Stage = "move"
			saveState()
			return result("queued", "Подготовлен перенос карточки СБИС в выбранный раздел.")
		}
		s.Stage = "media"
		saveState()
		return result("queued", "Связь проверена. Подготавливаем фотографии СБИС.")
	}

	if s.Stage == "move" || s.Stage == "move-verify" {
		card, err := p.Client.readSabyEditorNativeCard(ctx, id)
		if err != nil {
			return ce.Result{Progress: s}, err
		}
		if err = validateSabyEditorNativeIdentity(card, r, s.IDs); err != nil {
			return result("needs_attention", err.Error())
		}
		current, _ := card.scalarString("Folder")
		if current == r.Saby.Folder.ID {
			s.Stage = "media"
			return result("queued", "Раздел карточки СБИС проверен.")
		}
		if s.Stage == "move-verify" {
			return result("needs_attention", "СБИС не подтвердил перенос в выбранный раздел. Повторный перенос не отправлялся.")
		}
		folder, _ := strconv.ParseInt(r.Saby.Folder.ID, 10, 64)
		if _, err := (&SabyEditorHierarchySource{Client: p.Client}).ResolveSabyEditorHierarchy(ctx, "folder", r.Saby.Folder.ID); err != nil {
			return ce.Result{Progress: s}, err
		}
		s.Stage = "move-verify"
		if err = p.Client.moveSabyEditorFolder(ctx, id, folder); err != nil {
			return result("needs_attention", err.Error())
		}
		return result("queued", "Перенос отправлен в СБИС. Проверяем выбранный раздел.")
	}
	if s.Stage == "media" {
		if len(native.Images) > len(r.Images) {
			return result("needs_attention", "Состав фотографий изменился во время публикации СБИС. Нужна проверка.")
		}
		for i, image := range native.Images {
			if image.Source != r.Images[i] {
				return result("needs_attention", "Порядок фотографий изменился во время публикации СБИС. Нужна проверка.")
			}
		}
		if len(native.Images) < len(r.Images) {
			source := r.Images[len(native.Images)]
			data, ext, err := downloadSabyEditorImage(ctx, p.ImageClient, source)
			if err != nil {
				return ce.Result{Progress: s}, err
			}
			upload, err := p.Client.uploadSabyEditorImage(ctx, id, data, ext)
			if err != nil {
				s.Stage = "media-unknown"
				return result("needs_attention", "СБИС не подтвердил загрузку фотографии. Карточка сохранена в черновике; нужна проверка.")
			}
			raw, _ := json.Marshal(upload.raw)
			native.Images = append(native.Images, sabyEditorUploadedImage{Source: source, Result: raw})
			saveState()
			return result("queued", "Фотография загружена в СБИС. Продолжаем заполнение карточки.")
		}
		s.Stage = "save"
		saveState()
		return result("queued", "Фотографии подготовлены. Сохраняем сведения карточки СБИС.")
	}
	if s.Stage == "save" {
		card, err := p.Client.readSabyEditorNativeCard(ctx, id)
		if err != nil {
			return ce.Result{Progress: s}, err
		}
		if err = validateSabyEditorNativeIdentity(card, r, s.IDs); err != nil {
			return result("needs_attention", err.Error())
		}
		fields, imageIDs, err := p.buildSabyEditorFields(card, r, native)
		if err != nil {
			return ce.Result{Progress: s}, err
		}
		data, err := card.selectFields(fields)
		if err != nil {
			return ce.Result{Progress: s}, err
		}
		native.ImageIDs = imageIDs
		native.PreparedData = data
		native.PreparedHash = sabyEditorPayloadHash(data)
		requestData, _ := json.Marshal(r)
		native.PreparedRequestHash = sabyEditorPayloadHash(requestData)
		native.PreparedBaseHash, err = sabyEditorBaselineHash(card, data)
		if err != nil {
			return ce.Result{Progress: s}, err
		}
		native.PreparedID = s.IDs.ProductID
		saveState()
		s.Stage = "send-save"
		// No external mutation in this advance. The worker must commit this
		// exact payload and all expected photo IDs before leasing send-save.
		return result("queued", "Данные сохранения СБИС подготовлены. Отправляем после сохранения подтверждения операции.")
	}
	if s.Stage == "send-save" {
		if !sabyEditorPreparedValid(native, r, s.IDs.ProductID) {
			return result("needs_attention", "Подтверждение подготовленного сохранения СБИС не совпало. Запрос не отправлен.")
		}
		card, err := p.Client.readSabyEditorNativeCard(ctx, id)
		if err != nil {
			return ce.Result{Progress: s}, err
		}
		if err = validateSabyEditorNativeIdentity(card, r, s.IDs); err != nil {
			return result("needs_attention", err.Error())
		}
		baseline, err := sabyEditorBaselineHash(card, native.PreparedData)
		if err != nil || baseline != native.PreparedBaseHash {
			return result("needs_attention", "Карточка СБИС изменилась после подготовки. Сначала проверьте изменения на площадке.")
		}
		s.Stage = "verify"
		// The committed send-save receipt already includes every expected
		// image ID. A crash after POST recovers by readback without repeating it.
		// UI Update finalizes the draft; no invented IsDraft=false is sent.
		if err = p.Client.sabyEditorWriteRPCOnce(ctx, "Nomenclature.Update", map[string]any{"Id": id, "Data": native.PreparedData, "Option": sabyRecord(map[string]any{})}, nil); err != nil {
			s.Ambiguous = true
			return result("queued", "Ответ сохранения СБИС не получен. Проверяем фактические данные без повторного создания.")
		}
		return result("queued", "СБИС принял сохранение. Проверяем цену, описание, штрихкод и фотографии.")
	}
	if s.Stage == "verify" {
		if !sabyEditorPreparedValid(native, r, s.IDs.ProductID) {
			return result("needs_attention", "Нет полного сохранённого подтверждения отправки СБИС. Повторный запрос не выполнялся.")
		}
		card, err := p.Client.readSabyEditorNativeCard(ctx, id)
		if err != nil {
			return ce.Result{Progress: s}, err
		}
		if err = validateSabyEditorNativeIdentity(card, r, s.IDs); err != nil {
			return result("needs_attention", err.Error())
		}
		if err = p.verifySabyEditorFields(card, r, native); err != nil {
			return result("needs_attention", err.Error())
		}
		s.IDs.Barcode = r.Barcode
		s.IDs.ImageCount = len(native.ImageIDs)
		s.Stage = "done"
		s.Ambiguous = false
		if native.Created {
			return result("created", "Карточка создана в СБИС. Все переданные данные проверены чтением с площадки.")
		}
		return result("synced", "Изменения карточки СБИС сохранены и проверены.")
	}
	if s.Stage == "done" {
		if native.Created {
			return result("created", "Карточка СБИС уже проверена.")
		}
		return result("synced", "Карточка СБИС уже проверена.")
	}
	return result("needs_attention", "Операция СБИС остановлена для проверки. Повторное создание не выполняется.")
}

func sabyEditorCardFields() []string {
	return []string{"CodeList", "Images", "ImageData", "PriceData", "Category", "CategoryName", "CategoryObject", "Number", "Name", "NameBase", "Description", "Identifier", "IsDraft", "VAT", "MeasureUnit", "Path"}
}
func validateSabyEditorPublishRequest(r ce.Request) error {
	if _, err := sabyEditorDescription(r.Description); err != nil {
		return err
	}
	if strings.TrimSpace(r.Name) == "" || r.PriceMinor <= 0 {
		return errors.New("Заполните название и цену сайта для СБИС")
	}
	for _, id := range []string{r.Saby.Category.ID, r.Saby.Folder.ID} {
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil || n <= 0 {
			return errors.New("Выберите действительные категорию и раздел СБИС")
		}
	}
	if err := validateSabyEditorEAN13(r.Barcode); err != nil {
		return err
	}
	if len(r.Images) == 0 || len(r.Images) > 30 {
		return errors.New("Для СБИС добавьте от 1 до 30 фотографий")
	}
	for _, image := range r.Images {
		u, err := url.Parse(image)
		if err != nil || validateSabyEditorImageURL(u) != nil {
			return errors.New("Для СБИС нужны публичные HTTPS-фотографии")
		}
	}
	return nil
}

func sabyEditorPayloadHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func sabyEditorPreparedValid(native sabyEditorPublishState, r ce.Request, id string) bool {
	requestData, _ := json.Marshal(r)
	return len(native.PreparedData) > 0 && native.PreparedHash != "" && native.PreparedID == id && sabyEditorPayloadHash(native.PreparedData) == native.PreparedHash && sabyEditorPayloadHash(requestData) == native.PreparedRequestHash && len(r.Images) > 0 && len(native.ImageIDs) >= len(r.Images)
}

// Only semantically stable owned values participate. Saby may rotate gallery
// URLs or price/cache metadata between reads without any catalogue edit.
func sabyEditorBaselineHash(card *sabyEditorNativeRecord, prepared json.RawMessage) (string, error) {
	owned, err := parseSabyEditorNativeRecord(prepared)
	if err != nil {
		return "", err
	}
	semantic := map[string]any{}
	for _, name := range []string{"Name", "NameBase", "Category", "Folder", "Description"} {
		if _, ok := owned.fields[name]; !ok {
			continue
		}
		var value any
		decoder := json.NewDecoder(strings.NewReader(string(card.value(name))))
		decoder.UseNumber()
		if decoder.Decode(&value) != nil {
			return "", errors.New("СБИС не вернул поле исходной карточки")
		}
		semantic[name] = value
	}
	for field, names := range map[string][]string{
		"CodeList":  {"Id", "Code", "Type", "Order", "Packing", "PartyCode", "AccountCode"},
		"PriceData": {"Id", "PriceList", "Price", "MeasureUnit"},
		"ImageData": {"id", "isMaster", "canEdit", "canDelete", "isMarketImage"},
	} {
		if _, ok := owned.fields[field]; !ok {
			continue
		}
		set, e := parseSabyEditorNativeRecordSet(card.value(field))
		if e != nil {
			return "", e
		}
		rows := []map[string]any{}
		for i := range set.rows {
			row := map[string]any{}
			for _, name := range names {
				raw := set.cell(i, name)
				if len(raw) == 0 {
					continue
				}
				var value any
				decoder := json.NewDecoder(strings.NewReader(string(raw)))
				decoder.UseNumber()
				if decoder.Decode(&value) != nil {
					return "", errors.New("СБИС вернул неподдерживаемое поле таблицы")
				}
				row[name] = value
			}
			rows = append(rows, row)
		}
		semantic[field] = rows
	}
	baseline, err := json.Marshal(semantic)
	if err != nil {
		return "", err
	}
	return sabyEditorPayloadHash(baseline), nil
}
