package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
)

type sabyEditorImageRoundTrip struct{}

func (sabyEditorImageRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(png)), Request: r}, nil
}

type sabyEditorNativeFixture struct {
	t                                *testing.T
	server                           *httptest.Server
	card                             map[string]any
	creates, updates, uploads, reads int
	lostCreate, lostUpdate           bool
}

func newSabyEditorNativeFixture(t *testing.T) *sabyEditorNativeFixture {
	f := &sabyEditorNativeFixture{t: t}
	native := func(fields []string, types []any, values []any) map[string]any {
		schema := []any{}
		for i, name := range fields {
			schema = append(schema, map[string]any{"n": name, "t": types[i]})
		}
		return map[string]any{"_type": "record", "s": schema, "d": values}
	}
	codes := native([]string{"Id", "Code", "Type", "Packing", "PartyCode", "AccountCode", "Order"}, []any{"Число целое", "Строка", "Число целое", "JSON-объект", "Строка", "JSON-объект", "Число целое"}, []any{})
	codes["_type"] = "recordset"
	codes["d"] = []any{[]any{float64(-1), "Xfixture", float64(-1), nil, nil, nil, nil}}
	prices := native([]string{"Id", "PriceList", "Price", "CanEdit", "MeasureUnit", "IsUpdated"}, []any{"Строка", "Число целое", map[string]any{"n": "Деньги", "p": 2}, "Логическое", "Строка", "Логическое"}, []any{})
	prices["_type"] = "recordset"
	prices["d"] = []any{[]any{"6_base", float64(6), float64(100), true, nil, nil}}
	imageFields := []string{"id", "url", "fullUrl", "fileName", "title", "size", "isMaster", "canDelete", "canEdit", "UrlVideo", "isMarketImage", "marketImageId", "coords", "readFromDisk"}
	imageTypes := []any{"Строка", "Строка", "Строка", "Строка", "Строка", "Число целое", "Логическое", "Логическое", "Логическое", "Строка", "Логическое", "Число целое", "Строка", "Логическое"}
	images := native(imageFields, imageTypes, []any{})
	images["_type"] = "recordset"
	images["d"] = []any{}
	f.card = native([]string{"Id", "Folder", "Category", "Name", "NameBase", "Number", "Identifier", "IsDraft", "Description", "CodeList", "PriceData", "ImageData"}, []any{"Число целое", "Число целое", "Число целое", "Строка", "Строка", "Строка", "UUID", "Логическое", "JSON-объект", "Выборка", "Выборка", "Выборка"}, []any{float64(1101), float64(3333), float64(1041), "Старое название", "Старое название", "Xfixture", "0f56c2bd-2669-473f-9b94-875d521998a7", false, []any{[]any{"p", map[string]any{"version": "2"}, "Старое описание"}}, codes, prices, images})
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth" {
			io.WriteString(w, `{"token":"fixture"}`)
			return
		}
		var req struct {
			Method string
			Params map[string]any
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		reply := func(value any) { json.NewEncoder(w).Encode(map[string]any{"result": value}) }
		switch req.Method {
		case "NomenclatureCategory.Read":
			reply(native([]string{"Id", "Name"}, []any{"Число целое", "Строка"}, []any{float64(1041), "Комнатные растения"}))
		case "Nomenclature.Read":
			if req.Params["ИдО"] != nil {
				if req.Params["ИдО"] != float64(3333) {
					t.Error("wrong folder")
				}
				reply(native([]string{"@Номенклатура", "Наименование"}, []any{"Число целое", "Строка"}, []any{float64(3333), "Цветы маркетплейс"}))
				return
			}
			f.reads++
			if req.Params["Id"] != float64(1101) {
				t.Error("wrong product")
			}
			option, _ := req.Params["Option"].(map[string]any)
			lists, _ := sabyRecordField(option, "PriceList").([]any)
			if len(lists) != 1 || lists[0] != float64(6) {
				t.Error("price list was not explicitly scoped")
			}
			reply(f.card)
		case "Nomenclature.Create":
			f.creates++
			data, _ := req.Params["Data"].(map[string]any)
			if sabyRecordField(data, "IsDraft") != true || sabyRecordField(data, "Category") != float64(1041) || sabyRecordField(data, "Folder") != float64(3333) {
				t.Error("unverified create schema")
			}
			setSabyRecordField(f.card, "IsDraft", true)
			if f.lostCreate {
				w.WriteHeader(500)
				return
			}
			reply(f.card)
		case "NomenclatureImages.UploadTemporary":
			f.uploads++
			if req.Params["ИдО"] != float64(1101) {
				t.Error("wrong upload product")
			}
			reply(native([]string{"UUID", "UrlRelative", "Url", "Name"}, []any{"UUID", "Строка", "Строка", "Строка"}, []any{fmt.Sprintf("image-%d", f.uploads), fmt.Sprintf("/image-%d", f.uploads), fmt.Sprintf("https://disk.sbis.ru/image-%d", f.uploads), "Фото.png"}))
		case "Nomenclature.Update":
			f.updates++
			if req.Params["Id"] != float64(1101) {
				t.Error("wrong update product")
			}
			data, _ := req.Params["Data"].(map[string]any)
			schema, _ := data["s"].([]any)
			for _, raw := range schema {
				field, _ := raw.(map[string]any)
				name, _ := field["n"].(string)
				if name == "IsDraft" {
					t.Error("invented finalization mutation")
				}
				if !setSabyRecordField(f.card, name, sabyRecordField(data, name)) {
					t.Errorf("unknown update field %s", name)
				}
			}
			setSabyRecordField(f.card, "IsDraft", false)
			if f.lostUpdate {
				w.WriteHeader(500)
				return
			}
			reply(true)
		default:
			t.Errorf("unsupported RPC: %s", req.Method)
			w.WriteHeader(400)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}
func (f *sabyEditorNativeFixture) publisher() *SabyEditorPublisher {
	client := NewSabyClient("id", "secret", "key", 278, 6)
	client.authURL = f.server.URL + "/auth"
	client.serviceURL = f.server.URL + "/service/"
	return &SabyEditorPublisher{Client: client, ImageClient: &http.Client{Transport: sabyEditorImageRoundTrip{}}}
}
func sabyEditorRequestFixture() ce.Request {
	return ce.Request{Name: "Адениум D13", Description: "Описание растения\nВторая строка", PriceMinor: 149099, SKU: "ficusin-13", Barcode: "2000000000015", Images: []string{"https://images.example.test/a.png", "https://images.example.test/b.png"}, Saby: ce.Saby{Mode: "create", Category: ce.Category{ID: "1041", Name: "Комнатные растения"}, Folder: ce.Category{ID: "3333", Name: "Цветы маркетплейс"}}}
}
func driveSabyEditorFixture(t *testing.T, p *SabyEditorPublisher, r ce.Request, s ce.Progress) ce.Result {
	t.Helper()
	for i := 0; i < 16; i++ {
		if s.Stage == "create" {
			s.CreateAttempted = true
		}
		res, err := p.Advance(context.Background(), "saby", r, s)
		if err != nil {
			t.Fatalf("stage %s: %v", s.Stage, err)
		}
		if res.Status != "queued" {
			return res
		}
		s = res.Progress
	}
	t.Fatal("provider did not finish")
	return ce.Result{}
}
func TestSabyEditorNativeCreateFullFlowPreservesTypedCodesPricePhotos(t *testing.T) {
	f := newSabyEditorNativeFixture(t)
	result := driveSabyEditorFixture(t, f.publisher(), sabyEditorRequestFixture(), ce.Progress{})
	if result.Status != "created" || f.creates != 1 || f.updates != 1 || f.uploads != 2 || result.Progress.IDs.OfferID != "Xfixture" || result.Progress.IDs.ImageCount != 2 {
		t.Fatalf("wrong outcome: %+v create=%d update=%d upload=%d", result, f.creates, f.updates, f.uploads)
	}
}
func TestSabyEditorNativeExistingUpdateNeverCreatesAndReconcilesLostSaveReply(t *testing.T) {
	f := newSabyEditorNativeFixture(t)
	f.lostUpdate = true
	r := sabyEditorRequestFixture()
	r.Action = "update"
	r.SabyID = "1101"
	r.SabyUUID = "0f56c2bd-2669-473f-9b94-875d521998a7"
	result := driveSabyEditorFixture(t, f.publisher(), r, ce.Progress{})
	if result.Status != "synced" || f.creates != 0 || f.updates != 1 || result.Progress.Ambiguous {
		t.Fatalf("lost reply replayed or not reconciled: %+v", result)
	}
}
func TestSabyEditorExistingPreservesNumberAndAllOriginalCodes(t *testing.T) {
	f := newSabyEditorNativeFixture(t)
	r := sabyEditorRequestFixture()
	r.Action = "update"
	r.SabyID = "1101"
	original := sabyRecordField(f.card, "CodeList").(map[string]any)
	originalRows := original["d"].([]any)
	originalRows = append(originalRows, []any{float64(2702), "2000000002064", float64(2), nil, nil, nil, float64(1000)}, []any{float64(2571), "OZN2770570198", nil, nil, nil, nil, float64(2000)})
	original["d"] = originalRows
	snapshot, _ := json.Marshal(originalRows)
	result := driveSabyEditorFixture(t, f.publisher(), r, ce.Progress{})
	if result.Status != "synced" || sabyRecordField(f.card, "Number") != "Xfixture" {
		t.Fatal("existing native Number was overwritten")
	}
	after := sabyRecordField(f.card, "CodeList").(map[string]any)["d"].([]any)
	preserved, _ := json.Marshal(after[:len(originalRows)])
	if string(snapshot) != string(preserved) {
		t.Fatal("an original nominal/article/barcode code was rewritten")
	}
}
func TestSabyEditorNativeUnknownCreateNeverRetries(t *testing.T) {
	f := newSabyEditorNativeFixture(t)
	f.lostCreate = true
	p := f.publisher()
	r := sabyEditorRequestFixture()
	result := driveSabyEditorFixture(t, p, r, ce.Progress{})
	if result.Status != "needs_attention" || f.creates != 1 {
		t.Fatalf("unknown create not stopped: %+v", result)
	}
	again, err := p.Advance(context.Background(), "saby", r, result.Progress)
	if err != nil || again.Status != "needs_attention" || f.creates != 1 {
		t.Fatal("unknown create retried")
	}
}
func TestSabyEditorNativeKnownIdentityMismatchBlocksEveryWrite(t *testing.T) {
	f := newSabyEditorNativeFixture(t)
	r := sabyEditorRequestFixture()
	r.Action = "update"
	r.SabyID = "1101"
	r.SabyUUID = "11111111-1111-1111-1111-111111111111"
	result := driveSabyEditorFixture(t, f.publisher(), r, ce.Progress{})
	if result.Status != "needs_attention" || f.creates+f.updates+f.uploads != 0 {
		t.Fatal("mismatched UUID mutated card")
	}
}

func prepareSabyEditorFixture(t *testing.T, p *SabyEditorPublisher, r ce.Request) ce.Progress {
	t.Helper()
	s := ce.Progress{}
	for i := 0; i < 16; i++ {
		if s.Stage == "create" {
			s.CreateAttempted = true
		}
		result, err := p.Advance(context.Background(), "saby", r, s)
		if err != nil || result.Status != "queued" {
			t.Fatalf("prepare %s: %+v/%v", s.Stage, result, err)
		}
		// This JSON round trip is the exact persisted progress boundary used by the
		// worker. A send may only consume the result from the prior committed call.
		data, _ := json.Marshal(result.Progress)
		if err = json.Unmarshal(data, &s); err != nil {
			t.Fatal(err)
		}
		if s.Stage == "send-save" {
			return s
		}
	}
	t.Fatal("save payload was not durably prepared")
	return ce.Progress{}
}
func TestSabyEditorPreparedSaveSurvivesCrashAfterPOSTBeforeProgressCommit(t *testing.T) {
	f := newSabyEditorNativeFixture(t)
	p := f.publisher()
	r := sabyEditorRequestFixture()
	r.Action = "update"
	r.SabyID = "1101"
	saved := prepareSabyEditorFixture(t, p, r)
	if f.updates != 0 {
		t.Fatal("prepare-save performed a remote write")
	}
	var receipt sabyEditorPublishState
	if json.Unmarshal(saved.Native, &receipt) != nil || len(receipt.ImageIDs) != 2 || len(receipt.PreparedData) == 0 || receipt.PreparedHash == "" {
		t.Fatal("incomplete receipt before POST")
	}
	dbCommitted, _ := json.Marshal(saved)
	// Saby commits the Update, but the process dies before committing ANY of
	// Advance's returned fields. Only the earlier send-save receipt survives.
	sent, err := p.Advance(context.Background(), "saby", r, saved)
	if err != nil || sent.Progress.Stage != "verify" || f.updates != 1 {
		t.Fatalf("send %+v/%v", sent, err)
	}
	var recovered ce.Progress
	json.Unmarshal(dbCommitted, &recovered)
	// publisher_worker expired-processing recovery maps send-save to verify.
	recovered.Stage = "verify"
	recovered.Ambiguous = true
	verified, err := p.Advance(context.Background(), "saby", r, recovered)
	if err != nil || verified.Status != "synced" || f.updates != 1 || verified.Progress.IDs.ImageCount != 2 {
		t.Fatalf("crash replayed or lost expected image IDs: %+v/%v writes=%d", verified, err, f.updates)
	}
}
func TestSabyEditorPreparedSaveIgnoresRotatingURLsButRejectsRealEdits(t *testing.T) {
	for _, realEdit := range []bool{false, true} {
		t.Run(fmt.Sprint(realEdit), func(t *testing.T) {
			f := newSabyEditorNativeFixture(t)
			gallery := sabyRecordField(f.card, "ImageData").(map[string]any)
			gallery["d"] = []any{[]any{"old-id", "/old?token=first", "https://disk.sbis.ru/old?token=first", "old.png", "Фото", nil, true, true, true, "", false, nil, "{}", true}}
			p := f.publisher()
			r := sabyEditorRequestFixture()
			r.Action = "update"
			r.SabyID = "1101"
			saved := prepareSabyEditorFixture(t, p, r)
			rows := gallery["d"].([]any)
			row := rows[0].([]any)
			row[1] = "/old?token=rotated"
			row[2] = "https://disk.sbis.ru/old?token=rotated"
			if realEdit {
				setSabyRecordField(f.card, "Name", "Другой редактор изменил название")
			}
			sent, err := p.Advance(context.Background(), "saby", r, saved)
			if err != nil {
				t.Fatal(err)
			}
			if realEdit {
				if sent.Status != "needs_attention" || f.updates != 0 {
					t.Fatal("concurrent owned edit was overwritten")
				}
			} else if sent.Status != "queued" || sent.Progress.Stage != "verify" || f.updates != 1 {
				t.Fatalf("URL rotation blocked save: %+v", sent)
			}
		})
	}
}
func TestSabyEditorVerifyRejectsLegacyIncompleteReceiptWithoutWriting(t *testing.T) {
	f := newSabyEditorNativeFixture(t)
	r := sabyEditorRequestFixture()
	r.Action = "update"
	r.SabyID = "1101"
	result, err := f.publisher().Advance(context.Background(), "saby", r, ce.Progress{Stage: "verify", IDs: ce.RemoteIDs{ProductID: "1101"}})
	if err != nil || result.Status != "needs_attention" || f.updates+f.uploads+f.creates != 0 {
		t.Fatalf("legacy receipt fabricated verification or wrote: %+v/%v", result, err)
	}
}
