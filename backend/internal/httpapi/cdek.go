package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
)

type cdekService interface {
	Configured() bool
	FindCities(context.Context, string) ([]integration.CDEKCity, error)
	GetOffices(context.Context, int) ([]integration.CDEKOffice, error)
	CalculatePVZ(context.Context, int, int) (integration.CDEKQuote, error)
}

type cdekHandlers struct {
	logger  *slog.Logger
	service cdekService
}

func (handlers cdekHandlers) get(response http.ResponseWriter, request *http.Request) {
	action := request.URL.Query().Get("action")
	// The checkout asks this first, so it can drop the pick-up option
	// instead of letting a customer pick a method that cannot be completed.
	if action == "status" {
		writeJSON(response, http.StatusOK, map[string]bool{"available": handlers.available()})
		return
	}
	if !handlers.available() {
		writeJSON(response, http.StatusServiceUnavailable, errorResponse{
			Error: "Доставка СДЭК временно недоступна",
		})
		return
	}

	switch action {
	case "cities":
		city := strings.TrimSpace(request.URL.Query().Get("city"))
		if len([]rune(city)) < 2 {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Введите хотя бы 2 буквы"})
			return
		}
		cities, err := handlers.service.FindCities(request.Context(), city)
		if err != nil {
			handlers.externalError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"cities": cities})
	case "offices":
		cityCode, err := strconv.Atoi(request.URL.Query().Get("cityCode"))
		if err != nil || cityCode <= 0 {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Выберите город"})
			return
		}
		offices, err := handlers.service.GetOffices(request.Context(), cityCode)
		if err != nil {
			handlers.externalError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"offices": offices})
	default:
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Неизвестная операция"})
	}
}

func (handlers cdekHandlers) calculate(response http.ResponseWriter, request *http.Request) {
	if !handlers.available() {
		writeJSON(response, http.StatusServiceUnavailable, errorResponse{
			Error: "Доставка СДЭК временно недоступна",
		})
		return
	}
	var body struct {
		CityCode  int `json:"cityCode"`
		ItemCount int `json:"itemCount"`
	}
	if err := decodeJSON(request, &body); err != nil || body.CityCode <= 0 {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Выберите город"})
		return
	}
	quote, err := handlers.service.CalculatePVZ(
		request.Context(),
		body.CityCode,
		max(1, min(10, body.ItemCount)),
	)
	if err != nil {
		handlers.externalError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"quote": quote})
}

// available reports whether pick-up points can be offered at all. A missing
// service means the shop was assembled without CDEK, which counts as off
// rather than as a crash.
func (handlers cdekHandlers) available() bool {
	return handlers.service != nil && handlers.service.Configured()
}

func (handlers cdekHandlers) externalError(response http.ResponseWriter, err error) {
	handlers.logger.Error("cdek request failed", "error", err)
	writeJSON(response, http.StatusBadGateway, errorResponse{Error: err.Error()})
}
