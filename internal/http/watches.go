package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/namepiq/api/internal/fx"
	"github.com/namepiq/api/internal/opensrs"
	"github.com/namepiq/api/internal/razorpay"
	"github.com/namepiq/api/internal/store"
)

var (
	topWatchedMu    sync.Mutex
	topWatchedCache []store.TopWatched
	topWatchedAt    time.Time
)

func (s *Server) handleWatchCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if u.EmailVerifiedAt == nil {
		writeError(w, http.StatusForbidden, "email_unverified", "verify your email before watching domains")
		return
	}
	var req struct {
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	domainName := opensrs.NormalizeDomain(req.Domain)
	if domainName == "" || !strings.Contains(domainName, ".") {
		writeError(w, http.StatusBadRequest, "invalid_domain", "valid domain required")
		return
	}
	freeN, err := s.store.CountActiveWatchesByTier(r.Context(), u.ID, store.WatchTierFree)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not count watches")
		return
	}
	if freeN >= store.FreeWatchLimit {
		writeError(w, http.StatusPaymentRequired, "free_limit",
			"free watch limit reached (3) — use paid checkout for $1 / 6h or $5 / hour (1 month)")
		return
	}
	if owned, _ := s.store.UserOwnsDomainName(r.Context(), u.ID, domainName); owned {
		writeError(w, http.StatusBadRequest, "already_owned", "you already own this domain")
		return
	}
	if err := s.ensureDomainTaken(r, domainName); err != nil {
		writeWatchLookupError(w, err)
		return
	}
	now := time.Now().UTC()
	expires := now.Add(store.WatchDuration)
	watch, err := s.store.CreateWatch(r.Context(), store.DomainWatch{
		UserID:      u.ID,
		Domain:      domainName,
		Tier:        store.WatchTierFree,
		Status:      store.WatchStatusTaken,
		NextCheckAt: &now,
		ExpiresAt:   &expires,
	})
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "already_watching", "you are already watching this domain")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not create watch")
		return
	}
	uid := u.ID
	_ = s.store.InsertAudit(r.Context(), &uid, clientIP(r), "watch.create_free", map[string]any{"domain": domainName})
	writeJSON(w, http.StatusCreated, map[string]any{"watch": watch})
}

