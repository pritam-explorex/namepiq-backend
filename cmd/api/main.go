package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/namepiq/api/internal/config"
	"github.com/namepiq/api/internal/domain"
	"github.com/namepiq/api/internal/fx"
	httpapi "github.com/namepiq/api/internal/http"
	"github.com/namepiq/api/internal/mail"
	"github.com/namepiq/api/internal/notify"
	"github.com/namepiq/api/internal/opensrs"
	"github.com/namepiq/api/internal/razorpay"
	"github.com/namepiq/api/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx := context.Background()
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	migDir := os.Getenv("MIGRATIONS_DIR")
	if migDir == "" {
		migDir = findMigrations()
	}
	if err := st.Migrate(ctx, migDir); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Printf("migrations applied from %s", migDir)

	if n, err := st.PromoteAdminsByEmail(ctx, cfg.AdminEmails); err != nil {
		log.Fatalf("promote admins: %v", err)
	} else if n > 0 {
		log.Printf("promoted %d admin(s) from ADMIN_EMAILS", n)
	}

	var mailer mail.Sender = mail.LogSender{}
	if cfg.ResendAPIKey != "" {
		mailer = mail.NewResend(cfg.ResendAPIKey, cfg.ResendFromEmail)
		log.Printf("mailer: resend (%s)", cfg.ResendFromEmail)
	} else {
		log.Printf("mailer: log (set RESEND_API_KEY to send real email)")
	}
	if cfg.E2EMailCapture {
		// Capture only — do not forward to Resend (fake e2e addresses would fail delivery).
		mailer = mail.NewCapture(mail.LogSender{})
		log.Printf("mailer: E2E capture enabled (GET /v1/test/last-email)")
	}

	reg := opensrs.NewWithConcurrency(cfg.OpenSRSUsername, cfg.OpenSRSAPIKey, cfg.OpenSRSEndpoint, cfg.OpenSRSMaxConcurrent)
	if reg.Configured() {
		log.Printf("opensrs: configured (%s, max_concurrent=%d)", cfg.OpenSRSEndpoint, cfg.OpenSRSMaxConcurrent)
	} else {
		log.Printf("opensrs: not configured (set OPENSRS_USERNAME + OPENSRS_API_KEY)")
	}
	rates := fx.NewClient(map[string]float64{
		"INR": cfg.USDINRRate,
		"USD": 1,
	})
	search := domain.NewSearchService(reg, domain.Pricing{
		MarkupPercent:          cfg.RetailMarkupPercent,
		DisplayDiscountPercent: cfg.DisplayDiscountPercent,
		FallbackUSD:            map[string]float64{"INR": cfg.USDINRRate, "USD": 1},
	}, rates)

	pay := razorpay.New(cfg.RazorpayKeyID, cfg.RazorpayKeySecret, cfg.RazorpayWebhookSecret)
	if pay.Configured() {
		mode := "live"
		if pay.IsTestMode() {
			mode = "test"
		}
		log.Printf("razorpay: configured (%s)", mode)
	} else {
		log.Printf("razorpay: not configured (set RAZORPAY_KEY_ID + RAZORPAY_KEY_SECRET)")
	}

	alerter := notify.NewTelegram(cfg.TelegramBotToken, cfg.TelegramChatID)
	if cfg.TelegramBotToken != "" && cfg.TelegramChatID != "" {
		log.Printf("telegram: ops alerts enabled (payments + failures only)")
	} else {
		log.Printf("telegram: not configured (set TELEGRAM_BOT_TOKEN + TELEGRAM_CHAT_ID)")
	}

	srv := httpapi.NewServer(cfg, st, mailer, search, reg, pay, rates, alerter)

	runCtx, runCancel := context.WithCancel(context.Background())
	defer runCancel()
	srv.StartWatchChecker(runCtx)

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("namepiq api listening on %s (%s)", cfg.HTTPAddr, cfg.Env)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	runCancel()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
}

func findMigrations() string {
	candidates := []string{
		"migrations",
		"apps/api/migrations",
		filepath.Join("..", "migrations"),
		filepath.Join("..", "..", "migrations"),
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	return "migrations"
}
