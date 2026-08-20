package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	yandexDeliveryAPIBaseURL = "https://b2b.taxi.yandex.net"
	yandexGeocoderBaseURL    = "https://geocode-maps.yandex.ru/1.x/"
)

var ErrYandexOutsideRyazan = errors.New("yandex delivery address is outside Ryazan")

type YandexDeliveryClient struct {
	token          string
	geocoderKey    string
	senderAddress  string
	senderLongitude float64
	senderLatitude  float64
	deliveryBaseURL string
	geocoderBaseURL string
	httpClient      *http.Client
}

func NewYandexDeliveryClient(token, geocoderKey, senderAddress string, senderLongitude, senderLatitude float64) *YandexDeliveryClient {
	return &YandexDeliveryClient{
		token:           strings.TrimSpace(token),
		geocoderKey:     strings.TrimSpace(geocoderKey),
		senderAddress:   strings.TrimSpace(senderAddress),
		senderLongitude: senderLongitude,
		senderLatitude:  senderLatitude,
		deliveryBaseURL: yandexDeliveryAPIBaseURL,
		geocoderBaseURL: yandexGeocoderBaseURL,
		httpClient:      &http.Client{Timeout: 10 * time.Second},
	}
}

func (client *YandexDeliveryClient) Configured() bool {
	return client != nil && client.token != "" && client.geocoderKey != "" && client.senderAddress != "" && client.senderLongitude != 0 && client.senderLatitude != 0
}

type yandexPoint struct {
	Longitude float64
	Latitude  float64
	Address   string
	City      string
}

type yandexOfferResponse struct {
	Offers []struct {
		Price struct {
			TotalPrice        string `json:"total_price"`
			TotalPriceWithVAT string `json:"total_price_with_vat"`
			Currency          string `json:"currency"`
		} `json:"price"`
		TaxiClass string `json:"taxi_class"`
		Pickup struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"pickup_interval"`
		Delivery struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"delivery_interval"`
	} `json:"offers"`
}

