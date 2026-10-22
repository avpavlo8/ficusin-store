package integration

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestFindCitiesSuggestsPartialNames(t *testing.T) {
	client := NewCDEKClient("test", "test")
	client.token, client.tokenExpiry = "test-token", time.Now().Add(time.Hour)
	client.httpClient.Transport = cdekRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Path != "/v2/location/suggest/cities" || request.URL.Query().Get("name") != "Ряз" || request.URL.Query().Get("country_code") != "RU" {
			t.Fatalf("wrong city autocomplete request: %s %s", request.Method, request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`[
			{"code":159,"full_name":"Рязань, Рязанская область, Россия","country_code":"RU"},
			{"code":160,"full_name":"Другой город","country_code":"BY"},
			{"code":0,"full_name":"Без кода","country_code":"RU"},
			{"code":161,"full_name":" ","country_code":"RU"}
		]`))}, nil
	})
	cities, err := client.FindCities(context.Background(), " Ряз ")
	if err != nil {
		t.Fatal(err)
	}
	if len(cities) != 1 || cities[0].Code != 159 || cities[0].City != "Рязань, Рязанская область, Россия" {
		t.Fatalf("unexpected suggestions: %+v", cities)
	}
}

func TestCombineParcelsStandsBoxesSideBySide(t *testing.T) {
	// The case the shop owner described: a pineapple and a monstera travel
	// next to each other, not stacked and not merged.
	pineapple := Parcel{LengthCM: 40, WidthCM: 20, HeightCM: 20, WeightGrams: 1200}
	monstera := Parcel{LengthCM: 60, WidthCM: 20, HeightCM: 20, WeightGrams: 2300}

	box, measured := CombineParcels([]Parcel{pineapple, monstera})

	if !measured {
		t.Fatal("обе коробки заданы, расчёт должен получиться")
	}
	if box.LengthCM != 60 || box.WidthCM != 40 || box.HeightCM != 20 {
		t.Fatalf("expected 60x40x20, got %dx%dx%d", box.LengthCM, box.WidthCM, box.HeightCM)
	}
	if box.WeightGrams != 3500 {
		t.Fatalf("expected 3500 g, got %d", box.WeightGrams)
	}
}

func TestCombineParcelsLaysEachBoxOnItsLongestSide(t *testing.T) {
	// Same box, dimensions entered in a different order. A quote must not
	// depend on which field the manager typed the 60 into.
	upright := Parcel{LengthCM: 20, WidthCM: 60, HeightCM: 20, WeightGrams: 1000}
	flat := Parcel{LengthCM: 20, WidthCM: 20, HeightCM: 60, WeightGrams: 1000}

	first, _ := CombineParcels([]Parcel{upright})
	second, _ := CombineParcels([]Parcel{flat})

	if first != second {
		t.Fatalf("orientation changed the box: %+v vs %+v", first, second)
	}
	if first.LengthCM != 60 || first.WidthCM != 20 || first.HeightCM != 20 {
		t.Fatalf("expected 60x20x20, got %dx%dx%d", first.LengthCM, first.WidthCM, first.HeightCM)
	}
}

// A guessed box turns into a real number on the checkout page. Better to say
// the manager will work the price out than to quote something the shop would
// have to argue about later.
func TestCombineParcelsRefusesToGuessAnUnmeasuredItem(t *testing.T) {
	measured := Parcel{LengthCM: 50, WidthCM: 30, HeightCM: 30, WeightGrams: 2000}

	for name, parcels := range map[string][]Parcel{
		"пустая корзина":             {},
		"ничего не заполнено":        {{}, {}},
		"одна позиция без габаритов": {measured, {}},
		"забыли вес":                 {{LengthCM: 40, WidthCM: 20, HeightCM: 20}},
	} {
		if _, ok := CombineParcels(parcels); ok {
			t.Fatalf("%s: расчёт не должен получаться", name)
		}
	}
}
