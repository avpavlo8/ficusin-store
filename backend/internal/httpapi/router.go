package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
)

type catalogRepository interface {
	ListAvailable(context.Context) ([]catalog.Product, error)
	ListCategories(context.Context) ([]catalog.Category, error)
	DetailBySlug(context.Context, string) (catalog.ProductDetail, error)
}

type Dependencies struct {
	Catalog      catalogRepository
	Auth         authService
	Orders       orderRepository
	OrderCreator orderCreator
	CDEK         cdekService
	Admin        adminRepository
	Saby         sabySyncService
	// Push is nil when no VAPID keys are configured, which simply means the
	// shop sends no notifications.
	Push pushService
	// Cart is nil in tests that do not exercise the basket; the routes then
	// behave as they do for a guest.
	Cart cartStore
	// Packages supplies box dimensions for delivery quotes; nil simply
	// means every item is quoted at the fallback box size.
	Packages packageRepository
	// Collections are the hand-made sets shown as tabs on the storefront.
	Collections  collectionRepository
	// Payments is nil when no YooKassa keys are set, which means the shop
	// simply does not offer card payment.
	Payments paymentService
	// Settings is nil in tests; the routes then answer 503 and nothing
	// else in the shop notices.
	Settings settingsService
	// Refunds sends money back for a cancelled order; nil means the panel
	// says refunds are unavailable.
	Refunds      refundService
	CookieSecure bool
	StaticDir    string
	// YandexSuggestKey enables address autocomplete; empty simply turns the
	// suggestions off and leaves the address field as plain text.
	YandexSuggestKey string
}

