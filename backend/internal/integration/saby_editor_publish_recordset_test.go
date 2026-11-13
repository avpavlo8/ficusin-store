package integration

import (
	"encoding/json"
	"testing"
)

func TestSabyEditorEANPreservesNominalNumberAndUsesNegativeTemporaryID(t *testing.T) {
	raw := json.RawMessage(`{"_type":"recordset","s":[{"n":"Id","t":"Число целое"},{"n":"Code","t":"Строка"},{"n":"Type","t":"Число целое"},{"n":"Order","t":"Число целое"}],"d":[[-1,"X269652448",-1,0],[-101,"other",9,1]]}`)
	encoded, err := sabyEditorAddEAN13(raw, "2000000000015")
	if err != nil {
		t.Fatal(err)
	}
	set, err := parseSabyEditorNativeRecordSet(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.rows) != 3 || string(set.cell(0, "Code")) != `"X269652448"` || string(set.cell(2, "Id")) != "-102" || string(set.cell(2, "Type")) != "2" {
		t.Fatalf("wrong barcode transform: %s", encoded)
	}
	again, err := sabyEditorAddEAN13(encoded, "2000000000015")
	if err != nil || string(again) != string(encoded) {
		t.Fatalf("repeated barcode changed result: %s/%v", again, err)
	}
	if _, err = sabyEditorAddEAN13(encoded, "2000000000016"); err == nil {
		t.Fatal("bad EAN checksum accepted")
	}
}
func TestSabyEditorPriceOnlyPatchesConfiguredEditableListExactMoney(t *testing.T) {
	raw := json.RawMessage(`{"_type":"recordset","s":[{"n":"Id","t":"Строка"},{"n":"PriceList","t":"Число целое"},{"n":"Price","t":{"n":"Деньги","p":2}},{"n":"CanEdit","t":"Логическое"},{"n":"MeasureUnit","t":"Строка"},{"n":"IsUpdated","t":"Логическое"}],"d":[["a",6,4300,true,"шт",false],["b",7,999,false,"уп",false]]}`)
	encoded, err := sabyEditorSetPrice(raw, 6, 430025)
	if err != nil {
		t.Fatal(err)
	}
	set, _ := parseSabyEditorNativeRecordSet(encoded)
	if string(set.cell(0, "Price")) != "4300.25" || string(set.cell(0, "IsUpdated")) != "true" || string(set.cell(1, "Price")) != "999" || string(set.cell(1, "MeasureUnit")) != `"уп"` {
		t.Fatalf("wrong price transform: %s", encoded)
	}
	if _, err = sabyEditorSetPrice(raw, 7, 100); err == nil {
		t.Fatal("read-only list changed")
	}
	if _, err = sabyEditorSetPrice(raw, 8, 100); err == nil {
		t.Fatal("missing list invented")
	}
}

func TestSabyEditorImagesPreserveUneditableMarketplaceRows(t *testing.T) {
	raw := json.RawMessage(`{"_type":"recordset","s":[{"n":"id","t":"Строка"},{"n":"url","t":"Строка"},{"n":"fullUrl","t":"Строка"},{"n":"isMaster","t":"Логическое"},{"n":"canDelete","t":"Логическое"},{"n":"canEdit","t":"Логическое"},{"n":"isMarketImage","t":"Логическое"}],"d":[["market","/market","https://disk.sbis.ru/market",false,true,true,true],["locked","/locked","https://disk.sbis.ru/locked",false,false,false,false],["editable","/old","https://disk.sbis.ru/old",true,true,true,false]]}`)
	upload := sabyEditorUploadedImage{Source: "https://images.example.test/new.png", Result: json.RawMessage(`{"_type":"record","s":[{"n":"UUID","t":"UUID"},{"n":"Url","t":"Строка"}],"d":["our-new","https://disk.sbis.ru/new"]}`)}
	encoded, ids, err := sabyEditorImageData(raw, []sabyEditorUploadedImage{upload})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 || ids[0] != "our-new" || ids[1] != "market" || ids[2] != "locked" {
		t.Fatalf("protected photos lost: %s", encoded)
	}
	before, _ := parseSabyEditorNativeRecordSet(raw)
	after, _ := parseSabyEditorNativeRecordSet(encoded)
	a, _ := json.Marshal(before.rows[0])
	b, _ := json.Marshal(after.rows[1])
	if string(a) != string(b) {
		t.Fatal("marketplace-owned photo changed")
	}
	a, _ = json.Marshal(before.rows[1])
	b, _ = json.Marshal(after.rows[2])
	if string(a) != string(b) {
		t.Fatal("uneditable photo changed")
	}
	before.set(0, "isMaster", true)
	lockedCover, _ := before.marshal()
	if _, _, err = sabyEditorImageData(lockedCover, []sabyEditorUploadedImage{upload}); err == nil {
		t.Fatal("protected cover silently replaced")
	}
}
