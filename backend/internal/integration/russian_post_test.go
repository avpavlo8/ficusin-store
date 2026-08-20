package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRussianPostCalculateUsesContractRateAndDimensions(t *testing.T) {
	var tariffBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "AccessToken app-token" {
			t.Fatalf("нет application token: %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("X-User-Authorization") != "Basic user-key" {
			t.Fatalf("нет user auth key: %q", request.Header.Get("X-User-Authorization"))
		}
		switch request.URL.Path {
		case "/1.0/clean/address":
			_ = json.NewEncoder(response).Encode([]map[string]any{{
				"index": "101000", "quality-code": "GOOD", "raw-address": "Москва",
			}})
		case "/1.0/tariff":
			if err := json.NewDecoder(request.Body).Decode(&tariffBody); err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(response).Encode(map[string]any{
				"total-rate": 50000,
				"total-vat":  10000,
				"delivery-time": map[string]int{"min-days": 2, "max-days": 4},
			})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	client := NewRussianPostClient("app-token", "user-key", "390000")
	client.baseURL = server.URL
	client.httpClient = server.Client()
	quote, err := client.Calculate(context.Background(), "Москва, Мясницкая, 1", Parcel{
		LengthCM: 30, WidthCM: 20, HeightCM: 40, WeightGrams: 1500,
	})
	if err != nil {
		t.Fatal(err)
	}
	if quote.Price != 600 || quote.DaysMin != 2 || quote.DaysMax != 4 {
		t.Fatalf("неверный тариф: %+v", quote)
	}
	if tariffBody["index-from"] != "390000" || tariffBody["index-to"] != "101000" {
		t.Fatalf("индексы потерялись: %#v", tariffBody)
	}
	dimension, ok := tariffBody["dimension"].(map[string]any)
	if !ok {
		t.Fatalf("нет габаритов: %#v", tariffBody["dimension"])
	}
	if dimension["length"] != float64(300) || dimension["width"] != float64(200) || dimension["height"] != float64(400) {
		t.Fatalf("габариты должны уйти в миллиметрах: %#v", dimension)
	}
}

func TestRussianPostRejectsUnrecognizedAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(response).Encode([]map[string]any{{
			"index": "", "quality-code": "UNDEF_05",
		}})
	}))
	defer server.Close()
	client := NewRussianPostClient("token", "key", "390000")
	client.baseURL = server.URL
	client.httpClient = server.Client()
	_, err := client.Calculate(context.Background(), "непонятный адрес", Parcel{
		LengthCM: 10, WidthCM: 10, HeightCM: 10, WeightGrams: 100,
	})
	if err != ErrRussianPostAddress {
		t.Fatalf("ожидали ошибку адреса, получили %v", err)
	}
}

func TestRussianPostNeedsRealCredentials(t *testing.T) {
	if NewRussianPostClient("", "", "").Configured() {
		t.Fatal("пустые ключи включили Почту России")
	}
	if !NewRussianPostClient("token", "key", "390000").Configured() {
		t.Fatal("полный набор ключей не включил Почту России")
	}
}
