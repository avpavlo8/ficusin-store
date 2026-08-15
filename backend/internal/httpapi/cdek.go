package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
)

type cdekService interface {
	Configured() bool
	FindCities(context.Context, string) ([]integration.CDEKCity, error)
	GetOffices(context.Context, int) ([]integration.CDEKOffice, error)
	CalculatePVZ(context.Context, int, integration.Parcel) ([]integration.CDEKQuote, error)
}

// packageRepository supplies the box each product travels in, so the price
// is quoted for the parcel that will actually be shipped.
type packageRepository interface {
	PackageSizes(context.Context, []string) (map[string]catalog.PackageSize, error)
}

type cdekHandlers struct {
	logger   *slog.Logger
	service  cdekService
	packages packageRepository
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
		CityCode int `json:"cityCode"`
		Items    []struct {
			ID       string `json:"id"`
			Quantity int    `json:"quantity"`
		} `json:"items"`
	}
	if err := decodeJSON(request, &body); err != nil || body.CityCode <= 0 {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Выберите город"})
		return
	}

	slugs := make([]string, 0, len(body.Items))
	for _, item := range body.Items {
		if id := strings.TrimSpace(item.ID); id != "" {
			slugs = append(slugs, id)
		}
	}
	sizes := map[string]catalog.PackageSize{}
	if handlers.packages != nil && len(slugs) > 0 {
		loaded, err := handlers.packages.PackageSizes(request.Context(), slugs)
		if err != nil {
			handlers.logger.Error("load package sizes failed", "error", err)
		} else {
			sizes = loaded
		}
	}
	parcels := make([]integration.Parcel, 0, len(body.Items))
	for _, item := range body.Items {
		size := sizes[strings.TrimSpace(item.ID)]
		parcel := integration.Parcel{
			LengthCM:    size.LengthCM,
			WidthCM:     size.WidthCM,
			HeightCM:    size.HeightCM,
			WeightGrams: size.WeightGrams,
		}
		for count := 0; count < max(1, min(20, item.Quantity)); count++ {
			parcels = append(parcels, parcel)
		}
	}

	box, measured := integration.CombineParcels(parcels)
	if !measured {
		// Some plant has no box filled in. The order still goes through —
		// the manager works the price out and calls back.
		writeJSON(response, http.StatusOK, quoteUnavailable)
		return
	}
	quotes, err := handlers.service.CalculatePVZ(request.Context(), body.CityCode, box)
	if err != nil {
		// CDEK being down is our problem, not the customer's. Losing the
		// order over it would be the worse outcome.
		handlers.logger.Error("cdek quote failed", "error", err)
		writeJSON(response, http.StatusOK, quoteUnavailable)
		return
	}
	// "quote" stays for the cheapest option: the checkout preselects it, and
	// older clients that only read this field keep working.
	writeJSON(response, http.StatusOK, map[string]any{
		"quote":  quotes[0],
		"quotes": quotes,
	})
}

// quoteUnavailable is the answer whenever a price cannot be produced, for
// whatever reason. The checkout shows the same sentence either way: the
// customer does not care which of our systems is having a moment.
var quoteUnavailable = map[string]any{
	"pending": true,
	"message": "Оплата после подтверждения заказа менеджером",
}

// available reports whether pick-up points can be offered at all. A missing
// service means the shop was assembled without CDEK, which counts as off
// rather than as a crash.
func (handlers cdekHandlers) available() bool {
	return handlers.service != nil && handlers.service.Configured()
}

func (handlers cdekHandlers) externalError(response http.ResponseWriter, err error) {
	handlers.logger.Error("cdek request failed", "error", err)
	writeJSON(response, http.StatusBadGateway, errorResponse{
		Error: "Не удалось получить данные СДЭК. Попробуйте ещё раз позже",
	})
}
