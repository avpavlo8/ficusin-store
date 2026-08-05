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

// DefaultParcel is used while a product has no dimensions filled in yet.
// It is a small pot in a snug box: understating the box is cheaper to
// discover at the counter than overcharging every customer up front.
var DefaultParcel = Parcel{LengthCM: 40, WidthCM: 25, HeightCM: 25, WeightGrams: 1500}

// ParcelOrDefault substitutes the fallback box for an unmeasured product, so
// an item with no dimensions still takes up room in the quote instead of
// silently shipping for free.
func ParcelOrDefault(parcel Parcel) Parcel {
	if parcel.LengthCM <= 0 || parcel.WidthCM <= 0 || parcel.HeightCM <= 0 {
		return DefaultParcel
	}
	if parcel.WeightGrams <= 0 {
		parcel.WeightGrams = DefaultParcel.WeightGrams
	}
	return parcel
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
func CombineParcels(parcels []Parcel) Parcel {
	combined := Parcel{}
	for _, parcel := range parcels {
		sides := []int{parcel.LengthCM, parcel.WidthCM, parcel.HeightCM}
		sort.Sort(sort.Reverse(sort.IntSlice(sides)))
		combined.LengthCM = max(combined.LengthCM, sides[0])
		combined.WidthCM += sides[1]
		combined.HeightCM = max(combined.HeightCM, sides[2])
		combined.WeightGrams += parcel.WeightGrams
	}
	if combined.LengthCM <= 0 || combined.WidthCM <= 0 || combined.HeightCM <= 0 {
		return DefaultParcel
	}
	if combined.WeightGrams <= 0 {
		combined.WeightGrams = DefaultParcel.WeightGrams
	}
	return combined
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
