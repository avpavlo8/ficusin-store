package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
	"github.com/avpavlo8/ficusin-store/backend/internal/cart"
	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
	"github.com/avpavlo8/ficusin-store/backend/internal/catalogai"
	"github.com/avpavlo8/ficusin-store/backend/internal/catalogenrichment"
	"github.com/avpavlo8/ficusin-store/backend/internal/config"
	"github.com/avpavlo8/ficusin-store/backend/internal/httpapi"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/avpavlo8/ficusin-store/backend/internal/mail"
	"github.com/avpavlo8/ficusin-store/backend/internal/migrate"
	"github.com/avpavlo8/ficusin-store/backend/internal/notify"
	"github.com/avpavlo8/ficusin-store/backend/internal/order"
	"github.com/avpavlo8/ficusin-store/backend/internal/payment"
	"github.com/avpavlo8/ficusin-store/backend/internal/photos"
	"github.com/avpavlo8/ficusin-store/backend/internal/procurement"
	"github.com/avpavlo8/ficusin-store/backend/internal/reviews"
	"github.com/avpavlo8/ficusin-store/backend/internal/saby"
	"github.com/avpavlo8/ficusin-store/backend/internal/settings"
	"github.com/avpavlo8/ficusin-store/backend/internal/store"
)

// switchHandler lets the container bind its HTTP port before PostgreSQL
// migrations and integration bootstrap finish. Timeweb uses the Docker
// healthcheck as a liveness gate; the real external production smoke still
// waits for the final router, whose health response is {"status":"ok"}.
//
// During bootstrap only /api/v1/health is 200. Everything else is explicitly
// unavailable, so no request can accidentally be handled against a half-ready
// database.
type switchHandler struct {
	mu      sync.RWMutex
	handler http.Handler
}

func newSwitchHandler(initial http.Handler) *switchHandler {
	return &switchHandler{handler: initial}
}

func (handler *switchHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	handler.mu.RLock()
	current := handler.handler
	handler.mu.RUnlock()
	current.ServeHTTP(writer, request)
}

func (handler *switchHandler) Swap(next http.Handler) {
	handler.mu.Lock()
	handler.handler = next
	handler.mu.Unlock()
}

