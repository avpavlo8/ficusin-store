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
	Catalog          catalogRepository
	Auth             authService
	Orders           orderRepository
	OrderCreator     orderCreator
	CDEK             cdekService
	RussianPost      deliveryPricer
	YandexDelivery   deliveryPricer
	Admin            adminRepository
	Saby             sabySyncService
	Push             pushService
	Cart             cartStore
	Packages         packageRepository
	Collections      collectionRepository
	Payments         paymentService
	Settings         settingsService
	Procurement      procurementService
	Reviews          reviewStore
	Refunds          refundService
	ProductPhotos    productPhotoStorage
	CookieSecure     bool
	StaticDir        string
	YandexSuggestKey string
}

func NewRouter(logger *slog.Logger, dependencies Dependencies) http.Handler {
	mux := http.NewServeMux()
	authAPI := authHandlers{logger: logger, service: dependencies.Auth, cookieSecure: dependencies.CookieSecure}
	cdekAPI := cdekHandlers{logger: logger, service: dependencies.CDEK, packages: dependencies.Packages}
	deliveryAPI := deliveryQuoteHandlers{
		logger: logger, post: dependencies.RussianPost, courier: dependencies.YandexDelivery,
		packages: dependencies.Packages,
	}
	adminAPI := newAdminHandlers(logger, dependencies.Auth, dependencies.Admin, dependencies.Refunds)
	procurementAPI := newProcurementHandlers(logger, adminAPI, dependencies.Procurement)

	callLimiter := newRateLimiter(5, 10*time.Minute)
	orderLimiter := newRateLimiter(10, time.Hour)
	suggestLimiter := newRateLimiter(60, time.Minute)
	deliveryLimiter := newRateLimiter(60, time.Minute)

	mux.HandleFunc("GET /api/v1/health", func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.Handle("GET /api/v1/catalog", catalogHandler(logger, dependencies.Catalog))
	mux.Handle("GET /api/v1/categories", categoriesHandler(logger, dependencies.Catalog))
	mux.Handle("GET /api/v1/collections", collectionsHandler(logger, dependencies.Collections))
	mux.Handle("GET /api/v1/products/{slug}", productDetailHandler(logger, dependencies.Catalog))
	mux.HandleFunc("POST /api/v1/products/{slug}/reviews", createReviewHandler(logger, dependencies.Auth, dependencies.Reviews))
	mux.HandleFunc("GET /api/v1/review-photos/{id}", reviewPhotoHandler(dependencies.Reviews))
	if dependencies.Reviews != nil {
		mux.HandleFunc("GET /api/v1/account/reviews", accountReviewsHandler(dependencies.Auth, dependencies.Reviews))
		mux.HandleFunc("PATCH /api/v1/account/reviews/{id}", updateAccountReviewHandler(dependencies.Auth, dependencies.Reviews))
	}

	mux.HandleFunc("POST /api/v1/auth/request-code", callLimiter.guard(
		"Слишком много запросов звонка. Попробуйте через несколько минут", authAPI.requestCode,
	))
	mux.HandleFunc("POST /api/v1/auth/verify-code", authAPI.verifyCode)
	mux.HandleFunc("POST /api/v1/auth/logout", authAPI.logout)
	mux.HandleFunc("GET /api/v1/auth/me", authAPI.me)
	mux.Handle("GET /api/v1/account/orders", accountOrdersHandler(logger, dependencies.Auth, dependencies.Orders))
	if dependencies.Cart != nil {
		cartAPI := cartHandler(logger, dependencies.Auth, dependencies.Cart, dependencies.CookieSecure)
		mux.Handle("GET /api/v1/cart", cartAPI)
		mux.Handle("PUT /api/v1/cart", cartAPI)
		mux.Handle("GET /api/v1/account/cart", cartAPI)
		mux.Handle("PUT /api/v1/account/cart", cartAPI)
	}
	mux.HandleFunc("PUT /api/v1/account/profile", authAPI.updateProfile)
	mux.HandleFunc("PUT /api/v1/account/avatar", authAPI.uploadAvatar)
	mux.HandleFunc("DELETE /api/v1/account/avatar", authAPI.deleteAvatar)
	mux.HandleFunc("GET /api/v1/account/avatar", authAPI.avatar)
	mux.Handle("GET /api/v1/account/orders/{orderNumber}", accountOrderHandler(logger, dependencies.Auth, dependencies.Orders))
	mux.HandleFunc("GET /api/v1/address/suggest", suggestLimiter.guard(
		"Слишком много запросов. Введите адрес вручную",
		addressSuggestHandler(logger, dependencies.YandexSuggestKey).ServeHTTP,
	))
	mux.Handle("GET /api/v1/push/key", pushKeyHandler(dependencies.Push))
	mux.Handle("POST /api/v1/push/subscribe", pushSubscribeHandler(logger, dependencies.Auth, dependencies.Push))
	mux.Handle("POST /api/v1/push/unsubscribe", pushUnsubscribeHandler(logger, dependencies.Push))
	mux.Handle("GET /api/v1/payments/methods", paymentMethodsHandler(dependencies.Auth, dependencies.Payments))
	mux.Handle("POST /api/v1/payments/orders/{orderNumber}", startPaymentHandler(logger, dependencies.Payments))
	mux.Handle("POST /api/v1/payments/yookassa/webhook", yooKassaWebhookHandler(logger, dependencies.Payments))

	mux.HandleFunc("GET /api/v1/delivery/cdek", cdekAPI.get)
	mux.HandleFunc("POST /api/v1/delivery/cdek", cdekAPI.calculate)
	mux.HandleFunc("GET /api/v1/delivery/providers", deliveryAPI.providers)
	mux.HandleFunc("POST /api/v1/delivery/post", deliveryLimiter.guard(
		"Слишком много расчётов доставки. Попробуйте через несколько минут", deliveryAPI.postQuote,
	))
	mux.HandleFunc("POST /api/v1/delivery/courier", deliveryLimiter.guard(
		"Слишком много расчётов доставки. Попробуйте через несколько минут", deliveryAPI.courierQuote,
	))
	// Backward compatibility for older clients. The current checkout does not
	// use fixed courier/post prices.
	mux.Handle("GET /api/v1/delivery/fees", deliveryFeesHandler(dependencies.Settings))
	mux.HandleFunc("POST /api/v1/orders", orderLimiter.guard(
		"Слишком много заказов подряд. Позвоните нам, если это ошибка",
		createOrderHandler(logger, dependencies.Auth, dependencies.OrderCreator, dependencies.Payments).ServeHTTP,
	))

	settingsAPI := settingsHandlers{logger: logger, auth: dependencies.Auth, settings: dependencies.Settings}
	mux.HandleFunc("GET /api/v1/admin/settings", settingsAPI.get)
	mux.HandleFunc("PUT /api/v1/admin/settings", settingsAPI.update)
	mux.HandleFunc("GET /api/v1/admin/dashboard", adminAPI.dashboard)
	mux.HandleFunc("GET /api/v1/admin/customers", adminAPI.customers)
	mux.HandleFunc("PATCH /api/v1/admin/customers/{id}", adminAPI.updateCustomer)
	mux.HandleFunc("GET /api/v1/admin/orders", adminAPI.orders)
	mux.HandleFunc("PATCH /api/v1/admin/orders/{id}", adminAPI.updateOrder)
	mux.HandleFunc("GET /api/v1/admin/products", adminAPI.products)
	mux.HandleFunc("POST /api/v1/admin/products", adminAPI.createProduct)
	mux.HandleFunc("POST /api/v1/admin/products/import", adminAPI.importProducts)
	mux.HandleFunc("PATCH /api/v1/admin/products/{id}", safeAdminProductUpdateHandler(adminAPI))
	mux.HandleFunc("POST /api/v1/admin/products/sync", adminAPI.syncProducts)
	registerAdminCatalogToolRoutes(mux, adminAPI, dependencies.ProductPhotos)
	if dependencies.Reviews != nil {
		mux.HandleFunc("GET /api/v1/admin/reviews", pendingReviewsHandler(adminAPI, dependencies.Reviews))
		mux.HandleFunc("GET /api/v1/admin/review-media/{id}", moderationMediaHandler(adminAPI, dependencies.Reviews))
		mux.HandleFunc("PATCH /api/v1/admin/reviews/{id}", moderateReviewHandler(adminAPI, dependencies.Reviews))
	}
	mux.HandleFunc("GET /api/v1/admin/collections", adminAPI.collections)
	mux.HandleFunc("PATCH /api/v1/admin/collections/{id}", adminAPI.updateCollection)
	mux.HandleFunc("GET /api/v1/admin/categories", adminAPI.categories)
	mux.HandleFunc("GET /api/v1/admin/categories/{id}/attributes", adminAPI.categoryAttributes)
	mux.HandleFunc("GET /api/v1/admin/catalog/media-health", adminAPI.catalogMediaHealth)
	mux.HandleFunc("POST /api/v1/admin/categories", adminAPI.createCategory)
	mux.HandleFunc("PATCH /api/v1/admin/categories/{id}", adminAPI.updateCategory)
	mux.HandleFunc("DELETE /api/v1/admin/categories/{id}", adminAPI.deleteCategory)

	mux.HandleFunc("GET /api/v1/admin/procurement", procurementAPI.dashboard)
	mux.HandleFunc("PUT /api/v1/admin/procurement/settings", procurementAPI.updateSettings)
	mux.HandleFunc("POST /api/v1/admin/procurement/suppliers", procurementAPI.createSupplier)
	mux.HandleFunc("DELETE /api/v1/admin/procurement/suppliers/{id}", procurementAPI.deleteSupplier)
	mux.HandleFunc("POST /api/v1/admin/procurement/orders", procurementAPI.createOrder)
	mux.HandleFunc("POST /api/v1/admin/procurement/plans", procurementAPI.createPlan)
	mux.HandleFunc("GET /api/v1/admin/procurement/orders/{id}", procurementAPI.orderDetail)
	mux.HandleFunc("POST /api/v1/admin/procurement/orders/{id}/calculate", procurementAPI.calculateOrder)
	mux.HandleFunc("PATCH /api/v1/admin/procurement/orders/{id}/status", procurementAPI.updateOrderStatus)
	mux.HandleFunc("PATCH /api/v1/admin/procurement/order-lines/{id}", procurementAPI.updateOrderLine)
	mux.HandleFunc("POST /api/v1/admin/procurement/orders/{id}/batches", procurementAPI.prepareBatch)
	mux.HandleFunc("POST /api/v1/admin/procurement/documents", procurementAPI.importDocument)
	mux.HandleFunc("GET /api/v1/admin/procurement/nomenclature", procurementAPI.searchNomenclature)
	mux.HandleFunc("GET /api/v1/admin/procurement/sales/unlinked", procurementAPI.unlinkedSales)
	mux.HandleFunc("GET /api/v1/admin/procurement/sales/nomenclature", procurementAPI.linkableNomenclature)
	mux.HandleFunc("POST /api/v1/admin/procurement/sales/link", procurementAPI.linkSales)
	mux.HandleFunc("PATCH /api/v1/admin/procurement/aliases/{id}", procurementAPI.resolveAlias)
	mux.HandleFunc("PATCH /api/v1/admin/procurement/availability", procurementAPI.updateAvailability)
	mux.HandleFunc("PUT /api/v1/admin/procurement/exclusions", procurementAPI.updateExclusion)
	mux.HandleFunc("POST /api/v1/admin/procurement/requests", procurementAPI.createRequest)
	mux.HandleFunc("PATCH /api/v1/admin/procurement/requests/{id}", procurementAPI.updateRequest)
	mux.HandleFunc("GET /api/v1/admin/procurement/products", procurementAPI.listProducts)
	mux.HandleFunc("PUT /api/v1/admin/procurement/products", procurementAPI.updateProduct)
	mux.HandleFunc("POST /api/v1/admin/procurement/batches/{id}/approve", procurementAPI.approveBatch)
	mux.HandleFunc("POST /api/v1/admin/procurement/batches/{id}/retry", procurementAPI.retryBatch)
	mux.HandleFunc("POST /api/v1/admin/procurement/integrations/{channel}/check", procurementAPI.checkIntegration)
	mux.HandleFunc("POST /api/v1/admin/procurement/integrations/{channel}/catalog", procurementAPI.syncChannelCatalog)
	mux.Handle("POST /api/v1/integrations/saby/catalog", sabyCatalogSyncHandler(logger, dependencies.Saby))
	mux.Handle("POST /api/v1/integrations/saby/sales", sabySalesSyncHandler(logger, dependencies.Saby))

	var handler http.Handler = mux
	if dependencies.StaticDir != "" {
		handler = spaFallback(logger, mux, dependencies.StaticDir,
			sitemapHandler(logger, dependencies.Catalog), dependencies.Catalog)
	}
	return requestLogger(logger,
		securityHeaders(dependencies.CookieSecure, recoverPanics(logger, handler)))
}

func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		startedAt := time.Now()
		next.ServeHTTP(response, request)
		logger.Info("http request", "method", request.Method, "path", request.URL.Path,
			"duration_ms", time.Since(startedAt).Milliseconds())
	})
}

func recoverPanics(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("request panic", "value", recovered)
				writeJSON(response, http.StatusInternalServerError, errorResponse{Error: "Внутренняя ошибка сервера"})
			}
		}()
		next.ServeHTTP(response, request)
	})
}
