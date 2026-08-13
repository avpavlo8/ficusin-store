package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultSabyAuth = "https://online.sbis.ru/oauth/service/"
	defaultSabyAPI  = "https://api.sbis.ru"
)

// SabyClient caches one service session. Saby allows only five active
// sessions per application, so authenticating for every request is unsafe.
type SabyClient struct {
	client       *http.Client
	appClientID  string
	appSecret    string
	secretKey    string
	pointID      int64
	priceListID  int64
	authURL      string
	apiBase      string
	mu           sync.Mutex
	token        string
	tokenUntil   time.Time
}

func NewSabyClient(appClientID, appSecret, secretKey string, pointID, priceListID int64) *SabyClient {
	return &SabyClient{
		client: &http.Client{Timeout: 20 * time.Second},
		appClientID: strings.TrimSpace(appClientID), appSecret: strings.TrimSpace(appSecret),
		secretKey: strings.TrimSpace(secretKey), pointID: pointID, priceListID: priceListID,
		authURL: defaultSabyAuth, apiBase: defaultSabyAPI,
	}
}

func (client *SabyClient) Configured() bool {
	return client != nil && client.appClientID != "" && client.appSecret != "" && client.secretKey != "" &&
		client.pointID > 0 && client.priceListID > 0
}

// Probe is deliberately read-only and requests a single catalogue row from
// the configured point and price list.
func (client *SabyClient) Probe(ctx context.Context) error {
	if !client.Configured() {
		return errors.New("ключи Saby не настроены")
	}
	query := url.Values{
		"pointId": {strconv.FormatInt(client.pointID, 10)}, "priceListId": {strconv.FormatInt(client.priceListID, 10)},
		"page": {"0"}, "pageSize": {"1"}, "noStopList": {"true"},
	}
	var response map[string]json.RawMessage
	if err := client.authorizedJSON(ctx, http.MethodGet, client.apiBase+"/retail/v2/nomenclature/list?"+query.Encode(), nil, &response); err != nil {
		return fmt.Errorf("проверить Saby: %w", err)
	}
	if _, ok := response["nomenclatures"]; !ok {
		if _, ok = response["items"]; !ok {
			if _, ok = response["result"]; !ok {
				return errors.New("Saby вернул неожиданный формат каталога")
			}
		}
	}
	return nil
}

func (client *SabyClient) authorizedJSON(ctx context.Context, method, endpoint string, payload any, target any) error {
	token, err := client.accessToken(ctx, false)
	if err != nil {
		return err
	}
	status, err := client.requestJSON(ctx, method, endpoint, payload, token, target)
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		token, authErr := client.accessToken(ctx, true)
		if authErr != nil {
			return authErr
		}
		_, err = client.requestJSON(ctx, method, endpoint, payload, token, target)
	}
	return err
}

func (client *SabyClient) accessToken(ctx context.Context, force bool) (string, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if !force && client.token != "" && time.Now().Before(client.tokenUntil) {
		return client.token, nil
	}
	payload := map[string]string{
		"app_client_id": client.appClientID, "app_secret": client.appSecret, "secret_key": client.secretKey,
	}
	var response struct { Token string `json:"token"` }
	if _, err := client.requestJSON(ctx, http.MethodPost, client.authURL, payload, "", &response); err != nil {
		return "", fmt.Errorf("авторизация Saby: %w", err)
	}
	if strings.TrimSpace(response.Token) == "" {
		return "", errors.New("авторизация Saby не вернула токен")
	}
	client.token = response.Token
	// The public API does not publish a token TTL. Reuse one session for a
	// bounded period and refresh immediately on 401/403.
	client.tokenUntil = time.Now().Add(50 * time.Minute)
	return client.token, nil
}

func (client *SabyClient) requestJSON(ctx context.Context, method, endpoint string, payload any, token string, target any) (int, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil { return 0, fmt.Errorf("encode Saby request: %w", err) }
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil { return 0, fmt.Errorf("create Saby request: %w", err) }
	request.Header.Set("Accept", "application/json")
	if payload != nil { request.Header.Set("Content-Type", "application/json") }
	if token != "" { request.Header.Set("X-SBISAccessToken", token) }
	response, err := client.client.Do(request)
	if err != nil { return 0, fmt.Errorf("Saby request failed: %w", err) }
	defer response.Body.Close() //nolint:errcheck
	content, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil { return response.StatusCode, fmt.Errorf("read Saby response: %w", err) }
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, &remoteError{Status: response.StatusCode, Message: safeRemoteMessage(string(content))}
	}
	if err := json.Unmarshal(content, target); err != nil {
		return response.StatusCode, fmt.Errorf("decode Saby response: %w", err)
	}
	return response.StatusCode, nil
}
