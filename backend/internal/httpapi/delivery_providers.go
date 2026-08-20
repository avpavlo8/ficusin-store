package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
)

type deliveryPricer interface {
	Configured() bool
	Calculate(context.Context, string, integration.Parcel) (integration.DeliveryQuote, error)
}

type deliveryQuoteHandlers struct {
	logger   *slog.Logger
	post     deliveryPricer
	courier  deliveryPricer
	packages packageRepository
}

func (handlers deliveryQuoteHandlers) providers(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]bool{
		"post":    configuredPricer(handlers.post),
		"courier": configuredPricer(handlers.courier),
	})
}

func (handlers deliveryQuoteHandlers) postQuote(response http.ResponseWriter, request *http.Request) {
	handlers.quote(response, request, handlers.post, "Почта России")
}

func (handlers deliveryQuoteHandlers) courierQuote(response http.ResponseWriter, request *http.Request) {
	handlers.quote(response, request, handlers.courier, "Яндекс Доставка")
}

func (handlers deliveryQuoteHandlers) quote(response http.ResponseWriter, request *http.Request, pricer deliveryPricer, providerName string) {
	if !configuredPricer(pricer) {
		writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: providerName + " временно недоступна"})
		return
	}
	var body struct {
		Address string `json:"address"`
		Items []struct {
			ID       string `json:"id"`
			Quantity int    `json:"quantity"`
		} `json:"items"`
	}
	if err := decodeJSON(request, &body); err != nil || strings.TrimSpace(body.Address) == "" {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Укажите адрес доставки"})
		return
	}
	box, measured, err := quoteParcel(request.Context(), handlers.packages, body.Items)
	if err != nil {
		handlers.logger.Error("load package sizes for delivery quote failed", "provider", providerName, "error", err)
		writeJSON(response, http.StatusOK, quoteUnavailable)
		return
	}
	if !measured {
		writeJSON(response, http.StatusOK, quoteUnavailable)
		return
	}
	quote, err := pricer.Calculate(request.Context(), strings.TrimSpace(body.Address), box)
	if errors.Is(err, integration.ErrRussianPostAddress) {
		// Неизвестный адрес — повод уточнить заказ, а не выкинуть корзину.
		writeJSON(response, http.StatusOK, quoteUnavailable)
		return
	}
	if errors.Is(err, integration.ErrYandexOutsideRyazan) {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Курьер Яндекс Доставки доступен только по Рязани"})
		return
	}
	if err != nil {
		handlers.logger.Error("delivery quote failed", "provider", providerName, "error", err)
		writeJSON(response, http.StatusOK, quoteUnavailable)
		return
	}
	if quote.Price <= 0 {
		writeJSON(response, http.StatusOK, quoteUnavailable)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"quote": quote})
}

func configuredPricer(pricer deliveryPricer) bool {
	return pricer != nil && pricer.Configured()
}

func quoteParcel(ctx context.Context, repository packageRepository, items []struct {
	ID       string `json:"id"`
	Quantity int    `json:"quantity"`
}) (integration.Parcel, bool, error) {
	slugs := make([]string, 0, len(items))
	for _, item := range items {
		if id := strings.TrimSpace(item.ID); id != "" {
			slugs = append(slugs, id)
		}
	}
	if len(slugs) == 0 {
		return integration.Parcel{}, false, nil
	}
	sizes := map[string]catalog.PackageSize{}
	if repository != nil {
		loaded, err := repository.PackageSizes(ctx, slugs)
		if err != nil {
			return integration.Parcel{}, false, err
		}
		sizes = loaded
	}
	parcels := make([]integration.Parcel, 0, len(items))
	for _, item := range items {
		size := sizes[strings.TrimSpace(item.ID)]
		parcel := integration.Parcel{
			LengthCM: size.LengthCM, WidthCM: size.WidthCM,
			HeightCM: size.HeightCM, WeightGrams: size.WeightGrams,
		}
		for count := 0; count < max(1, min(20, item.Quantity)); count++ {
			parcels = append(parcels, parcel)
		}
	}
	box, measured := integration.CombineParcels(parcels)
	return box, measured, nil
}