func (s *Server) handleWatchCheckout(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if u.EmailVerifiedAt == nil {
		writeError(w, http.StatusForbidden, "email_unverified", "verify your email before watching domains")
		return
	}
	if s.pay == nil || !s.pay.Configured() {
		writeError(w, http.StatusServiceUnavailable, "razorpay_unavailable", "Razorpay is not configured")
		return
	}
	var req struct {
		Domain   string `json:"domain"`
		Tier     string `json:"tier"`
		Currency string `json:"currency"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	tier := strings.ToLower(strings.TrimSpace(req.Tier))
	var usdMinor int64
	switch tier {
	case store.WatchTierStandard:
		usdMinor = store.StandardPriceMinor
	case store.WatchTierPriority:
		usdMinor = store.PriorityPriceMinor
	default:
		writeError(w, http.StatusBadRequest, "invalid_tier", `tier must be "standard" (~$1) or "priority" (~$5) — each watch lasts 1 month`)
		return
	}
	currency := fx.NormalizeCurrency(req.Currency)
	if currency == "" {
		currency = s.requestCurrency(r)
	}
	if currency == "" {
		currency = "USD"
	}
	price, err := s.watchPriceInCurrency(r.Context(), usdMinor, currency)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_currency", err.Error())
		return
	}
	domainName := opensrs.NormalizeDomain(req.Domain)
	if domainName == "" || !strings.Contains(domainName, ".") {
		writeError(w, http.StatusBadRequest, "invalid_domain", "valid domain required")
		return
	}
	paidN, err := s.store.CountActivePaidWatches(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not count watches")
		return
	}
	if paidN >= store.MaxPaidWatches {
		writeError(w, http.StatusBadRequest, "paid_limit", "paid watch limit reached")
		return
	}
	if owned, _ := s.store.UserOwnsDomainName(r.Context(), u.ID, domainName); owned {
		writeError(w, http.StatusBadRequest, "already_owned", "you already own this domain")
		return
	}
	if err := s.ensureDomainTaken(r, domainName); err != nil {
		writeWatchLookupError(w, err)
		return
	}

	rzOrder, err := s.pay.CreateOrder(r.Context(), razorpay.CreateOrderRequest{
		Amount:   price,
		Currency: currency,
		Receipt:  "watch-" + strings.ReplaceAll(uuid.New().String(), "-", "")[:24],
		Notes: map[string]string{
			"type":     "watch",
			"domain":   domainName,
			"tier":     tier,
			"user_id":  u.ID.String(),
			"currency": currency,
		},
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "razorpay_error", "could not create payment order")
		return
	}
	oid := rzOrder.ID
	watch, err := s.store.CreateWatch(r.Context(), store.DomainWatch{
		UserID:          u.ID,
		Domain:          domainName,
		Tier:            tier,
		Status:          store.WatchStatusPending,
		RazorpayOrderID: &oid,
		PriceMinor:      &price,
		Currency:        &currency,
	})
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "already_watching", "you are already watching this domain")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not create watch")
		return
	}
	uid := u.ID
	_ = s.store.InsertAudit(r.Context(), &uid, clientIP(r), "watch.checkout", map[string]any{
		"domain": domainName, "tier": tier, "razorpay_order_id": oid,
		"amount": price, "currency": currency,
	})
	writeJSON(w, http.StatusCreated, map[string]any{
		"watch":             watch,
		"razorpay_order_id": oid,
		"amount":            price,
		"currency":          currency,
		"key_id":            s.pay.KeyID(),
		"test_mode":         s.pay.IsTestMode(),
		"prefill": map[string]string{
			"email": u.Email,
		},
	})
}

func (s *Server) handleWatchPrices(w http.ResponseWriter, r *http.Request) {
	currency := fx.NormalizeCurrency(r.URL.Query().Get("currency"))
	if currency == "" {
		currency = s.requestCurrency(r)
	}
	if currency == "" {
		currency = "USD"
	}
	std, err1 := s.watchPriceInCurrency(r.Context(), store.StandardPriceMinor, currency)
	pri, err2 := s.watchPriceInCurrency(r.Context(), store.PriorityPriceMinor, currency)
	if err1 != nil || err2 != nil {
		writeError(w, http.StatusBadRequest, "invalid_currency", "unsupported currency")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"currency": currency,
		"standard": map[string]any{"amount_minor": std, "tier": "standard", "interval": "6h"},
		"priority": map[string]any{"amount_minor": pri, "tier": "priority", "interval": "1h"},
		"duration": "1 month",
	})
}

// watchPriceInCurrency converts USD list prices (2-decimal minor) into shopper currency minor units.
func (s *Server) watchPriceInCurrency(ctx context.Context, usdMinor int64, currency string) (int64, error) {
	currency = fx.NormalizeCurrency(currency)
	if currency == "" {
		return 0, errors.New("unsupported currency")
	}
	if currency == "USD" {
		return usdMinor, nil
	}
	rate, _, err := s.rates.Rate(ctx, currency)
	if err != nil {
		return 0, err
	}
	usdMajor := float64(usdMinor) / 100.0
	major := usdMajor * rate
	scale := math.Pow10(fx.Exponent(currency))
	minor := int64(math.Round(major * scale))
	if minor < 1 {
		minor = 1
	}
	return minor, nil
}

func (s *Server) handleWatchVerify(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if s.pay == nil || !s.pay.Configured() {
		writeError(w, http.StatusServiceUnavailable, "razorpay_unavailable", "Razorpay is not configured")
		return
	}
	var req struct {
		RazorpayOrderID   string `json:"razorpay_order_id"`
		RazorpayPaymentID string `json:"razorpay_payment_id"`
		RazorpaySignature string `json:"razorpay_signature"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		req.RazorpayOrderID == "" || req.RazorpayPaymentID == "" || req.RazorpaySignature == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "payment fields required")
		return
	}
	if !s.pay.VerifyPaymentSignature(req.RazorpayOrderID, req.RazorpayPaymentID, req.RazorpaySignature) {
		writeError(w, http.StatusBadRequest, "invalid_signature", "payment signature verification failed")
		return
	}
	existing, err := s.store.GetWatchByRazorpayOrderID(r.Context(), req.RazorpayOrderID)
	if err != nil {
		writeError(w, http.StatusNotFound, "watch_not_found", "watch checkout not found")
		return
	}
	if existing.UserID != u.ID {
		writeError(w, http.StatusForbidden, "forbidden", "watch does not belong to this user")
		return
	}
	wasPending := existing.Status == store.WatchStatusPending
	watch, err := s.store.ActivatePaidWatch(r.Context(), req.RazorpayOrderID, req.RazorpayPaymentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not activate watch")
		return
	}
	uid := u.ID
	_ = s.store.InsertAudit(r.Context(), &uid, clientIP(r), "watch.paid", map[string]any{
		"domain": watch.Domain, "tier": watch.Tier, "razorpay_order_id": req.RazorpayOrderID,
	})
	if wasPending {
		s.sendWatchPurchaseEmail(r.Context(), u, watch)
		s.notifyWatchPurchase(u, watch)
	}
	writeJSON(w, http.StatusOK, map[string]any{"watch": watch})
}

