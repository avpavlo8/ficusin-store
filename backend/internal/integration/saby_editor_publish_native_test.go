package integration

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestSabyEditorNativeFieldsKeepExactVendorSchema(t *testing.T) {
	raw := []byte(`{"_type":"record","s":[{"n":"Id","t":"Число целое"},{"n":"PriceData","t":"Выборка","extra":{"keep":true}},{"n":"Folder","t":"Число целое"}],"d":[1101,{"_type":"recordset","s":[{"n":"Price","t":{"n":"Деньги","p":2}}],"d":[[4300.25]]},3333]}`)
	r, err := parseSabyEditorNativeRecord(raw)
	if err != nil {
		t.Fatal(err)
	}
	if id, err := r.scalarString("Id"); err != nil || id != "1101" {
		t.Fatalf("identity %s/%v", id, err)
	}
	filtered, err := r.selectFields(map[string]json.RawMessage{"PriceData": r.value("PriceData")})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	json.Unmarshal(filtered, &got)
	var fields []json.RawMessage
	json.Unmarshal(got["s"], &fields)
	if len(fields) != 1 || !bytes.Equal(fields[0], r.schema[1]) {
		t.Fatalf("native schema changed: %s", filtered)
	}
	var data []json.RawMessage
	json.Unmarshal(got["d"], &data)
	if len(data) != 1 || !bytes.Equal(data[0], r.value("PriceData")) {
		t.Fatalf("native nested values changed: %s", filtered)
	}
	if _, err = r.selectFields(map[string]json.RawMessage{"GuessedField": json.RawMessage(`1`)}); err == nil {
		t.Fatal("guessed schema accepted")
	}
}
func TestSabyEditorNativeRecordRejectsAmbiguity(t *testing.T) {
	for _, raw := range []string{`{}`, `{"_type":"recordset","s":[],"d":[]}`, `{"_type":"record","s":[{"n":"Id"}],"d":[]}`, `{"_type":"record","s":[{"n":"Id"},{"n":"Id"}],"d":[1,2]}`} {
		if _, err := parseSabyEditorNativeRecord([]byte(raw)); err == nil {
			t.Fatalf("malformed native record accepted: %s", raw)
		}
	}
}
