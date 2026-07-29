package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
)

type catalogRepository interface {
	ListAvailable(context.Context) ([]catalog.Product, error)
}

type Dependencies struct {
	Catalog      catalogRepository
	Auth         authService
	Orders       orderRepository
	OrderCreator orderCreator
	CDEK         cdekService
	Admin        adminRepository
	Saby         sabySyncService
	AdminEmails  []string
	CookieSecure bool
	StaticDir    string
}

func NewRouter(logger *slog.Logger, dependencies Dependencies) http.Handler {
	mux := http.NewServeMux()
	authAPI := authHandlers{
		logger:       logger,
		service:      dependencies.Auth,
		cookieSecure: dependencies.CookieSecure,
	}
	cdekAPI := cdekHandlers{logger: logger, service: dependencies.CDEK}
	adminEmails := make(map[string]struct{}, len(dependencies.AdminEmails))
	for _, email := range dependencies.AdminEmails {
		adminEmails[strings.ToLower(email)] = struct{}{}
	}
	mux.HandleFunc("GET /api/v1/health", func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.Handle("GET /api/v1/catalog", catalogHandler(logger, dependencies.Catalog))
	mux.HandleFunc("POST /api/v1/auth/register", authAPI.register)
	mux.HandleFunc("POST /api/v1/auth/login", authAPI.login)
	mux.HandleFunc("POST /api/v1/auth/logout", authAPI.logout)
	mux.HandleFunc("GET /api/v1/auth/me", authAPI.me)
	mux.Handle(
		"GET /api/v1/account/orders",
		accountOrdersHandler(logger, dependencies.Auth, dependencies.Orders),
	)
	mux.HandleFunc("GET /api/v1/delivery/cdek", cdekAPI.get)
	mux.HandleFunc("POST /api/v1/delivery/cdek", cdekAPI.calculate)
	mux.Handle(
		"POST /api/v1/orders",
		createOrderHandler(logger, dependencies.Auth, dependencies.OrderCreator),
	)
	mux.Handle(
		"GET /api/v1/admin/dashboard",
		adminDashboardHandler(logger, dependencies.Auth, dependencies.Admin, adminEmails),
	)
	mux.Handle(
		"POST /api/v1/integrations/saby/catalog",
		sabyCatalogSyncHandler(logger, dependencies.Saby),
	)

	var handler http.Handler = mux
	if dependencies.StaticDir != "" {
		handler = spaFallback(mux, dependencies.StaticDir)
	}

	return requestLogger(logger, recoverPanics(logger, handler))
}

func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		startedAt := time.Now()
		next.ServeHTTP(response, request)
		logger.Info(
			"http request",
			"method", request.Method,
			"path", request.URL.Path,
			"duration_ms", time.Since(startedAt).Milliseconds(),
		)
	})
}

func recoverPanics(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("request panic", "value", recovered)
				writeJSON(response, http.StatusInternalServerError, errorResponse{
					Error: "Внутренняя ошибка сервера",
				})
			}
		}()
		next.ServeHTTP(response, request)
	})
}
