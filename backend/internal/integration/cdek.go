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

type CDEKCredentials struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
}

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

type CDEKClient struct {
	credentials *CredentialStore
	httpClient  *http.Client
	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
}

func NewCDEKClient(credentials *CredentialStore) *CDEKClient {
	return &CDEKClient{
		credentials: credentials,
		httpClient:  &http.Client{Timeout: 20 * time.Second},
	}
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

func (client *CDEKClient) CalculatePVZ(
	ctx context.Context,
	cityCode, itemCount int,
) (CDEKQuote, error) {
	type tariff struct {
		Code         int     `json:"tariff_code"`
		Name         string  `json:"tariff_name"`
		DeliveryMode int     `json:"delivery_mode"`
		DeliverySum  float64 `json:"delivery_sum"`
		PeriodMin    int     `json:"period_min"`
		PeriodMax    int     `json:"period_max"`
	}
	packages := make([]map[string]int, max(1, min(10, itemCount)))
	for index := range packages {
		packages[index] = map[string]int{
			"weight": 2500,
			"length": 35,
			"width":  35,
			"height": 60,
		}
	}
	body := map[string]any{
		"type":          1,
		"currency":      1,
		"from_location": map[string]int{"code": cdekFromCityCode},
		"to_location":   map[string]int{"code": cityCode},
		"packages":      packages,
	}
	var result struct {
		Tariffs []tariff `json:"tariff_codes"`
	}
	if err := client.request(ctx, http.MethodPost, "/calculator/tarifflist", body, &result); err != nil {
		return CDEKQuote{}, err
	}
	sort.Slice(result.Tariffs, func(left, right int) bool {
		return result.Tariffs[left].DeliverySum < result.Tariffs[right].DeliverySum
	})
	for _, option := range result.Tariffs {
		if option.DeliverySum > 0 && (option.DeliveryMode == 2 || option.DeliveryMode == 4) {
			return CDEKQuote{
				TariffCode: option.Code,
				TariffName: option.Name,
				Price:      int(option.DeliverySum + .999999),
				DaysMin:    option.PeriodMin,
				DaysMax:    option.PeriodMax,
			}, nil
		}
	}
	return CDEKQuote{}, errors.New("СДЭК не нашёл доставку до пункта выдачи")
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
	credentials, err := GetCredentials[CDEKCredentials](ctx, client.credentials, "cdek")
	if err != nil {
		return "", err
	}
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {strings.TrimSpace(credentials.ClientID)},
		"client_secret": {strings.TrimSpace(credentials.ClientSecret)},
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
