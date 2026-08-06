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
	"sync"
	"time"
)

const (
	cdekBaseURL      = "https://api.cdek.ru/v2"
	cdekFromCityCode = 159
)

type CDEKCity struct {
	Code        int    `json:"code"`
	City        string `json:"city"`
	Region      string `json:"region,omitempty"`
	Country     string `json:"country,omitempty"`
	CountryCode string `json:"country_code,omitempty"`
}

type CDEKLocation struct {
	City        string `json:"city"`
	Address     string `json:"address"`
	AddressFull string `json:"address_full,omitempty"`
}

type CDEKOffice struct {
	Code     string       `json:"code"`
	Name     string       `json:"name"`
	Type     string       `json:"type,omitempty"`
	Location CDEKLocation `json:"location"`
	WorkTime string       `json:"work_time,omitempty"`
}

type CDEKQuote struct {
	TariffCode int    `json:"tariffCode"`
	TariffName string `json:"tariffName"`
	Price      int    `json:"price"`
	DaysMin    int    `json:"daysMin"`
	DaysMax    int    `json:"daysMax"`
}

// Parcel is one box, in centimetres and grams.
//
// Size matters as much as weight: CDEK bills whichever is larger, the real
// weight or the volumetric one (length × width × height ÷ 5000). A plant
// weighs little and takes a lot of room, so the box is almost always what we
// are paying for — which is why every product carries its own dimensions.
type Parcel struct {
	LengthCM    int
	WidthCM     int
	HeightCM    int
	WeightGrams int
}

// Measured reports whether the box has been filled in. An unmeasured product
// gets no quote at all: a guessed size becomes a real number on the checkout
// page, and a price we cannot stand behind is worse than saying the manager
// will work it out.
func (parcel Parcel) Measured() bool {
	return parcel.LengthCM > 0 && parcel.WidthCM > 0 &&
		parcel.HeightCM > 0 && parcel.WeightGrams > 0
}

// CombineParcels puts several boxes into the one that will be shipped.
//
// The boxes are stood side by side: each is laid down on its longest side,
// the shipping box is as long and as tall as the largest of them, and as
// wide as all of them together. A pineapple in 40×20×20 next to a monstera
// in 60×20×20 travels as 60×40×20.
//
// This overstates nothing and understates nothing badly: boxes really do
// stand next to each other, and stacking them smarter is the packer's job,
// not something a price quote should assume.
//
// One unmeasured item makes the whole shipment unmeasured — it would travel
// in the same van, and pretending it takes no room would quote a price the
// shop cannot honour.
func CombineParcels(parcels []Parcel) (Parcel, bool) {
	if len(parcels) == 0 {
		return Parcel{}, false
	}
	combined := Parcel{}
	for _, parcel := range parcels {
		if !parcel.Measured() {
			return Parcel{}, false
		}
		sides := []int{parcel.LengthCM, parcel.WidthCM, parcel.HeightCM}
		sort.Sort(sort.Reverse(sort.IntSlice(sides)))
		combined.LengthCM = max(combined.LengthCM, sides[0])
		combined.WidthCM += sides[1]
		combined.HeightCM = max(combined.HeightCM, sides[2])
		combined.WeightGrams += parcel.WeightGrams
	}
	return combined, true
}

type CDEKClient struct {
	clientID     string
	clientSecret string
	httpClient   *http.Client
	mu           sync.Mutex
	token        string
	tokenExpiry  time.Time
}

func NewCDEKClient(clientID, clientSecret string) *CDEKClient {
	return &CDEKClient{
		clientID:     strings.TrimSpace(clientID),
		clientSecret: strings.TrimSpace(clientSecret),
		httpClient:   &http.Client{Timeout: 20 * time.Second},
	}
}

// Configured reports whether delivery by CDEK can work at all. The checkout
// asks before offering it, so a shop without keys does not send customers
// down a road that ends in an error.
func (client *CDEKClient) Configured() bool {
	return client.clientID != "" && client.clientSecret != ""
}

