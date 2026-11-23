package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/namepiq/api/internal/config"
	"github.com/namepiq/api/internal/domain"
	"github.com/namepiq/api/internal/fx"
	"github.com/namepiq/api/internal/mail"
	"github.com/namepiq/api/internal/notify"
	"github.com/namepiq/api/internal/opensrs"
	"github.com/namepiq/api/internal/razorpay"
	"github.com/namepiq/api/internal/store"
)

type Server struct {
	cfg         config.Config
	store       store.Repository
	mailer      mail.Sender
	mailCapture *mail.Capture
	notify      notify.Notifier
	search      *domain.SearchService
	reg         *opensrs.Client
	pay         *razorpay.Client
	rates       *fx.Client
	mux         *chi.Mux

	limitSearch   *rateLimiter
	limitCheckout *rateLimiter
	limitRegister *rateLimiter
	limitResend   *rateLimiter
	limitWatch    *rateLimiter
	limitComment  *rateLimiter
}

func NewServer(cfg config.Config, st store.Repository, mailer mail.Sender, search *domain.SearchService, reg *opensrs.Client, pay *razorpay.Client, rates *fx.Client, n notify.Notifier) *Server {
	if mailer == nil {
		mailer = mail.LogSender{}
	}
	if rates == nil {
		rates = fx.NewClient(map[string]float64{"USD": 1, "INR": cfg.USDINRRate})
	}
	if n == nil {
		n = notify.Noop{}
	}
	var capture *mail.Capture
	if cfg.E2EMailCapture {
		if c, ok := mailer.(*mail.Capture); ok {
			capture = c
		} else {
			capture = mail.NewCapture(mailer)
			mailer = capture
		}
	}
	// E2E / stage runs need high burst so suites are not throttled.
	searchEvery, searchBurst := 2*time.Second, 20
	checkoutEvery, checkoutBurst := 12*time.Second, 3
	registerEvery, registerBurst := 5*time.Minute, 5
	resendEvery, resendBurst := 5*time.Minute, 3
	watchEvery, watchBurst := 5*time.Second, 5
	commentEvery, commentBurst := 10*time.Second, 5
	if cfg.E2EMailCapture || cfg.Env == "test" || cfg.Env == "stage" {
		searchEvery, searchBurst = time.Millisecond, 10_000
		checkoutEvery, checkoutBurst = time.Millisecond, 10_000
		registerEvery, registerBurst = time.Millisecond, 10_000
		resendEvery, resendBurst = time.Millisecond, 10_000
		watchEvery, watchBurst = time.Millisecond, 10_000
		commentEvery, commentBurst = time.Millisecond, 10_000
	}
	s := &Server{
		cfg: cfg, store: st, mailer: mailer, mailCapture: capture, notify: n, search: search, reg: reg, pay: pay, rates: rates, mux: chi.NewRouter(),
		limitSearch:   newRateLimiter(searchEvery, searchBurst),
		limitCheckout: newRateLimiter(checkoutEvery, checkoutBurst),
		limitRegister: newRateLimiter(registerEvery, registerBurst),
		limitResend:   newRateLimiter(resendEvery, resendBurst),
		limitWatch:    newRateLimiter(watchEvery, watchBurst),
		limitComment:  newRateLimiter(commentEvery, commentBurst),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	r := s.mux
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	reqTimeout := 60 * time.Second
	if s.cfg.E2EMailCapture || s.cfg.Env == "stage" || s.cfg.Env == "test" {
		reqTimeout = 180 * time.Second // OpenSRS horizon can be slow under concurrency=2
	}
	r.Use(middleware.Timeout(reqTimeout))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   s.cfg.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "Accept-Currency", "Accept-Language"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/healthz", s.handleHealth)

	r.Route("/v1", func(r chi.Router) {
		if s.cfg.E2EMailCapture && s.mailCapture != nil {
			r.Get("/test/last-email", s.handleE2ELastEmail)
			r.Get("/test/emails", s.handleE2EEmails)
		}
		r.Get("/agent", s.handleAgentDiscovery)
		r.Get("/geo", s.handleGeo)
		r.With(s.rateLimit(s.limitSearch, s.rateKeyIP)).Get("/domains/search", s.handleDomainSearch)
		r.Get("/domains/quote", s.handleDomainQuote)
		r.Get("/domains/suggest", s.handleNameSuggest)
		r.Get("/watches/top", s.handleWatchTop)
		r.Get("/watches/status", s.handleWatchStatus)
		r.Get("/watches/prices", s.handleWatchPrices)
		r.Get("/domains/{domain}/comments", s.handleCommentList)
		r.Post("/webhooks/razorpay", s.handleRazorpayWebhook)

		r.Route("/auth", func(r chi.Router) {
			r.With(s.rateLimit(s.limitRegister, s.rateKeyIP)).Post("/register", s.handleRegister)
			r.Post("/login", s.handleLogin)
			r.Post("/logout", s.handleLogout)
			r.Post("/verify-email", s.handleVerifyEmail)
			r.Get("/verify-email", s.handleVerifyEmail)
			r.With(s.requireAuth, s.rateLimit(s.limitResend, s.rateKeyUserOrIP)).Post("/resend-verification", s.handleResendVerification)
		})

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Get("/me", s.handleMe)
			r.Patch("/me", s.handlePatchMe)
			r.Put("/me/contact", s.handleUpsertContact)

			r.With(s.rateLimit(s.limitCheckout, s.rateKeyUserOrIP)).Post("/checkout", s.handleCheckoutCreate)
			r.Post("/checkout/verify", s.handleCheckoutVerify)

			r.With(s.rateLimit(s.limitWatch, s.rateKeyUserOrIP)).Post("/watches", s.handleWatchCreate)
			r.With(s.rateLimit(s.limitWatch, s.rateKeyUserOrIP)).Post("/watches/checkout", s.handleWatchCheckout)
			r.Post("/watches/verify", s.handleWatchVerify)
			r.Get("/watches", s.handleWatchList)
			r.Delete("/watches/{id}", s.handleWatchDelete)

			r.With(s.rateLimit(s.limitComment, s.rateKeyUserOrIP)).Post("/domains/{domain}/comments", s.handleCommentCreate)
			r.Delete("/domains/{domain}/comments/{id}", s.handleCommentDelete)

			r.Get("/domains", s.handleListDomains)
			r.Post("/domains/register", s.handleRegisterDomain)
			r.Route("/domains/{id}", func(r chi.Router) {
				r.Get("/", s.handleGetDomain)
				r.Patch("/", s.handlePatchDomain)
				r.Post("/renew", s.handleRenewDomain)
				r.Put("/contacts", s.handlePushDomainContacts)
				r.Get("/nameservers", s.handleGetNameservers)
				r.Put("/nameservers", s.handlePutNameservers)
				r.Get("/dns", s.handleGetDNS)
				r.Put("/dns", s.handlePutDNS)
			})

			r.Route("/admin", func(r chi.Router) {
				r.Use(s.requireAdmin)
				r.Get("/stats", s.handleAdminStats)
				r.Get("/users", s.handleAdminListUsers)
				r.Patch("/users/{id}", s.handleAdminPatchUser)
				r.Get("/domains", s.handleAdminListDomains)
				r.Get("/orders", s.handleAdminListOrders)
				r.Get("/audit", s.handleAdminListAudit)
			})
		})
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db := "ok"
	if err := s.store.Ping(ctx); err != nil {
		db = "error"
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "degraded",
			"db":     db,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "namepiq-api",
		"db":      db,
	})
}

