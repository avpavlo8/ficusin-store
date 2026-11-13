package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	"github.com/jackc/pgx/v5"
)

func TestSabyEditorPreparedReceiptPostgresJSONBRoundTrip(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	roundTrip := func(value any, target any) {
		t.Helper()
		raw, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		var stored []byte
		// The real PostgreSQL JSONB codec normalizes the nested RawMessages
		// stored by publisher_worker; encoding/json alone cannot model it.
		if e = conn.QueryRow(ctx, `SELECT $1::jsonb`, raw).Scan(&stored); e != nil {
			t.Fatal(e)
		}
		if bytes.Equal(raw, stored) {
			t.Fatal("fixture did not exercise JSONB normalization")
		}
		decoder := json.NewDecoder(bytes.NewReader(stored))
		decoder.UseNumber()
		if e = decoder.Decode(target); e != nil {
			t.Fatal(e)
		}
	}
	f := newSabyEditorNativeFixture(t)
	p := f.publisher()
	request := sabyEditorRequestFixture()
	request.Action, request.SabyID = "update", "1101"
	request.SiteAttributes = map[string]any{"large": json.Number("9007199254740993"), "exact": json.Number("0.123456789012345678901")}
	roundTrip(request, &request)
	saved := prepareSabyEditorFixture(t, p, request)
	var restored ce.Progress
	roundTrip(saved, &restored)
	var receipt sabyEditorPublishState
	if err = json.Unmarshal(restored.Native, &receipt); err != nil {
		t.Fatal(err)
	}
	if !sabyEditorPreparedValid(receipt, request, restored.IDs.ProductID) {
		t.Fatal("valid prepared receipt was rejected after real PostgreSQL JSONB normalization")
	}
	sent, err := p.Advance(ctx, "saby", request, restored)
	if err != nil || sent.Progress.Stage != "verify" || f.updates != 1 {
		t.Fatalf("send: %+v/%v updates=%d", sent, err, f.updates)
	}
	roundTrip(sent.Progress, &restored)
	verified, err := p.Advance(ctx, "saby", request, restored)
	if err != nil || verified.Status != "synced" || f.updates != 1 {
		t.Fatalf("verify: %+v/%v updates=%d", verified, err, f.updates)
	}
	if f.creates != 0 {
		t.Fatal("receipt recovery created another Saby card")
	}
	for _, change := range []string{"native", "request", "legacy"} {
		t.Run(change, func(t *testing.T) {
			var changed ce.Progress
			roundTrip(saved, &changed)
			var state sabyEditorPublishState
			if e := json.Unmarshal(changed.Native, &state); e != nil {
				t.Fatal(e)
			}
			alteredRequest := request
			switch change {
			case "native":
				state.PreparedData = bytes.Replace(state.PreparedData, []byte(`"Name"`), []byte(`"OtherName"`), 1)
			case "request":
				alteredRequest.PriceMinor++
			case "legacy":
				sum := sha256.Sum256(state.PreparedData)
				state.PreparedHash = hex.EncodeToString(sum[:])
			}
			changed.Native, _ = json.Marshal(state)
			roundTrip(changed, &changed)
			before := f.updates
			beforeReads := f.reads
			rejected, e := p.Advance(ctx, "saby", alteredRequest, changed)
			if e != nil || rejected.Status != "needs_attention" || f.updates != before || f.reads != beforeReads {
				t.Fatalf("changed receipt reached vendor: %+v/%v", rejected, e)
			}
		})
	}
}

func TestSabyEditorPayloadHashPreservesExactJSONSemantics(t *testing.T) {
	original := []byte(`{"schema":[{"n":"Деньги","t":{"p":2,"n":"Число"}}],"values":[9007199254740993,0.123456789012345678901,1e-30,-0.00],"nested":{"b":2,"a":1}}`)
	normalized := []byte(` { "nested": {"a":1.0,"b":2e0}, "values": [9007199254740993.00,123456789012345678901e-21,0.000000000000000000000000000001,0],"schema":[{"t":{"n":"Число","p":2.0},"n":"Деньги"}]} `)
	hash := sabyEditorPayloadHash(original)
	if hash == "" || hash != sabyEditorPayloadHash(normalized) {
		t.Fatal("equivalent JSON formatting/numeric representation changed receipt")
	}
	for _, changed := range [][]byte{
		bytes.Replace(original, []byte("9007199254740993"), []byte("9007199254740992"), 1),
		bytes.Replace(original, []byte("0.123456789012345678901"), []byte("0.123456789012345678902"), 1),
		bytes.Replace(original, []byte(`"Деньги"`), []byte(`"Строка"`), 1),
		bytes.Replace(original, []byte(`"p":2`), []byte(`"p":"2"`), 1),
		bytes.Replace(original, []byte(`1e-30,-0.00`), []byte(`-0.00,1e-30`), 1),
	} {
		if hash == sabyEditorPayloadHash(changed) {
			t.Fatal("meaningfully changed native JSON shared a hash")
		}
	}
	for _, invalid := range [][]byte{nil, []byte(`{"a":`), []byte(`{} {}`)} {
		if sabyEditorPayloadHash(invalid) != "" {
			t.Fatal("invalid JSON produced a usable receipt hash")
		}
	}
}
