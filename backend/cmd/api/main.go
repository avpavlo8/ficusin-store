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
	commerceanalytics "github.com/avpavlo8/ficusin-store/backend/internal/analytics"
	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
	"github.com/avpavlo8/ficusin-store/backend/internal/avito"
	"github.com/avpavlo8/ficusin-store/backend/internal/cart"
	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
	"github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	"github.com/avpavlo8/ficusin-store/backend/internal/chat"
	"github.com/avpavlo8/ficusin-store/backend/internal/config"
	"github.com/avpavlo8/ficusin-store/backend/internal/fileassets"
	"github.com/avpavlo8/ficusin-store/backend/internal/httpapi"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/avpavlo8/ficusin-store/backend/internal/mail"
	"github.com/avpavlo8/ficusin-store/backend/internal/migrate"
	"github.com/avpavlo8/ficusin-store/backend/internal/notify"
	"github.com/avpavlo8/ficusin-store/backend/internal/operations"
	"github.com/avpavlo8/ficusin-store/backend/internal/order"
	"github.com/avpavlo8/ficusin-store/backend/internal/orderworkflow"
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

	// Keep liveness available during database setup; readiness is served only after the router swap.
	liveHandler := newSwitchHandler(http.HandlerFunc(bootstrapHTTPHandler))
	server := &http.Server{
		Addr:              cfg.HTTP.Address,
		Handler:           httpapi.EnvironmentGuard(liveHandler, cfg.Environment, cfg.StagingAccessToken),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
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
	if cfg.Environment == "staging" {
		var databaseName string
		if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&databaseName); err != nil || cfg.ValidateDatabaseName(databaseName) != nil {
			logger.Error("staging database isolation check failed")
			os.Exit(1)
		}
	}

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
	var stagingPublicFiles *fileassets.StagingPublicStorage
	var privateStorage fileassets.Storage = photos.NewPrivateStorage(cfg.PrivatePhotos.Endpoint, cfg.PrivatePhotos.Region, cfg.PrivatePhotos.Bucket, cfg.PrivatePhotos.AccessKey, cfg.PrivatePhotos.SecretKey)
	if cfg.LocalPrivateFilesDir != "" {
		localFiles, localErr := fileassets.NewLocalPrivateStorage(cfg.LocalPrivateFilesDir)
		if localErr != nil {
			logger.Error("local private file emulator unavailable", "error", localErr)
			os.Exit(1)
		}
		defer localFiles.Close()
		privateStorage = localFiles
		logger.Warn("development private-file emulator enabled; files stay local and are not uploaded to S3")
	}
	if cfg.FileStorageMode == "mock" {
		stageFiles, fileErr := fileassets.NewStagingPrivateStorage(ctx, pool)
		if fileErr != nil {
			logger.Error("staging private file mock unavailable", "error", fileErr)
			os.Exit(1)
		}
		privateStorage = stageFiles
		stagingPublicFiles, fileErr = fileassets.NewStagingPublicStorage(ctx, pool, cfg.SiteURL)
		if fileErr != nil {
			logger.Error("staging public image mock unavailable", "error", fileErr)
			os.Exit(1)
		}
		logger.Warn("explicit staging private-file mock enabled; test objects stay in the isolated staging database, external S3 is not contacted")
	}
	authService := auth.NewService(pool, cfg.Auth.SessionDays, callChecker).WithFileStorage(privateStorage)
	orderRepository := order.NewPostgresRepository(pool)
	cdekClient := integration.NewCDEKClient(cfg.CDEK.ClientID, cfg.CDEK.ClientSecret)
	telegramClient, err := newTelegramNotifications(cfg)
	if err != nil {
		logger.Error("Telegram configuration failed", "error", err)
		os.Exit(1)
	}
	shopSettings := settings.NewService(pool, logger)
	orderService := order.NewService(pool, cdekClient, telegramClient, shopSettings, logger)
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
	if pushService != nil {
		go order.NewStaffOrderPushWorker(pool, pushService, logger).Run(ctx)
	}
	if !cdekClient.Configured() {
		logger.Warn("CDEK delivery is off; set CDEK_CLIENT_ID and CDEK_CLIENT_SECRET to enable pick-up points")
	}
	shippingWorker := order.NewShippingWorker(pool, cdekClient, shopSettings, pushService, logger)
	orderService.WithShipmentWake(shippingWorker.Wake)
	adminRepository := admin.NewPostgresRepository(pool).WithFileStorage(privateStorage).WithNotifier(pushService).WithShipmentQuotes(cdekClient).WithPriceChannelsConfigured(map[string]bool{
		"wb":    cfg.CatalogMarketplaces.WBToken != "",
		"ozon":  cfg.CatalogMarketplaces.OzonClientID != "" && cfg.CatalogMarketplaces.OzonAPIKey != "",
		"saby":  cfg.Saby.AppClientID != "" && cfg.Saby.AppSecret != "" && cfg.Saby.SecretKey != "",
		"avito": cfg.Marketplaces.AvitoClientID != "" && cfg.Marketplaces.AvitoClientSecret != "",
	})
	paymentService := payment.NewService(
		pool,
		integration.NewYooKassaClient(
			cfg.Payments.ShopID,
			cfg.Payments.SecretKey,
			cfg.Payments.TaxSystem,
			cfg.Payments.VATCode,
		),
		cfg.SiteURL,
		logger,
	)
	adminRepository.WithOrderPacking(shopSettings, cfg.Mail.Host != "" && cfg.Mail.From != "", paymentService.Configured(), shippingWorker.Wake)
	sabyService := saby.NewService(pool, saby.NewOIDCVerifier())
	procurementStore := procurement.NewPostgresStore(pool).WithFileStorage(privateStorage)
	marketplaceExecutor := integration.NewMarketplaceExecutor(
		cfg.Marketplaces.WBToken, cfg.Marketplaces.OzonClientID, cfg.Marketplaces.OzonAPIKey,
	).WithWBRequestLimiter(procurementStore).WithIntegrationRequestLimiter(procurementStore)
	sabyProcurementClient := integration.NewSabyClient(
		cfg.Saby.AppClientID, cfg.Saby.AppSecret, cfg.Saby.SecretKey, cfg.Saby.PointID, cfg.Saby.PriceListID,
	).WithIntegrationRequestLimiter(procurementStore)
	adminRepository.WithEditorSabyMetadata(&integration.SabyEditorMetadataSource{Client: sabyProcurementClient, Hierarchy: &integration.SabyEditorHierarchySource{Client: sabyProcurementClient}}, cfg.Environment)
	if cfg.Environment == "staging" {
		stock, err := saby.NewStagingStockRefresher(ctx, pool)
		if err != nil {
			logger.Error("staging stock configuration failed", "error", err)
			os.Exit(1)
		}
		go stock.Run(ctx, logger)
	}
	go catalogeditor.RunPreview(ctx, pool, cfg.Environment, logger)
	adminRepository.WithEditorPublicationMode(cfg.CatalogPublicationMode)
	editorReferences := integration.NewMarketplaceExecutor(cfg.CatalogMarketplaces.WBToken, cfg.CatalogMarketplaces.OzonClientID, cfg.CatalogMarketplaces.OzonAPIKey).WithWBRequestLimiter(procurementStore).WithIntegrationRequestLimiter(procurementStore)
	avitoService := avito.New(pool, cfg.Marketplaces.AvitoClientID, cfg.Marketplaces.AvitoClientSecret, cfg.SiteURL, cfg.Marketplaces.AvitoAddress, cfg.Marketplaces.AvitoManagerName, cfg.Marketplaces.AvitoContactPhone, cfg.Marketplaces.AvitoCategory, cfg.Marketplaces.AvitoGoodsType)
	var editorPublisher catalogeditor.Publisher
	if cfg.CatalogPublicationMode == "mock" {
		mockPublisher, err := integration.NewPersistentMockCatalog(ctx, pool)
		if err != nil {
			logger.Error("local marketplace emulator unavailable", "error", err)
			os.Exit(1)
		}
		editorPublisher = mockPublisher
		editorReferences = mockPublisher.Executor
	}
	if cfg.CatalogPublicationMode == "live" {
		marketplacePublisher := &integration.CatalogPublisher{Executor: editorReferences}
		editorPublisher = catalogeditor.ChannelPublishers{
			"wb": marketplacePublisher, "ozon": marketplacePublisher,
			"saby": &integration.SabyEditorPublisher{Client: sabyProcurementClient},
		}
	}
	if editorPublisher != nil {
		editorPublisher = &integration.EditorPublisher{Cards: editorPublisher, Prices: &integration.EditorPricePublisher{Marketplaces: editorReferences, Saby: sabyProcurementClient, Avito: avitoService, Mock: cfg.CatalogPublicationMode == "mock"}}
		go catalogeditor.RunPublisher(ctx, pool, editorPublisher, cfg.CatalogPublicationMode, logger)
	}

	procurementExecutor := integration.NewProcurementExecutor(marketplaceExecutor, sabyProcurementClient, avitoService).
		WithSabyCatalogSync(sabyService)
	salesExecutor := integration.NewSalesExecutor(marketplaceExecutor, sabyProcurementClient)
	procurementService := procurement.NewServiceWithExecutor(procurementStore, procurementExecutor)
	photoStorage := photos.NewStorage(cfg.Photos.Endpoint, cfg.Photos.Region, cfg.Photos.Bucket, cfg.Photos.AccessKey, cfg.Photos.SecretKey)
	var productPhotoStorage interface {
		Configured() bool
		Put(context.Context, string, []byte, string) error
		PublicURL(string) string
	} = photoStorage
	if stagingPublicFiles != nil {
		productPhotoStorage = stagingPublicFiles
	}
	analyticsStore := commerceanalytics.NewStore(pool)
	metrikaWorker := analyticsStore.WithMetrika(cfg.MetrikaMPToken, cfg.SiteURL, func() string { return shopSettings.All()[settings.MetrikaID] })
	go metrikaWorker.Run(ctx, logger)
	operationsProbe := operations.NewProbe(pool).WithAlerts(telegramClient)
	go avitoService.Run(ctx)
	chatStore := chat.NewStore(pool).WithFileStorage(privateStorage)
	if pushService != nil {
		for range 4 {
			go chatStore.RunPush(ctx, pushService, logger)
		}
	}

	liveHandler.Swap(httpapi.NewRouter(logger, httpapi.Dependencies{
		StagingMedia:        stagingPublicFiles,
		StaticAssetsBaseURL: cfg.StaticAssetsBaseURL,
		Environment:         cfg.Environment,
		EditorReferences:    editorReferences,
		Catalog:             catalogRepository,
		Auth:                authService,
		Orders:              orderRepository,
		OrderCreator:        orderService,
		CDEK:                cdekClient,
		Admin:               adminRepository,
		Saby:                sabyService,
		Push:                pushService,
		Chat:                chatStore,
		Cart:                cart.NewStore(pool),
		Packages:            catalogRepository,
		Collections:         catalogRepository,
		Payments:            paymentService,
		Settings:            shopSettings,
		Procurement:         procurementService,
		Reviews:             reviews.NewStore(pool, privateStorage).WithLegacyMediaStorage(photoStorage),
		Refunds:             paymentService,
		ProductPhotos:       productPhotoStorage,
		CookieSecure:        cfg.Auth.CookieSecure,
		StaticDir:           cfg.HTTP.StaticDir,
		YandexSuggestKey:    cfg.YandexSuggestKey,
		DaDataAPIKey:        cfg.DaDataAPIKey,
		Analytics:           analyticsStore,
		SiteURL:             cfg.SiteURL,
		Readiness:           pool,
		Operations:          operationsProbe,
		Avito:               avitoService,
	}))
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			statistics := pool.Stat()
			logger.Info("postgres pool",
				"max_connections", statistics.MaxConns(),
				"total_connections", statistics.TotalConns(),
				"acquired_connections", statistics.AcquiredConns(),
				"idle_connections", statistics.IdleConns(),
				"empty_acquires", statistics.EmptyAcquireCount(),
				"empty_acquire_wait_ms", float64(statistics.EmptyAcquireWaitTime().Microseconds())/1000,
				"cancelled_acquires", statistics.CanceledAcquireCount(),
			)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	go shopSettings.Run(ctx)
	go order.NewExpiryWorker(pool, shopSettings, paymentService, logger).Run(ctx)
	go order.NewLoyaltyWorker(pool, logger).Run(ctx)
	go shippingWorker.Run(ctx)
	go orderworkflow.Run(ctx, pool, logger)
	go order.NewPackingPushWorker(pool, pushService, logger).Run(ctx)
	go order.NewPackingWorker(pool, paymentService, cfg.SiteURL, logger).WithPaymentsEnabled(func() bool { return shopSettings.Enabled(settings.PaymentsEnabled) }).Run(ctx)
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
	go procurement.NewWBMirrorWorker(procurementStore, marketplaceExecutor, logger).Run(ctx)
	go procurement.NewCatalogWorker(procurementStore, procurementExecutor, logger).Run(ctx)
	go procurement.NewSalesWorker(procurementStore, salesExecutor, logger).Run(ctx)
	go payment.NewReconcileWorker(paymentService, logger).Run(ctx)
	go operationsProbe.Run(ctx, logger)

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