func NewRouter(logger *slog.Logger, dependencies Dependencies) http.Handler {
	mux := http.NewServeMux()
	authAPI := authHandlers{
		logger:       logger,
		service:      dependencies.Auth,
		cookieSecure: dependencies.CookieSecure,
	}
	cdekAPI := cdekHandlers{
		logger:   logger,
		service:  dependencies.CDEK,
		packages: dependencies.Packages,
	}
	adminAPI := newAdminHandlers(
		logger,
		dependencies.Auth,
		dependencies.Admin,
		dependencies.Refunds,
	)

	// Nothing behind these three routes is free for us: a call costs money
	// at SMS.ru, an order pings a person, and every address suggestion is
	// billed by Yandex against our key. Without a ceiling a single script
	// could drain all three.
	callLimiter := newRateLimiter(5, 10*time.Minute)
	orderLimiter := newRateLimiter(10, time.Hour)
	suggestLimiter := newRateLimiter(60, time.Minute)
	mux.HandleFunc("GET /api/v1/health", func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.Handle("GET /api/v1/catalog", catalogHandler(logger, dependencies.Catalog))
	mux.Handle("GET /api/v1/categories", categoriesHandler(logger, dependencies.Catalog))
	mux.Handle("GET /api/v1/collections", collectionsHandler(logger, dependencies.Collections))
	mux.Handle("GET /api/v1/products/{slug}", productDetailHandler(logger, dependencies.Catalog))
	mux.HandleFunc("POST /api/v1/auth/request-code", callLimiter.guard(
		"Слишком много запросов звонка. Попробуйте через несколько минут",
		authAPI.requestCode,
	))
	mux.HandleFunc("POST /api/v1/auth/verify-code", authAPI.verifyCode)
	mux.HandleFunc("POST /api/v1/auth/logout", authAPI.logout)
	mux.HandleFunc("GET /api/v1/auth/me", authAPI.me)
	mux.Handle(
		"GET /api/v1/account/orders",
		accountOrdersHandler(logger, dependencies.Auth, dependencies.Orders),
	)
	if dependencies.Cart != nil {
		cartAPI := cartHandler(logger, dependencies.Auth, dependencies.Cart)
		mux.Handle("GET /api/v1/account/cart", cartAPI)
		mux.Handle("PUT /api/v1/account/cart", cartAPI)
	}
	mux.HandleFunc("PUT /api/v1/account/profile", authAPI.updateProfile)
	mux.HandleFunc("PUT /api/v1/account/avatar", authAPI.uploadAvatar)
	mux.HandleFunc("DELETE /api/v1/account/avatar", authAPI.deleteAvatar)
	mux.HandleFunc("GET /api/v1/account/avatar", authAPI.avatar)
	mux.Handle(
		"GET /api/v1/account/orders/{orderNumber}",
		accountOrderHandler(logger, dependencies.Auth, dependencies.Orders),
	)
	mux.HandleFunc("GET /api/v1/address/suggest", suggestLimiter.guard(
		"Слишком много запросов. Введите адрес вручную",
		addressSuggestHandler(logger, dependencies.YandexSuggestKey).ServeHTTP,
	))
	mux.Handle("GET /api/v1/push/key", pushKeyHandler(dependencies.Push))
	mux.Handle(
		"POST /api/v1/push/subscribe",
		pushSubscribeHandler(logger, dependencies.Auth, dependencies.Push),
	)
	mux.Handle("POST /api/v1/push/unsubscribe", pushUnsubscribeHandler(logger, dependencies.Push))
	mux.Handle(
		"GET /api/v1/payments/methods",
		paymentMethodsHandler(dependencies.Auth, dependencies.Payments),
	)
	mux.Handle(
		"POST /api/v1/payments/orders/{orderNumber}",
		startPaymentHandler(logger, dependencies.Payments),
	)
	mux.Handle(
		"POST /api/v1/payments/yookassa/webhook",
		yooKassaWebhookHandler(logger, dependencies.Payments),
	)
	mux.HandleFunc("GET /api/v1/delivery/cdek", cdekAPI.get)
	mux.HandleFunc("POST /api/v1/delivery/cdek", cdekAPI.calculate)
	mux.HandleFunc("POST /api/v1/orders", orderLimiter.guard(
		"Слишком много заказов подряд. Позвоните нам, если это ошибка",
		createOrderHandler(
			logger,
			dependencies.Auth,
			dependencies.OrderCreator,
			dependencies.Payments,
		).ServeHTTP,
	))
	settingsAPI := settingsHandlers{
		logger:   logger,
		auth:     dependencies.Auth,
		settings: dependencies.Settings,
	}
	mux.HandleFunc("GET /api/v1/admin/settings", settingsAPI.get)
	mux.HandleFunc("PUT /api/v1/admin/settings", settingsAPI.update)
	mux.HandleFunc("GET /api/v1/admin/dashboard", adminAPI.dashboard)
	mux.HandleFunc("GET /api/v1/admin/customers", adminAPI.customers)
	mux.HandleFunc("PATCH /api/v1/admin/customers/{id}", adminAPI.updateCustomer)
	mux.HandleFunc("GET /api/v1/admin/orders", adminAPI.orders)
	mux.HandleFunc("PATCH /api/v1/admin/orders/{id}", adminAPI.updateOrder)
	mux.HandleFunc("GET /api/v1/admin/products", adminAPI.products)
	mux.HandleFunc("PATCH /api/v1/admin/products/{id}", adminAPI.updateProduct)
	mux.HandleFunc("POST /api/v1/admin/products/sync", adminAPI.syncProducts)
	mux.HandleFunc("GET /api/v1/admin/collections", adminAPI.collections)
	mux.HandleFunc("PATCH /api/v1/admin/collections/{id}", adminAPI.updateCollection)
	mux.HandleFunc("GET /api/v1/admin/categories", adminAPI.categories)
	mux.HandleFunc("POST /api/v1/admin/categories", adminAPI.createCategory)
	mux.HandleFunc("PATCH /api/v1/admin/categories/{id}", adminAPI.updateCategory)
	mux.HandleFunc("DELETE /api/v1/admin/categories/{id}", adminAPI.deleteCategory)
	mux.Handle(
		"POST /api/v1/integrations/saby/catalog",
		sabyCatalogSyncHandler(logger, dependencies.Saby),
	)

	var handler http.Handler = mux
	if dependencies.StaticDir != "" {
		handler = spaFallback(
			logger, mux, dependencies.StaticDir,
			sitemapHandler(logger, dependencies.Catalog), dependencies.Catalog,
		)
	}

	return requestLogger(
		logger,
		securityHeaders(dependencies.CookieSecure, recoverPanics(logger, handler)),
	)
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
