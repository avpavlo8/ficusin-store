package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestYandexDeliveryChoosesCheapestFinalPrice(t *testing.T) {
	var deliveryBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/geocode" {
			_ = json.NewEncoder(response).Encode(map[string]any{
				"response": map[string]any{"GeoObjectCollection": map[string]any{"featureMember": []any{
					map[string]any{"GeoObject": map[string]any{
						"metaDataProperty": map[string]any{"GeocoderMetaData": map[string]any{
							"text": "Россия, Рязань, улица Ленина, 1",
							"Address": map[string]any{
								"formatted": "Россия, Рязань, улица Ленина, 1",
								"Components": []any{map[string]any{"kind": "locality", "name": "Рязань"}},
							},
						}},
						"Point": map[string]any{"pos": "39.740000 54.630000"},
					}},
				}}},
			})
			return
		}
		if request.URL.Path != "/b2b/cargo/integration/v2/offers/calculate" {
			http.NotFound(response, request)
			return
		}
		if request.Header.Get("Authorization") != "Bearer yandex-token" {
			t.Fatalf("нет bearer token: %q", request.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(request.Body).Decode(&deliveryBody); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"offers": []any{
			map[string]any{"taxi_class": "express", "price": map[string]any{"total_price": "520.00", "total_price_with_vat": "624.00", "currency": "RUB"}},
			map[string]any{"taxi_class": "courier", "price": map[string]any{"total_price": "400.00", "total_price_with_vat": "480.00", "currency": "RUB"}},
		}})
	}))
	defer server.Close()

	client := NewYandexDeliveryClient("yandex-token", "geo-key", "Рязань, Новосёлов, 40А", 39.80, 54.62)
	client.httpClient = server.Client()
	client.deliveryBaseURL = server.URL
	client.geocoderBaseURL = server.URL + "/geocode"
	quote, err := client.Calculate(context.Background(), "Рязань, улица Ленина, 1", Parcel{
		LengthCM: 30, WidthCM: 20, HeightCM: 40, WeightGrams: 1500,
	})
	if err != nil {
		t.Fatal(err)
	}
	if quote.Price != 480 || !strings.Contains(quote.Service, "Курьер") {
		t.Fatalf("неверный оффер: %+v", quote)
	}
	items, ok := deliveryBody["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("товар не доехал до Яндекса: %#v", deliveryBody["items"])
	}
	item := items[0].(map[string]any)
	if item["weight"] != 1.5 {
		t.Fatalf("вес должен быть в килограммах: %#v", item["weight"])
	}
	size := item["size"].(map[string]any)
	if size["length"] != 0.3 || size["width"] != 0.2 || size["height"] != 0.4 {
		t.Fatalf("размеры должны быть в метрах: %#v", size)
	}
}

func TestYandexDeliveryRefusesAnotherCity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(response).Encode(map[string]any{
			"response": map[string]any{"GeoObjectCollection": map[string]any{"featureMember": []any{
				map[string]any{"GeoObject": map[string]any{
					"metaDataProperty": map[string]any{"GeocoderMetaData": map[string]any{"Address": map[string]any{
						"formatted": "Россия, Москва, Тверская, 1",
						"Components": []any{map[string]any{"kind": "locality", "name": "Москва"}},
					}}},
					"Point": map[string]any{"pos": "37.61 55.75"},
				}},
			}}},
		})
	}))
	defer server.Close()
	client := NewYandexDeliveryClient("token", "geo", "Рязань, Новосёлов, 40А", 39.8, 54.6)
	client.httpClient = server.Client()
	client.geocoderBaseURL = server.URL
	client.deliveryBaseURL = server.URL
	_, err := client.Calculate(context.Background(), "Москва, Тверская, 1", Parcel{
		LengthCM: 10, WidthCM: 10, HeightCM: 10, WeightGrams: 100,
	})
	if err != ErrYandexOutsideRyazan {
		t.Fatalf("Москва не должна считаться городским курьером: %v", err)
	}
}

func TestYandexDeliveryNeedsCompleteConfiguration(t *testing.T) {
	if NewYandexDeliveryClient("", "", "", 0, 0).Configured() {
		t.Fatal("пустая конфигурация включила курьера")
	}
	if !NewYandexDeliveryClient("token", "geo", "Рязань, Новосёлов, 40А", 39.8, 54.6).Configured() {
		t.Fatal("полная конфигурация не включила курьера")
	}
}