func (client *YandexDeliveryClient) Calculate(ctx context.Context, address string, parcel Parcel) (DeliveryQuote, error) {
	if !client.Configured() {
		return DeliveryQuote{}, errors.New("yandex delivery is not configured")
	}
	if !parcel.Measured() {
		return DeliveryQuote{}, errors.New("parcel dimensions are missing")
	}
	destination, err := client.geocode(ctx, address)
	if err != nil {
		return DeliveryQuote{}, err
	}
	if !strings.EqualFold(strings.TrimSpace(destination.City), "Рязань") {
		return DeliveryQuote{}, ErrYandexOutsideRyazan
	}

	body := map[string]any{
		"items": []map[string]any{{
			"size": map[string]float64{
				"length": float64(parcel.LengthCM) / 100,
				"width":  float64(parcel.WidthCM) / 100,
				"height": float64(parcel.HeightCM) / 100,
			},
			"weight":        float64(parcel.WeightGrams) / 1000,
			"quantity":      1,
			"pickup_point":  1,
			"dropoff_point": 2,
			"age_restricted": false,
		}},
		"route_points": []map[string]any{
			{
				"id": 1,
				"coordinates": []float64{client.senderLongitude, client.senderLatitude},
				"fullname": client.senderAddress,
				"country": "Россия",
				"city": "Рязань",
			},
			{
				"id": 2,
				"coordinates": []float64{destination.Longitude, destination.Latitude},
				"fullname": destination.Address,
				"country": "Россия",
				"city": "Рязань",
			},
		},
		// Asking for all useful classes lets Yandex choose a bike/car/cargo
		// option that really fits the plant instead of us guessing from its
		// height. Oversized plants therefore fail closed rather than getting a
		// cheap quote for a vehicle they cannot enter.
		"requirements": map[string]any{
			"taxi_classes": []string{"courier", "express", "cargo"},
			"cargo_type": "lcv_m",
			"cargo_loaders": 0,
			"skip_door_to_door": false,
		},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return DeliveryQuote{}, fmt.Errorf("encode yandex delivery request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(client.deliveryBaseURL, "/")+"/b2b/cargo/integration/v2/offers/calculate", bytes.NewReader(encoded))
	if err != nil {
		return DeliveryQuote{}, fmt.Errorf("build yandex delivery request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("Accept-Language", "ru")
	request.Header.Set("Content-Type", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return DeliveryQuote{}, fmt.Errorf("yandex delivery request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return DeliveryQuote{}, fmt.Errorf("yandex delivery returned HTTP %d", response.StatusCode)
	}
	var payload yandexOfferResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return DeliveryQuote{}, fmt.Errorf("decode yandex delivery response: %w", err)
	}
	if len(payload.Offers) == 0 {
		return DeliveryQuote{}, errors.New("yandex delivery returned no offers")
	}

	type pricedOffer struct {
		price float64
		class string
	}
	priced := make([]pricedOffer, 0, len(payload.Offers))
	for _, offer := range payload.Offers {
		value := strings.TrimSpace(offer.Price.TotalPriceWithVAT)
		if value == "" {
			value = strings.TrimSpace(offer.Price.TotalPrice)
		}
		price, parseErr := strconv.ParseFloat(value, 64)
		if parseErr == nil && price > 0 && (offer.Price.Currency == "" || offer.Price.Currency == "RUB") {
			priced = append(priced, pricedOffer{price: price, class: offer.TaxiClass})
		}
	}
	if len(priced) == 0 {
		return DeliveryQuote{}, errors.New("yandex delivery returned no priced offers")
	}
	sort.Slice(priced, func(i, j int) bool { return priced[i].price < priced[j].price })
	return DeliveryQuote{Price: priced[0].price, Service: "Яндекс Доставка · " + yandexClassTitle(priced[0].class)}, nil
}

func (client *YandexDeliveryClient) geocode(ctx context.Context, address string) (yandexPoint, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return yandexPoint{}, errors.New("delivery address is empty")
	}
	values := url.Values{}
	values.Set("apikey", client.geocoderKey)
	values.Set("geocode", address)
	values.Set("format", "json")
	values.Set("results", "1")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.geocoderBaseURL+"?"+values.Encode(), nil)
	if err != nil {
		return yandexPoint{}, fmt.Errorf("build yandex geocoder request: %w", err)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return yandexPoint{}, fmt.Errorf("yandex geocoder request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return yandexPoint{}, fmt.Errorf("yandex geocoder returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Response struct {
			GeoObjectCollection struct {
				FeatureMember []struct {
					GeoObject struct {
						MetaDataProperty struct {
							GeocoderMetaData struct {
								Text    string `json:"text"`
								Address struct {
									Formatted  string `json:"formatted"`
									Components []struct {
										Kind string `json:"kind"`
										Name string `json:"name"`
									} `json:"Components"`
								} `json:"Address"`
							} `json:"GeocoderMetaData"`
						} `json:"metaDataProperty"`
						Point struct { Pos string `json:"pos"` } `json:"Point"`
					} `json:"GeoObject"`
				} `json:"featureMember"`
			} `json:"GeoObjectCollection"`
		} `json:"response"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return yandexPoint{}, fmt.Errorf("decode yandex geocoder response: %w", err)
	}
	members := payload.Response.GeoObjectCollection.FeatureMember
	if len(members) == 0 {
		return yandexPoint{}, errors.New("yandex geocoder found no address")
	}
	object := members[0].GeoObject
	parts := strings.Fields(object.Point.Pos)
	if len(parts) != 2 {
		return yandexPoint{}, errors.New("yandex geocoder returned invalid coordinates")
	}
	longitude, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return yandexPoint{}, errors.New("yandex geocoder returned invalid longitude")
	}
	latitude, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return yandexPoint{}, errors.New("yandex geocoder returned invalid latitude")
	}
	city := ""
	for _, component := range object.MetaDataProperty.GeocoderMetaData.Address.Components {
		if component.Kind == "locality" {
			city = component.Name
			break
		}
	}
	formatted := strings.TrimSpace(object.MetaDataProperty.GeocoderMetaData.Address.Formatted)
	if formatted == "" {
		formatted = strings.TrimSpace(object.MetaDataProperty.GeocoderMetaData.Text)
	}
	return yandexPoint{Longitude: longitude, Latitude: latitude, Address: formatted, City: city}, nil
}

func yandexClassTitle(value string) string {
	switch value {
	case "courier":
		return "Курьер"
	case "express":
		return "Экспресс"
	case "cargo":
		return "Грузовой"
	default:
		return "Курьер"
	}
}
