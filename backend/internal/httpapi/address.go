package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// addressSuggestHandler proxies Yandex's address suggest API so the key
// stays on the server. Without a key the endpoint reports "no suggestions"
// instead of failing, which lets the address field fall back to plain text
// entry until the key is configured.
func addressSuggestHandler(logger *slog.Logger, apiKey string) http.Handler {
	client := &http.Client{Timeout: 8 * time.Second}
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		query := strings.TrimSpace(request.URL.Query().Get("q"))
		if apiKey == "" || len([]rune(query)) < 3 {
			writeJSON(response, http.StatusOK, map[string]any{"suggestions": []string{}})
			return
		}

		values := url.Values{}
		values.Set("apikey", apiKey)
		values.Set("text", query)
		values.Set("lang", "ru")
		values.Set("results", "7")
		values.Set("types", "house,street,locality")
		values.Set("print_address", "1")

		outgoing, err := http.NewRequestWithContext(
			request.Context(),
			http.MethodGet,
			"https://suggest-maps.yandex.ru/v1/suggest?"+values.Encode(),
			nil,
		)
		if err != nil {
			writeJSON(response, http.StatusOK, map[string]any{"suggestions": []string{}})
			return
		}
		result, err := client.Do(outgoing)
		if err != nil {
			logger.Error("address suggest failed", "error", err)
			writeJSON(response, http.StatusOK, map[string]any{"suggestions": []string{}})
			return
		}
		defer result.Body.Close()

		var payload struct {
			Results []struct {
				Title struct {
					Text string `json:"text"`
				} `json:"title"`
				Subtitle struct {
					Text string `json:"text"`
				} `json:"subtitle"`
				Address struct {
					FormattedAddress string `json:"formatted_address"`
				} `json:"address"`
			} `json:"results"`
		}
		if err := json.NewDecoder(result.Body).Decode(&payload); err != nil {
			logger.Error("decode address suggest", "error", err)
			writeJSON(response, http.StatusOK, map[string]any{"suggestions": []string{}})
			return
		}

		suggestions := make([]string, 0, len(payload.Results))
		for _, item := range payload.Results {
			switch {
			case item.Address.FormattedAddress != "":
				suggestions = append(suggestions, item.Address.FormattedAddress)
			case item.Subtitle.Text != "":
				suggestions = append(suggestions, item.Subtitle.Text+", "+item.Title.Text)
			case item.Title.Text != "":
				suggestions = append(suggestions, item.Title.Text)
			}
		}
		writeJSON(response, http.StatusOK, map[string]any{"suggestions": suggestions})
	})
}
