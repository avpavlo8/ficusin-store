package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"github.com/avpavlo8/ficusin-store/backend/internal/catalogsync"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf16"
)

func decodeCML(t *testing.T, b []byte) string {
	t.Helper()
	if len(b) < 2 || b[0] != 255 || b[1] != 254 {
		t.Fatal("missing UTF16 BOM")
	}
	u := make([]uint16, (len(b)-2)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[2+i*2:])
	}
	return string(utf16.Decode(u))
}
func catalogueSnapshot() catalogsync.Snapshot {
	return catalogsync.Snapshot{VariantID: 1, SKU: "321", Name: "Калатея & медальон D12", PotCM: 11.3, HeightCM: 40, PriceMinor: 149099, Code: "catalogue-code", Article: "321"}
}
func TestCMLKeepsExactIdentityEncodingAndMoney(t *testing.T) {
	s := catalogueSnapshot()
	b, e := buildSabyCML(s, true)
	if e != nil {
		t.Fatal(e)
	}
	raw := decodeCML(t, b)
	for _, part := range []string{"<?xml version=\"1.0\" encoding=\"UTF-16\"?>\r\n", "СодержитТолькоИзменения=\"true\"", "Калатея &amp; медальон D12", "<Ид>catalogue-code</Ид>", "<Значение>11.3</Значение>", "<ЦенаЗаЕдиницу>1490.99</ЦенаЗаЕдиницу>"} {
		if !strings.Contains(raw, part) {
			t.Fatalf("missing %s in %s", part, raw)
		}
	}
	if strings.Contains(raw, "Количество") || strings.Contains(raw, "СтавкиНалогов") {
		t.Fatal("stock/tax included")
	}
	d := xml.NewDecoder(strings.NewReader(raw))
	d.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	var root cmlNode
	if e = d.Decode(&root); e != nil {
		t.Fatal(e)
	}
	b, e = buildSabyCML(s, false)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(decodeCML(t, b), "ПакетПредложений") {
		t.Fatal("unchanged price included")
	}
}
func TestSabyCatalogueNullAcceptanceAndDefiniteRejection(t *testing.T) {
	for _, body := range []string{`{"result":null}`, `{"error":{"message":"denied"}}`, `{}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/auth" {
				_, _ = io.WriteString(w, `{"token":"test-token"}`)
				return
			}
			if r.Header.Get("X-SBISAccessToken") != "test-token" {
				t.Error("no token")
			}
			var p struct {
				Protocol int    `json:"protocol"`
				Method   string `json:"method"`
				Params   struct {
					Parameter struct {
						File string `json:"Файл"`
					} `json:"Параметр"`
				} `json:"params"`
			}
			if e := json.NewDecoder(r.Body).Decode(&p); e != nil {
				t.Error(e)
			}
			if p.Protocol != 3 || p.Method != "СБИС.ИмпортНоменклатурыИзCML" {
				t.Error("wrong method")
			}
			b, e := base64.StdEncoding.DecodeString(p.Params.Parameter.File)
			if e != nil || !bytes.HasPrefix(b, []byte{255, 254}) {
				t.Error("bad file")
			}
			_, _ = io.WriteString(w, body)
		}))
		c := NewSabyClient("id", "secret", "key", 278, 6)
		c.authURL = server.URL + "/auth"
		c.serviceURL = server.URL + "/service/"
		e := c.Write(context.Background(), catalogueSnapshot(), true)
		if strings.Contains(body, "null") {
			if e != nil {
				t.Fatal(e)
			}
		} else if e == nil {
			t.Fatal("bad response accepted")
		}
		if strings.Contains(body, "denied") {
			var rejected *catalogsync.RejectedError
			if !errors.As(e, &rejected) {
				t.Fatal("rejection not classified")
			}
		}
		server.Close()
	}
}
func TestCatalogueLookupUsesFreshProjectionAndRejectsNameOnly(t *testing.T) {
	s := catalogueSnapshot()
	s.Name = "Калатея D12"
	s.LookupName = "Калатея"
	s.SabyID = "71"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth" {
			_, _ = io.WriteString(w, `{"token":"token"}`)
			return
		}
		height := 30
		if r.URL.Query().Get("searchString") == "Калатея" {
			height = 40
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"nomenclatures": []any{map[string]any{"id": 71, "nomNumber": "catalogue-code", "name": s.Name, "cost": 1490.99, "attributes": map[string]any{"Диаметр горшка": "11.3", "Высота": height}}}, "outcome": false})
	}))
	defer server.Close()
	c := NewSabyClient("id", "secret", "key", 278, 6)
	c.authURL = server.URL + "/auth"
	c.apiBase = server.URL
	r, e := c.Lookup(context.Background(), s)
	if e != nil || !s.Matches(r) {
		t.Fatalf("fresh result missing: %+v %v", r, e)
	}
	s.Code = "different-code"
	s.SabyID = ""
	_, e = c.Lookup(context.Background(), s)
	if e == nil {
		t.Fatal("name-only match adopted")
	}
}
