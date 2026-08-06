package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
	"github.com/avpavlo8/ficusin-store/backend/internal/cart"
	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
	"github.com/avpavlo8/ficusin-store/backend/internal/config"
	"github.com/avpavlo8/ficusin-store/backend/internal/httpapi"
	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/avpavlo8/ficusin-store/backend/internal/mail"
	"github.com/avpavlo8/ficusin-store/backend/internal/migrate"
	"github.com/avpavlo8/ficusin-store/backend/internal/notify"
	"github.com/avpavlo8/ficusin-store/backend/internal/order"
	"github.com/avpavlo8/ficusin-store/backend/internal/payment"
	"github.com/avpavlo8/ficusin-store/backend/internal/saby"
	"github.com/avpavlo8/ficusin-store/backend/internal/settings"
	"github.com/avpavlo8/ficusin-store/backend/internal/store"
)

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
	telegramClient, err := integration.NewTelegramClient(cfg.TelegramChatID, cfg.TelegramBotToken)
	if err != nil {
		logger.Error("Telegram configuration failed", "error", err)
		os.Exit(1)
	}
	orderService := order.NewService(pool, cdekClient, telegramClient, logger)
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
	shopSettings := settings.NewService(pool, logger)
	adminRepository := admin.NewPostgresRepository(pool).WithNotifier(pushService)
	// Payments stay nil-safe: without YooKassa keys the shop simply does not
	// offer card payment, exactly like CDEK without its own keys.
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
	server := &http.Server{
		Addr: cfg.HTTP.Address,
		Handler: httpapi.NewRouter(logger, httpapi.Dependencies{
			Catalog:      catalogRepository,
			Auth:         authService,
			Orders:       orderRepository,
			OrderCreator: orderService,
			CDEK:         cdekClient,
			Admin:        adminRepository,
			Saby:         sabyService,
			Push:         pushService,
			Cart:         cart.NewStore(pool),
			Packages:     catalogRepository,
			Payments:     paymentService,
			Settings:     shopSettings,
			Refunds:      paymentService,
			CookieSecure: cfg.Auth.CookieSecure,
			StaticDir:    cfg.HTTP.StaticDir,

			YandexSuggestKey: cfg.YandexSuggestKey,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go shopSettings.Run(ctx)
	// An unpaid order holds its plants in reserve; this puts them back.
	go order.NewExpiryWorker(pool, shopSettings, logger).Run(ctx)
	// Parcels are handed to CDEK only when the panel switch is on, so test
	// orders do not turn into real shipments.
	go order.NewShippingWorker(pool, cdekClient, shopSettings, pushService, logger).Run(ctx)
	// Letters go out from a queue: a slow mail server must never hold up an
	// order or a status change.
	go order.NewLetterWorker(pool, mail.NewSender(mail.Config{
		Host:     cfg.Mail.Host,
		Port:     cfg.Mail.Port,
		Username: cfg.Mail.Username,
		Password: cfg.Mail.Password,
		From:     cfg.Mail.From,
		FromName: cfg.Mail.FromName,
	}, logger), logger).Run(ctx)
	go notificationWorker.Run(ctx)
	// The safety net under YooKassa's notifications: a lost one would
	// otherwise leave a paid order looking unpaid.
	go payment.NewReconcileWorker(paymentService, logger).Run(ctx)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", "error", err)
		}
	}()

	logger.Info("api started", "address", cfg.HTTP.Address)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("api stopped unexpectedly", "error", err)
		os.Exit(1)
	}
}