func (client *CDEKClient) FindCities(ctx context.Context, city string) ([]CDEKCity, error) {
	query := url.Values{"city": {city}, "country_codes": {"RU"}, "size": {"20"}}
	var cities []CDEKCity
	if err := client.request(ctx, http.MethodGet, "/location/cities?"+query.Encode(), nil, &cities); err != nil {
		return nil, err
	}
	filtered := cities[:0]
	for _, item := range cities {
		if item.CountryCode == "" || item.CountryCode == "RU" {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func (client *CDEKClient) GetOffices(ctx context.Context, cityCode int) ([]CDEKOffice, error) {
	query := url.Values{
		"city_code":  {strconv.Itoa(cityCode)},
		"type":       {"PVZ"},
		"is_handout": {"true"},
	}
	var offices []CDEKOffice
	if err := client.request(ctx, http.MethodGet, "/deliverypoints?"+query.Encode(), nil, &offices); err != nil {
		return nil, err
	}
	filtered := offices[:0]
	for _, office := range offices {
		if strings.TrimSpace(office.Location.Address) != "" {
			filtered = append(filtered, office)
		}
	}
	sort.Slice(filtered, func(left, right int) bool {
		return filtered[left].Location.Address < filtered[right].Location.Address
	})
	return filtered, nil
}

// CalculatePVZ returns every tariff that ends at a pick-up point, cheapest
// first. Customers choose for themselves: the cheapest option is often three
// days slower, and that is not our call to make for them.
func (client *CDEKClient) CalculatePVZ(
	ctx context.Context,
	cityCode int,
	box Parcel,
) ([]CDEKQuote, error) {
	type tariff struct {
		Code         int     `json:"tariff_code"`
		Name         string  `json:"tariff_name"`
		DeliveryMode int     `json:"delivery_mode"`
		DeliverySum  float64 `json:"delivery_sum"`
		PeriodMin    int     `json:"period_min"`
		PeriodMax    int     `json:"period_max"`
	}
	body := map[string]any{
		"type":          1,
		"currency":      1,
		"from_location": map[string]int{"code": cdekFromCityCode},
		"to_location":   map[string]int{"code": cityCode},
		"packages": []map[string]int{{
			"weight": max(1, box.WeightGrams),
			"length": max(1, box.LengthCM),
			"width":  max(1, box.WidthCM),
			"height": max(1, box.HeightCM),
		}},
	}
	var result struct {
		Tariffs []tariff `json:"tariff_codes"`
	}
	if err := client.request(ctx, http.MethodPost, "/calculator/tarifflist", body, &result); err != nil {
		return nil, err
	}
	sort.Slice(result.Tariffs, func(left, right int) bool {
		return result.Tariffs[left].DeliverySum < result.Tariffs[right].DeliverySum
	})
	quotes := make([]CDEKQuote, 0, len(result.Tariffs))
	for _, option := range result.Tariffs {
		// Modes 2 and 4 end at a pick-up point; the rest go to the door and
		// would quote a price for something we did not offer.
		if option.DeliverySum <= 0 || (option.DeliveryMode != 2 && option.DeliveryMode != 4) {
			continue
		}
		quotes = append(quotes, CDEKQuote{
			TariffCode: option.Code,
			TariffName: option.Name,
			Price:      int(option.DeliverySum + .999999),
			DaysMin:    option.PeriodMin,
			DaysMax:    option.PeriodMax,
		})
	}
	if len(quotes) == 0 {
		return nil, errors.New("СДЭК не нашёл доставку до пункта выдачи")
	}
	// A dozen near-identical tariffs is a decision, not a choice. Four is
	// enough to cover "cheapest" through "fastest".
	if len(quotes) > 4 {
		quotes = quotes[:4]
	}
	return quotes, nil
}

func (client *CDEKClient) request(
	ctx context.Context,
	method, path string,
	body any,
	destination any,
) error {
	var encoded *bytes.Reader
	if body == nil {
		encoded = bytes.NewReader(nil)
	} else {
		value, err := json.Marshal(body)
		if err != nil {
			return err
		}
		encoded = bytes.NewReader(value)
	}
	request, err := http.NewRequestWithContext(ctx, method, cdekBaseURL+path, encoded)
	if err != nil {
		return err
	}
	token, err := client.accessToken(ctx)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Ficusin-Store/1.0")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("СДЭК временно недоступен: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("СДЭК временно недоступен (%d)", response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
		return fmt.Errorf("СДЭК вернул некорректный ответ: %w", err)
	}
	return nil
}

func (client *CDEKClient) accessToken(ctx context.Context) (string, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.token != "" && time.Now().Before(client.tokenExpiry) {
		return client.token, nil
	}
	if !client.Configured() {
		return "", errors.New("учётные данные СДЭК не заданы: CDEK_CLIENT_ID и CDEK_CLIENT_SECRET")
	}
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {client.clientID},
		"client_secret": {client.clientSecret},
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		cdekBaseURL+"/oauth/token",
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", "Ficusin-Store/1.0")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("СДЭК не выдал токен: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("СДЭК не выдал токен: %d", response.StatusCode)
	}
	var result struct {
		Token     string `json:"access_token"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return "", err
	}
	client.token = result.Token
	client.tokenExpiry = time.Now().Add(time.Duration(max(60, result.ExpiresIn-120)) * time.Second)
	return client.token, nil
}

// Shipment is an order handed to CDEK.
type Shipment struct {
	// UUID is CDEK's own identifier: the only reliable way to ask about a
	// shipment later. The tracking number appears a little afterwards.
	UUID         string
	TrackNumber  string
	Status       string
	StatusReason string
}

// ShipmentRequest is everything CDEK needs to accept a parcel.
type ShipmentRequest struct {
	OrderNumber   string
	TariffCode    int
	OfficeCode    string
	CityCode      int
	Box           Parcel
	Items         []ShipmentItem
	SenderName    string
	SenderPhone   string
	SenderAddress string
	// Recipient details go to CDEK because a courier cannot deliver to
	// nobody. This is a Russian carrier under a contract, not a foreign
	// service — unlike Telegram, where contacts must never appear.
	RecipientName  string
	RecipientPhone string
	// PaymentOnDelivery is money CDEK collects at the counter. Zero for an
	// order already paid on the site.
	PaymentOnDelivery float64
}

type ShipmentItem struct {
	Name     string
	Price    float64
	Quantity int
	// WeightGrams is per unit; CDEK wants a weight for every line.
	WeightGrams int
}

// CreateOrder hands a parcel to CDEK and returns its identifier. It is
// called only for orders the shop is actually ready to ship.
func (client *CDEKClient) CreateOrder(
	ctx context.Context,
	request ShipmentRequest,
) (Shipment, error) {
	if !client.Configured() {
		return Shipment{}, errors.New("СДЭК не настроен")
	}
	if request.OfficeCode == "" || request.TariffCode <= 0 {
		return Shipment{}, errors.New("не хватает пункта выдачи или тарифа")
	}
	packages := []map[string]any{{
		"number": request.OrderNumber,
		"weight": max(1, request.Box.WeightGrams),
		"length": max(1, request.Box.LengthCM),
		"width":  max(1, request.Box.WidthCM),
		"height": max(1, request.Box.HeightCM),
		"items":  shipmentItems(request.Items),
	}}
	body := map[string]any{
		"type":            1,
		"number":          request.OrderNumber,
		"tariff_code":     request.TariffCode,
		"from_location":   map[string]any{"code": cdekFromCityCode, "address": request.SenderAddress},
		"delivery_point":  request.OfficeCode,
		"packages":        packages,
		"sender":          map[string]any{"name": request.SenderName, "phones": phones(request.SenderPhone)},
		"recipient":       map[string]any{"name": request.RecipientName, "phones": phones(request.RecipientPhone)},
		"comment":         "Заказ " + request.OrderNumber + " с сайта ficusin.ru",
	}
	if request.PaymentOnDelivery > 0 {
		// Only what the customer still owes travels as cash on delivery. An
		// order paid on the site must never be charged twice.
		body["delivery_recipient_cost"] = map[string]any{"value": request.PaymentOnDelivery}
	}
	var result struct {
		Entity struct {
			UUID string `json:"uuid"`
		} `json:"entity"`
		Requests []struct {
			State  string `json:"state"`
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		} `json:"requests"`
	}
	if err := client.request(ctx, http.MethodPost, "/orders", body, &result); err != nil {
		return Shipment{}, err
	}
	for _, attempt := range result.Requests {
		for _, failure := range attempt.Errors {
			return Shipment{}, fmt.Errorf("СДЭК отказал: %s", failure.Message)
		}
	}
	if result.Entity.UUID == "" {
		return Shipment{}, errors.New("СДЭК не вернул номер заявки")
	}
	return Shipment{UUID: result.Entity.UUID}, nil
}

// FetchOrder asks what has happened to a shipment. CDEK registers a parcel
// asynchronously, so the tracking number is often empty on the first ask.
func (client *CDEKClient) FetchOrder(ctx context.Context, uuid string) (Shipment, error) {
	if !client.Configured() {
		return Shipment{}, errors.New("СДЭК не настроен")
	}
	var result struct {
		Entity struct {
			UUID        string `json:"uuid"`
			CDEKNumber  string `json:"cdek_number"`
			Statuses    []struct {
				Code string `json:"code"`
				Name string `json:"name"`
			} `json:"statuses"`
		} `json:"entity"`
	}
	if err := client.request(ctx, http.MethodGet, "/orders/"+url.PathEscape(uuid), nil, &result); err != nil {
		return Shipment{}, err
	}
	shipment := Shipment{UUID: result.Entity.UUID, TrackNumber: result.Entity.CDEKNumber}
	// Statuses arrive oldest first; the last one is where the parcel is now.
	if count := len(result.Entity.Statuses); count > 0 {
		shipment.Status = result.Entity.Statuses[count-1].Code
		shipment.StatusReason = result.Entity.Statuses[count-1].Name
	}
	return shipment, nil
}

func shipmentItems(items []ShipmentItem) []map[string]any {
	encoded := make([]map[string]any, 0, len(items))
	for _, item := range items {
		encoded = append(encoded, map[string]any{
			"name":        truncateRunes(item.Name, 120),
			"ware_key":    truncateRunes(item.Name, 20),
			"payment":     map[string]any{"value": 0},
			"cost":        item.Price,
			"weight":      max(1, item.WeightGrams),
			"amount":      max(1, item.Quantity),
		})
	}
	return encoded
}

func phones(number string) []map[string]string {
	if strings.TrimSpace(number) == "" {
		return []map[string]string{}
	}
	return []map[string]string{{"number": number}}
}
