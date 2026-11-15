package integration

import (
	"context"
	"encoding/json"
	"testing"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	"github.com/avpavlo8/ficusin-store/backend/internal/money"
)

func fixtureBasePrice(t *testing.T, f *sabyEditorNativeFixture) int64 {
	t.Helper()
	raw, _ := json.Marshal(f.basePrices)
	set, err := parseSabyEditorNativeRecordSet(raw)
	if err != nil {
		t.Fatal(err)
	}
	card := &sabyEditorNativeRecord{fields: map[string]int{"PriceData": 0}, values: []json.RawMessage{raw}}
	index, err := sabyEditorBasePriceIndex(set)
	if err != nil {
		t.Fatal(err)
	}
	var number json.Number
	if json.Unmarshal(set.cell(index, "Price"), &number) != nil {
		t.Fatal("invalid base money")
	}
	// Exercise the same independent main-price matcher used at completion.
	minor, err := money.Parse(number.String())
	if err != nil {
		t.Fatal(err)
	}
	matched, err := sabyEditorBasePriceMatches(card, minor)
	if err != nil || !matched {
		t.Fatal("base readback did not match", err)
	}
	return minor
}

func TestSabyEditorCreationPublishesBothMainAndConfiguredPrice(t *testing.T) {
	f := newSabyEditorNativeFixture(t)
	result := driveSabyEditorFixture(t, f.publisher(), sabyEditorRequestFixture(), ce.Progress{})
	if result.Status != "created" || fixtureBasePrice(t, f) != 149099 {
		t.Fatal("main price not created", result)
	}
	var native sabyEditorPublishState
	if json.Unmarshal(result.Progress.Native, &native) != nil || !native.BasePriceIncluded {
		t.Fatal("dual-price obligation not persisted")
	}
	raw, _ := json.Marshal(f.card)
	card, err := parseSabyEditorNativeRecord(raw)
	if err != nil {
		t.Fatal(err)
	}
	matched, err := sabyEditorPriceMatches(card, 6, 149099)
	if err != nil || !matched {
		t.Fatal("configured price not retained", err)
	}
}

func TestSabyEditorCannotCompleteIfEitherPriceWasIgnored(t *testing.T) {
	for _, scope := range []string{"main", "configured"} {
		t.Run(scope, func(t *testing.T) {
			f := newSabyEditorNativeFixture(t)
			f.ignoreBasePrice = scope == "main"
			f.ignoreConfiguredPrice = scope == "configured"
			result := driveSabyEditorFixture(t, f.publisher(), sabyEditorRequestFixture(), ce.Progress{})
			if result.Status != "needs_attention" || f.updates != 1 || f.creates != 1 {
				t.Fatal("unconfirmed price reported created", result)
			}
		})
	}
}

func TestSabyPriceOnlyRequiresMainAndConfiguredReadback(t *testing.T) {
	f := newSabyEditorNativeFixture(t)
	f.ignoreBasePrice = true
	publisher := &EditorPricePublisher{Saby: f.publisher().Client}
	request := ce.Request{Action: "price", SabyID: "1101", Price: &ce.PriceRequest{TargetID: "Xfixture", PriceMinor: 149099}}
	progress := ce.Progress{}
	for i := 0; i < 3; i++ {
		result, err := publisher.Advance(context.Background(), "saby", request, progress)
		if err != nil || result.Status != "queued" {
			t.Fatal("price preparation failed", result, err)
		}
		progress = result.Progress
	}
	if f.updates != 1 || progress.Stage != "price-verify" {
		t.Fatal("price fixture did not send once")
	}
	progress.Checks = 89
	result, err := publisher.Advance(context.Background(), "saby", request, progress)
	if err != nil || result.Status != "needs_attention" || f.updates != 1 {
		t.Fatal("configured list alone reported synced or resent", result, err)
	}
}

func TestSabyPriceOnlyOldPreparedRequestCannotSkipMainPrice(t *testing.T) {
	f := newSabyEditorNativeFixture(t)
	publisher := &EditorPricePublisher{Saby: f.publisher().Client}
	request := ce.Request{Action: "price", SabyID: "1101", Price: &ce.PriceRequest{TargetID: "Xfixture", PriceMinor: 149099}}
	result, err := publisher.Advance(context.Background(), "saby", request, ce.Progress{Stage: "price-native-send", Native: json.RawMessage(`{"preparedData":{}}`)})
	if err != nil || result.Status != "needs_attention" || f.updates != 0 {
		t.Fatal("old incomplete receipt could send a partial price update", result, err)
	}
}