func (s *Server) handleWatchList(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	list, err := s.store.ListWatchesByUser(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not list watches")
		return
	}
	freeN, _ := s.store.CountActiveWatchesByTier(r.Context(), u.ID, store.WatchTierFree)
	remaining := store.FreeWatchLimit - freeN
	if remaining < 0 {
		remaining = 0
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"watches":        list,
		"free_used":      freeN,
		"free_limit":     store.FreeWatchLimit,
		"free_remaining": remaining,
	})
}

func (s *Server) handleWatchDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid watch id")
		return
	}
	if err := s.store.DeleteWatch(r.Context(), u.ID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "watch not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "could not delete watch")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleWatchTop(w http.ResponseWriter, r *http.Request) {
	limit := 10
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	topWatchedMu.Lock()
	if time.Since(topWatchedAt) < 60*time.Second && topWatchedCache != nil {
		cached := topWatchedCache
		topWatchedMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"items": cached})
		return
	}
	topWatchedMu.Unlock()

	items, err := s.store.ListTopWatched(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not load top watches")
		return
	}
	if items == nil {
		items = []store.TopWatched{}
	}
	topWatchedMu.Lock()
	topWatchedCache = items
	topWatchedAt = time.Now()
	topWatchedMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleWatchStatus(w http.ResponseWriter, r *http.Request) {
	domainName := opensrs.NormalizeDomain(r.URL.Query().Get("domain"))
	if domainName == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "domain is required")
		return
	}
	st, err := s.store.GetWatchDomainStatus(r.Context(), domainName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not load status")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) activateWatchPayment(ctx context.Context, orderID, paymentID string) error {
	existing, err := s.store.GetWatchByRazorpayOrderID(ctx, orderID)
	if err != nil {
		return err
	}
	wasPending := existing.Status == store.WatchStatusPending
	watch, err := s.store.ActivatePaidWatch(ctx, orderID, paymentID)
	if err != nil {
		return err
	}
	if wasPending {
		if u, uerr := s.store.GetUserByID(ctx, watch.UserID); uerr == nil {
			s.sendWatchPurchaseEmail(ctx, u, watch)
			s.notifyWatchPurchase(u, watch)
		}
	}
	return nil
}

func (s *Server) notifyWatchPurchase(u store.User, w store.DomainWatch) {
	if s.notify == nil {
		return
	}
	amt := ""
	if w.PriceMinor != nil && w.Currency != nil {
		amt = s.formatMoneyOps(*w.PriceMinor, *w.Currency)
	}
	msg := "💳 <b>Watch payment</b>\n"
	msg += "User: " + u.Email + "\n"
	msg += "Domain: <code>" + w.Domain + "</code>\n"
	msg += "Tier: " + w.Tier
	if amt != "" {
		msg += " · " + amt
	}
	s.notify.Notify(context.Background(), msg)
}

func (s *Server) ensureDomainTaken(r *http.Request, domainName string) error {
	if s.reg == nil || !s.reg.Configured() {
		return errWatchSupplier
	}
	res, err := s.reg.Lookup(r.Context(), domainName)
	if err != nil {
		return err
	}
	if res.Available {
		return errWatchAvailable
	}
	return nil
}

var (
	errWatchAvailable = errors.New("domain_available")
	errWatchSupplier  = errors.New("supplier_unavailable")
)

func writeWatchLookupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errWatchAvailable):
		writeError(w, http.StatusBadRequest, "domain_available", "domain is available — add it to cart instead")
	case errors.Is(err, errWatchSupplier):
		writeError(w, http.StatusServiceUnavailable, "opensrs_unavailable", "OpenSRS is not configured")
	default:
		writeError(w, http.StatusBadGateway, "lookup_failed", "could not check domain availability")
	}
}