func (s *Server) handleDomainSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "query parameter q is required")
		return
	}
	currency := s.requestCurrency(r)
	results := s.search.Search(r.Context(), q, currency)
	domains := make([]string, 0, len(results))
	for _, res := range results {
		domains = append(domains, res.Domain)
	}
	counts, _ := s.store.CountWatchersByDomain(r.Context(), domains)
	enriched := make([]map[string]any, 0, len(results))
	for _, res := range results {
		item := map[string]any{
			"domain":            res.Domain,
			"available":         res.Available,
			"wholesale_usd":     res.WholesaleUSD,
			"amount_minor":      res.AmountMinor,
			"compare_at_minor":  res.CompareAtMinor,
			"discount_percent":  res.DiscountPercent,
			"price_paise":       res.PricePaise,
			"currency":          res.Currency,
			"premium":           res.Premium,
			"provider_status":   res.ProviderStatus,
			"message":           res.Message,
			"fx_source":         res.FXSource,
			"watch_count":       counts[res.Domain],
		}
		enriched = append(enriched, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"query":    q,
		"currency": currency,
		"results":  enriched,
	})
}

func (s *Server) handleDomainQuote(w http.ResponseWriter, r *http.Request) {
	domainName := r.URL.Query().Get("domain")
	if domainName == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "domain is required")
		return
	}
	years := 1
	if y := r.URL.Query().Get("years"); y != "" {
		if n, err := strconv.Atoi(y); err == nil {
			years = n
		}
	}
	currency := s.requestCurrency(r)
	q := s.search.Quote(r.Context(), domainName, years, currency)
	writeJSON(w, http.StatusOK, map[string]any{
		"domain":   domainName,
		"years":    years,
		"currency": currency,
		"quote":    q,
	})
}

func (s *Server) requestCurrency(r *http.Request) string {
	return domain.CurrencyFromRequest(
		r.URL.Query().Get("currency"),
		r.Header.Get("Accept-Currency"),
		r.Header.Get("Accept-Language"),
		r.Header.Get("CF-IPCountry"),
	)
}