func TestSabyPriceOnlyRefusesChangedBusinessCode(t *testing.T) {
	f := newSabyEditorNativeFixture(t)
	publisher := &EditorPricePublisher{Saby: f.publisher().Client}
	request := ce.Request{Action: "price", SabyID: "1101", Price: &ce.PriceRequest{TargetID: "Xwrong-card", PriceMinor: 149099}}
	result, err := publisher.Advance(context.Background(), "saby", request, ce.Progress{Stage: "price-send"})
	if err != nil || result.Status != "needs_attention" || f.updates != 0 {
		t.Fatal("changed business code permitted price write", result, err)
	}
}

func TestSabyPricePairPreservesTypedOtherListsAndRefusesAmbiguousMain(t *testing.T) {
	configured := json.RawMessage(`{"_type":"recordset","s":[{"n":"Id","t":"Строка"},{"n":"PriceList","t":"Число целое"},{"n":"Price","t":{"n":"Деньги","p":2}},{"n":"CanEdit","t":"Логическое"},{"n":"MeasureUnit","t":"Строка"},{"n":"IsUpdated","t":"Логическое"}],"d":[["6_base",6,400,true,"шт",false],["7_base",7,999,false,"уп",false]]}`)
	base := json.RawMessage(`{"_type":"recordset","s":[{"n":"Id","t":"Строка"},{"n":"PriceList","t":"Число целое"},{"n":"Price","t":{"n":"Деньги","p":2}},{"n":"CanEdit","t":"Логическое"},{"n":"MeasureUnit","t":"Строка"},{"n":"IsUpdated","t":"Логическое"}],"d":[["None_base",null,null,true,"шт",false]]}`)
	merged, err := sabyEditorMergeBasePrice(configured, base)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := sabyEditorSetPricePair(merged, 6, 149099)
	if err != nil {
		t.Fatal(err)
	}
	set, err := parseSabyEditorNativeRecordSet(updated)
	if err != nil {
		t.Fatal(err)
	}
	if string(set.cell(0, "Price")) != "1490.99" || string(set.cell(2, "Price")) != "1490.99" || string(set.cell(1, "Price")) != "999" || string(set.cell(1, "MeasureUnit")) != `"уп"` || string(set.cell(1, "IsUpdated")) != "false" || string(set.cell(2, "PriceList")) != "null" || string(set.cell(2, "Id")) != `"None_base"` {
		t.Fatal("price pair damaged native money, identity or an unmanaged price", string(updated))
	}
	for _, scenario := range []string{"missing main", "duplicate main", "main readonly", "different main unit", "different schema"} {
		t.Run(scenario, func(t *testing.T) {
			raw := append(json.RawMessage(nil), base...)
			parsed, err := parseSabyEditorNativeRecordSet(raw)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "missing main":
				parsed.rows = nil
			case "duplicate main":
				parsed.rows = append(parsed.rows, parsed.rows[0])
			case "main readonly":
				parsed.set(0, "CanEdit", false)
			case "different main unit":
				parsed.set(0, "MeasureUnit", "уп")
			case "different schema":
				parsed.raw["s"] = json.RawMessage(`[{"n":"Id","t":"Строка"}]`)
			}
			raw, _ = parsed.marshal()
			combined, err := sabyEditorMergeBasePrice(configured, raw)
			if err == nil {
				_, err = sabyEditorSetPricePair(combined, 6, 149099)
			}
			if err == nil {
				t.Fatal("unsafe main projection permitted price update")
			}
		})
	}
}

func TestSabyCreationReceiptCannotDropMainPriceObligation(t *testing.T) {
	f := newSabyEditorNativeFixture(t)
	publisher := f.publisher()
	request := sabyEditorRequestFixture()
	prepared := prepareSabyEditorFixture(t, publisher, request)
	var native sabyEditorPublishState
	if json.Unmarshal(prepared.Native, &native) != nil || !native.BasePriceIncluded {
		t.Fatal("missing fixture price proof")
	}
	native.BasePriceIncluded = false
	prepared.Native, _ = json.Marshal(native)
	result, err := publisher.Advance(context.Background(), "saby", request, prepared)
	if err != nil || result.Status != "needs_attention" || f.updates != 0 {
		t.Fatal("receipt could downgrade main-price verification", result, err)
	}
}

