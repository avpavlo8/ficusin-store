package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	commerceanalytics "github.com/avpavlo8/ficusin-store/backend/internal/analytics"
	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
	"github.com/avpavlo8/ficusin-store/backend/internal/settings"
)

type analyticsStore interface {
	Record(context.Context, *int64, []commerceanalytics.Event) error
	RecordOrder(context.Context, string, float64, commerceanalytics.Attribution) error
	Summary(context.Context, int) (commerceanalytics.Summary, error)
}

func analyticsConfigHandler(store settingsService) http.HandlerFunc {
	return func(response http.ResponseWriter, _ *http.Request) {
		yandexMetrikaID := 0
		if store != nil {
			yandexMetrikaID, _ = strconv.Atoi(strings.TrimSpace(store.All()[settings.MetrikaID]))
		}
		writeJSON(response, http.StatusOK, map[string]int{"yandexMetrikaId": yandexMetrikaID})
	}
}

func analyticsEventsHandler(logger *slog.Logger, authentication authService, store analyticsStore) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if store == nil {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		var body struct {
			Events []commerceanalytics.Event `json:"events"`
		}
		if err := decodeJSONWithLimit(request, &body, 256*1024); err != nil {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные аналитики"})
			return
		}
		var customerID *int64
		if cookie, err := request.Cookie(auth.CookieName); err == nil && authentication != nil {
			if user, lookupErr := authentication.UserByToken(request.Context(), cookie.Value); lookupErr == nil && user != nil {
				customerID = &user.ID
			}
		}
		if err := store.Record(request.Context(), customerID, body.Events); errors.Is(err, commerceanalytics.ErrInvalid) {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректное событие аналитики"})
			return
		} else if err != nil {
			logger.Error("record analytics failed", "error", err)
			writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Аналитика временно недоступна"})
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}
}

func analyticsSummaryHandler(logger *slog.Logger, handlers adminHandlers, store analyticsStore) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if _, _, ok := handlers.authorize(response, request, admin.PermissionAnalyticsRead); !ok {
			return
		}
		if store == nil {
			writeJSON(response, http.StatusOK, commerceanalytics.Summary{Period: 30})
			return
		}
		days, _ := strconv.Atoi(request.URL.Query().Get("days"))
		summary, err := store.Summary(request.Context(), days)
		if err != nil {
			logger.Error("analytics summary failed", "error", err)
			writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Не удалось загрузить аналитику"})
			return
		}
		writeJSON(response, http.StatusOK, summary)
	}
}