func bootstrapHTTPHandler(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	if request.URL.Path == "/api/v1/health" {
		writer.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(writer).Encode(map[string]string{"status": "starting"})
		return
	}
	writer.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(writer).Encode(map[string]string{"error": "Сервис запускается"})
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()

	liveHandler := newSwitchHandler(http.HandlerFunc(bootstrapHTTPHandler))
	server := &http.Server{
		Addr:              cfg.HTTP.Address,
		Handler:           liveHandler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	listener, err := net.Listen("tcp", cfg.HTTP.Address)
	if err != nil {
		logger.Error("bind http listener failed", "error", err)
		os.Exit(1)
	}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("bootstrap health endpoint started", "address", cfg.HTTP.Address)
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
		close(serverErrors)
	}()

	pool, err := store.Open(ctx, cfg.Database)
	if err != nil {
		logger.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := migrate.Apply(ctx, pool, cfg.MigrationsDir); err != nil {
		logger.Error("database migrations failed", "error", err)
		os.Exit(1)
	}

	if err := admin.BootstrapOwners(ctx, pool, logger, cfg.AdminEmails); err != nil {
		logger.Error("administrator bootstrap failed", "error", err)
		os.Exit(1)
	}

	catalogRepository := catalog.NewPostgresRepository(pool)
	callChecker := integration.NewSMSRUClient(cfg.SMS.APIKey)
	authService := auth.NewService(pool, cfg.Auth.SessionDays, callChecker)
	orderRepository := order.NewPostgresRepository(pool)
	cdekClient := integration.NewCDEKClient(cfg.CDEK.ClientID, cfg.CDEK.ClientSecret)
	russianPostClient := integration.NewRussianPostClient(
		cfg.RussianPost.AccessToken,
		cfg.RussianPost.UserAuthKey,
		cfg.RussianPost.FromIndex,
	)
	yandexDeliveryClient := integration.NewYandexDeliveryClient(
		cfg.YandexDelivery.Token,
		cfg.YandexDelivery.GeocoderKey,
		cfg.YandexDelivery.SenderAddress,
		cfg.YandexDelivery.SenderLongitude,
		cfg.YandexDelivery.SenderLatitude,
	)
	telegramClient, err := integration.NewTelegramClient(cfg.TelegramChatID, cfg.TelegramBotToken)
	if err != nil {
		logger.Error("Telegram configuration failed", "error", err)
		os.Exit(1)
	}
	shopSettings := settings.NewService(pool, logger)
	orderService := order.NewService(pool, cdekClient, telegramClient, shopSettings, logger).
		WithDeliveryPricers(russianPostClient, yandexDeliveryClient)
	notificationWorker := order.NewNotificationWorker(pool, telegramClient, logger)
	pushService, err := notify.NewService(
		pool, cfg.Push.PublicKey, cfg.Push.PrivateKey, cfg.Push.Subject, logger,
	)
	if err != nil {
		logger.Error("push notification configuration failed", "error", err)
		os.Exit(1)
	}
	if pushService == nil {
		logger.Info("push notifications are off; set VAPID_PUBLIC_KEY and VAPID_PRIVATE_KEY to enable them")
	}
	if !cdekClient.Configured() {
		logger.Warn("CDEK delivery is off; set CDEK_CLIENT_ID and CDEK_CLIENT_SECRET to enable pick-up points")
	}
	if !russianPostClient.Configured() {
		logger.Warn("Russian Post delivery is off; set RUSSIAN_POST_ACCESS_TOKEN, RUSSIAN_POST_USER_AUTH_KEY and RUSSIAN_POST_FROM_INDEX")
	}
	if !yandexDeliveryClient.Configured() {
		logger.Warn("Yandex Delivery is off; set YANDEX_DELIVERY_TOKEN, YANDEX_GEOCODER_API_KEY and sender point coordinates")
	}
	adminRepository := admin.NewPostgresRepository(pool).WithNotifier(pushService)
	paymentService := payment.NewService(
		pool,
		integration.NewYooKassaClient(
			cfg.Payments.ShopID,
			cfg.Payments.SecretKey,
			cfg.Payments.SendReceipt,
			cfg.Payments.TaxSystem,
			cfg.Payments.VATCode,
		),
		cfg.SiteURL,
		logger,
	)
	sabyService := saby.NewService(pool, saby.NewOIDCVerifier())
	procurementStore := procurement.NewPostgresStore(pool)
	marketplaceExecutor := integration.NewMarketplaceExecutor(
		cfg.Marketplaces.WBToken, cfg.Marketplaces.OzonClientID, cfg.Marketplaces.OzonAPIKey,
	)
	sabyProcurementClient := integration.NewSabyClient(
		cfg.Saby.AppClientID, cfg.Saby.AppSecret, cfg.Saby.SecretKey, cfg.Saby.PointID, cfg.Saby.PriceListID,
	)
	procurementExecutor := integration.NewProcurementExecutor(marketplaceExecutor, sabyProcurementClient)
	procurementService := procurement.NewServiceWithExecutor(procurementStore, procurementExecutor)
	photoStorage := photos.NewStorage(cfg.Photos.Endpoint, cfg.Photos.Region, cfg.Photos.Bucket, cfg.Photos.AccessKey, cfg.Photos.SecretKey)
	catalogAI := catalogai.New(cfg.OpenAI.APIKey,cfg.OpenAI.TextModel)
	catalogEnrichment := catalogenrichment.New(pool,adminRepository,catalogAI,photoStorage,logger)
	catalogEnrichment.Start(context.Background())

	liveHandler.Swap(httpapi.NewRouter(logger, httpapi.Dependencies{
		Catalog:           catalogRepository,
		Auth:              authService,
		Orders:            orderRepository,
		OrderCreator:      orderService,
		CDEK:              cdekClient,
		RussianPost:       russianPostClient,
		YandexDelivery:    yandexDeliveryClient,
		Admin:             adminRepository,
		Saby:              sabyService,
		Push:              pushService,
		Cart:              cart.NewStore(pool),
		Packages:          catalogRepository,
		Collections:       catalogRepository,
		Payments:          paymentService,
		Settings:          shopSettings,
		Procurement:       procurementService,
		Reviews:           reviews.NewStore(pool, photoStorage),
		Refunds:           paymentService,
		ProductPhotos:     photoStorage,
		CookieSecure:      cfg.Auth.CookieSecure,
		StaticDir:         cfg.HTTP.StaticDir,
		YandexSuggestKey: cfg.YandexSuggestKey,
		CatalogAI: catalogAI,
		CatalogEnrichment: catalogEnrichment,
	}))

	go shopSettings.Run(ctx)
	go order.NewExpiryWorker(pool, shopSettings, paymentService, logger).Run(ctx)
	go order.NewLoyaltyWorker(pool, logger).Run(ctx)
	go order.NewShippingWorker(pool, cdekClient, shopSettings, pushService, logger).Run(ctx)
	go order.NewLetterWorker(pool, mail.NewSender(mail.Config{
		Host:     cfg.Mail.Host,
		Port:     cfg.Mail.Port,
		Username: cfg.Mail.Username,
		Password: cfg.Mail.Password,
		From:     cfg.Mail.From,
		FromName: cfg.Mail.FromName,
	}, logger), logger).Run(ctx)
	if photoStorage.Configured() {
		go photos.NewMirror(photos.NewPostgresStore(pool), photoStorage, logger).Run(ctx)
	} else {
		logger.Info("перенос фотографий выключен; задайте S3_BUCKET, S3_ACCESS_KEY и S3_SECRET_KEY")
	}
	go notificationWorker.Run(ctx)
	go procurement.NewActionWorker(procurementStore, procurementExecutor, logger).Run(ctx)
	go procurement.NewSalesWorker(procurementStore, marketplaceExecutor, logger).Run(ctx)
	go payment.NewReconcileWorker(paymentService, logger).Run(ctx)

	logger.Info("api ready", "address", cfg.HTTP.Address)
	select {
	case err := <-serverErrors:
		if err != nil {
			logger.Error("api stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", "error", err)
		}
	}
}