func TestSabyLegacyCreateNeedsMainPriceProofWithoutReplayingFrozenUpdate(t *testing.T) {
	for _, drift := range []string{"none", "content", "identity"} {
		t.Run(drift, func(t *testing.T) {
			f := newSabyEditorNativeFixture(t)
			publisher := f.publisher()
			request := sabyEditorRequestFixture()
			progress := prepareSabyEditorFixture(t, publisher, request)
			var native sabyEditorPublishState
			if err := json.Unmarshal(progress.Native, &native); err != nil {
				t.Fatal(err)
			}
			// Model a durable pre-release receipt: the exact old frozen Update had
			// only configured list6. Keep its IDs, photographs and request checksum.
			data, err := parseSabyEditorNativeRecord(native.PreparedData)
			if err != nil {
				t.Fatal(err)
			}
			prices, err := parseSabyEditorNativeRecordSet(data.value("PriceData"))
			if err != nil {
				t.Fatal(err)
			}
			prices.rows = prices.rows[:1]
			configured, _ := prices.marshal()
			data.values[data.fields["PriceData"]] = configured
			values := map[string]json.RawMessage{}
			for field := range data.fields {
				values[field] = data.value(field)
			}
			native.PreparedData, err = data.selectFields(values)
			if err != nil {
				t.Fatal(err)
			}
			native.PreparedHash = sabyEditorPayloadHash(native.PreparedData)
			cardRaw, _ := json.Marshal(f.card)
			card, err := parseSabyEditorNativeRecord(cardRaw)
			if err != nil {
				t.Fatal(err)
			}
			native.PreparedBaseHash, err = sabyEditorBaselineHash(card, native.PreparedData)
			if err != nil {
				t.Fatal(err)
			}
			native.BasePriceIncluded = false
			progress.Native, _ = json.Marshal(native)
			frozen := append(json.RawMessage(nil), progress.Native...)
			sent, err := publisher.Advance(context.Background(), "saby", request, progress)
			if err != nil || sent.Status != "queued" || sent.Progress.Stage != "verify" || f.updates != 1 {
				t.Fatal("legacy send did not use its frozen payload once", sent, err)
			}
			missing, err := publisher.Advance(context.Background(), "saby", request, sent.Progress)
			if err != nil || missing.Status != "needs_attention" || missing.Progress.Stage != "verify-main-price" || f.updates != 1 {
				t.Fatal("legacy main price was silently accepted", missing, err)
			}
			if string(missing.Progress.Native) != string(frozen) {
				t.Fatal("legacy receipt was rewritten instead of inspected")
			}
			// This is a separately approved price adapter, not a replay of Create
			// or of the old full-content Update. Real queue admission is tested in PG.
			price := &EditorPricePublisher{Saby: publisher.Client}
			priceRequest := ce.Request{Action: "price", SabyID: "1101", Price: &ce.PriceRequest{TargetID: "Xfixture", PriceMinor: request.PriceMinor}}
			priceProgress := ce.Progress{}
			status := ""
			for i := 0; i < 5; i++ {
				result, err := price.Advance(context.Background(), "saby", priceRequest, priceProgress)
				if err != nil {
					t.Fatal(err)
				}
				priceProgress, status = result.Progress, result.Status
				if status == "synced" {
					break
				}
			}
			if status != "synced" || f.updates != 2 || f.creates != 1 {
				t.Fatal("approved price repair did not finish exactly once", status, f.updates, f.creates)
			}
			if drift == "content" {
				setSabyRecordField(f.card, "Name", "External content change")
			}
			if drift == "identity" {
				setSabyRecordField(f.card, "Identifier", "00000000-0000-4000-8000-000000000099")
			}
			verified, err := publisher.Advance(context.Background(), "saby", request, missing.Progress)
			if err != nil || f.updates != 2 || f.creates != 1 {
				t.Fatal("card retry replayed a write", verified, err)
			}
			if drift != "none" {
				if verified.Status != "needs_attention" || verified.Progress.Stage != "verify" {
					t.Fatal("changed content retained price-only exception", verified)
				}
			} else if verified.Status != "created" || verified.Progress.Stage != "done" {
				t.Fatal("main price proof did not release original creation", verified)
			}
		})
	}
}
