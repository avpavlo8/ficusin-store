package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

type organizationSuggestion struct {
	Name         string `json:"name"`
	INN          string `json:"inn"`
	KPP          string `json:"kpp"`
	LegalAddress string `json:"legalAddress"`
}

type organizationSuggester struct {
	logger   *slog.Logger
	key      string
	endpoint string
	client   *http.Client
}

func organizationSuggestHandler(logger *slog.Logger, key string) http.Handler {
	return &organizationSuggester{logger: logger, key: key, endpoint: "https://suggestions.dadata.ru/suggestions/api/4_1/rs/suggest/party", client: &http.Client{Timeout: 5 * time.Second}}
}

func (handler *organizationSuggester) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	query := strings.TrimSpace(request.URL.Query().Get("q"))
	if utf8.RuneCountInString(query) > 150 {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "Введите название или ИНН короче 150 символов"})
		return
	}
	if handler.key == "" || utf8.RuneCountInString(query) < 3 {
		writeJSON(response, http.StatusOK, map[string]any{"available": handler.key != "", "suggestions": []organizationSuggestion{}})
		return
	}
	body, _ := json.Marshal(map[string]any{"query": query, "count": 7, "status": []string{"ACTIVE"}})
	outgoing, err := http.NewRequestWithContext(request.Context(), http.MethodPost, handler.endpoint, bytes.NewReader(body))
	if err != nil {
		handler.unavailable(response)
		return
	}
	outgoing.Header.Set("Authorization", "Token "+handler.key)
	outgoing.Header.Set("Content-Type", "application/json")
	outgoing.Header.Set("Accept", "application/json")
	result, err := handler.client.Do(outgoing)
	if err != nil {
		if request.Context().Err() == nil {
			handler.logger.Warn("organization suggestions unavailable")
		}
		handler.unavailable(response)
		return
	}
	defer result.Body.Close()
	if result.StatusCode != http.StatusOK {
		handler.logger.Warn("organization suggestions provider rejected request", "status", result.StatusCode)
		handler.unavailable(response)
		return
	}
	var payload struct {
		Suggestions []struct {
			Value string `json:"value"`
			Data  struct {
				INN  string `json:"inn"`
				KPP  string `json:"kpp"`
				Name struct {
					Short string `json:"short_with_opf"`
					Full  string `json:"full_with_opf"`
				} `json:"name"`
				Address struct {
					Value string `json:"value"`
					Full  string `json:"unrestricted_value"`
					Data  struct {
						Source string `json:"source"`
					} `json:"data"`
				} `json:"address"`
			} `json:"data"`
		} `json:"suggestions"`
	}
	if json.NewDecoder(io.LimitReader(result.Body, 1<<20)).Decode(&payload) != nil {
		handler.unavailable(response)
		return
	}
	suggestions := make([]organizationSuggestion, 0, 7)
	seen := map[string]bool{}
	for _, item := range payload.Suggestions {
		data := item.Data
		if innLengthInvalid(data.INN) || digitsOnly.ReplaceAllString(data.INN, "") != data.INN {
			continue
		}
		name := strings.TrimSpace(data.Name.Short)
		if name == "" {
			name = strings.TrimSpace(item.Value)
		}
		if name == "" {
			name = strings.TrimSpace(data.Name.Full)
		}
		if name == "" {
			continue
		}
		identity := data.INN + ":" + data.KPP
		if seen[identity] {
			continue
		}
		seen[identity] = true
		address := strings.TrimSpace(data.Address.Data.Source)
		if address == "" {
			address = strings.TrimSpace(data.Address.Full)
		}
		if address == "" {
			address = strings.TrimSpace(data.Address.Value)
		}
		suggestions = append(suggestions, organizationSuggestion{Name: name, INN: data.INN, KPP: data.KPP, LegalAddress: address})
		if len(suggestions) == 7 {
			break
		}
	}
	writeJSON(response, http.StatusOK, map[string]any{"available": true, "suggestions": suggestions})
}

func (handler *organizationSuggester) unavailable(response http.ResponseWriter) {
	writeJSON(response, http.StatusServiceUnavailable, map[string]string{"error": "Справочник временно недоступен. Попробуйте позже."})
}
