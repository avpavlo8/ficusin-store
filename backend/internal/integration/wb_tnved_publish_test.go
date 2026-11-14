package integration

import "testing"

func TestWBPublicationSendsOnlyTNVEDCodeFromSharedDictionary(t *testing.T) {
	r := cardFixture("wb")
	r.Marketplace.TNVED = "0601109000 - Прочие цветы: луковицы, клубни"
	r.Marketplace.TNVEDID = 971396782
	body, err := BuildWBCard(r, false)
	if err != nil {
		t.Fatal(err)
	}
	variant := body[0]["variants"].([]any)[0].(map[string]any)
	for _, a := range variant["characteristics"].([]map[string]any) {
		if a["id"] == int64(15000003) {
			value := a["value"].([]string)
			if len(value) != 1 || value[0] != "0601109000" {
				t.Fatalf("WB received Ozon's display label: %+v", a)
			}
			return
		}
	}
	t.Fatal("TNVED was dropped from the publication")
}

func TestWBPublicationRejectsInvalidTNVEDBeforeSending(t *testing.T) {
	r := cardFixture("wb")
	r.Marketplace.TNVED = "06011090001"
	if _, err := BuildWBCard(r, false); err == nil {
		t.Fatal("invalid WB code was accepted for publication")
	}
}
