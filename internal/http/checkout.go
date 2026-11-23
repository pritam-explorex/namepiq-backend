package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/namepiq/api/internal/fx"
	"github.com/namepiq/api/internal/opensrs"
	"github.com/namepiq/api/internal/razorpay"
	"github.com/namepiq/api/internal/store"
	"github.com/namepiq/api/internal/validate"
)

type checkoutItemIn struct {
	Domain    string `json:"domain"`
	Years     int    `json:"years"`
	AutoRenew *bool  `json:"auto_renew"`
}

func (s *Server) handleCheckoutCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if s.pay == nil || !s.pay.Configured() {
		writeError(w, http.StatusServiceUnavailable, "razorpay_unavailable", "Razorpay is not configured (set RAZORPAY_KEY_ID + RAZORPAY_KEY_SECRET)")
		return
	}
	if u.EmailVerifiedAt == nil {
		writeError(w, http.StatusForbidden, "email_unverified", "verify your email before checkout")
		return
	}

	var req struct {
		Items           []checkoutItemIn `json:"items"`
		Currency        string           `json:"currency"`
		TOSAccepted     bool             `json:"tos_accepted"`
		AbandonPending  bool             `json:"abandon_pending"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Items) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "items are required")
		return
	}
	if !req.TOSAccepted {
		writeError(w, http.StatusBadRequest, "tos_required", "accept terms to continue")
		return
	}
	if len(req.Items) > validate.MaxCheckoutItems {
		writeError(w, http.StatusBadRequest, "cart_too_large",
			"cart limited to "+strconv.Itoa(validate.MaxCheckoutItems)+" domains")
		return
	}

	pendingSince := time.Now().UTC().Add(-45 * time.Minute)
	if req.AbandonPending {
		_, _ = s.store.AbandonPendingCheckouts(r.Context(), u.ID)
	} else if pending, ok, err := s.store.GetActivePendingCheckout(r.Context(), u.ID, pendingSince); err == nil && ok {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":             "checkout_in_progress",
			"message":           "finish or abandon your open payment before starting another",
			"checkout_id":       pending.CheckoutID,
			"razorpay_order_id": pending.RazorpayOrderID,
			"amount":            pending.Amount,
			"currency":          pending.Currency,
			"created_at":        pending.CreatedAt,
		})
		return
	} else {
		// Stale pending (>45m) — drop so a new checkout can proceed.
		_, _ = s.store.AbandonPendingCheckouts(r.Context(), u.ID)
	}

	c, err := s.store.GetContact(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "contact_required", "save registrant contact on the dashboard before checkout")
		return
	}
	var missing []string
	if strings.TrimSpace(c.FirstName) == "" {
		missing = append(missing, "first_name")
	}
	if strings.TrimSpace(c.Phone) == "" {
		missing = append(missing, "phone")
	}
	if strings.TrimSpace(c.Address1) == "" {
		missing = append(missing, "address1")
	}
	if strings.TrimSpace(c.City) == "" {
		missing = append(missing, "city")
	}
	if strings.TrimSpace(c.Country) == "" {
		missing = append(missing, "country")
	}
	if len(missing) > 0 {
		writeError(w, http.StatusBadRequest, "contact_required",
			"complete and Save registrant contact on the dashboard (missing: "+strings.Join(missing, ", ")+")")
		return
	}
	if _, err := validate.NormalizeCountry(c.Country); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_contact", err.Error())
		return
	}
	if _, err := validate.NormalizePhoneToE164(c.Phone, c.Country); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_contact", "phone: "+err.Error())
		return
	}
	if err := validate.ValidatePostal(c.PostalCode, c.Country); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_contact", err.Error())
		return
	}

	currency := fx.NormalizeCurrency(req.Currency)
	if currency == "" {
		currency = s.requestCurrency(r)
	}

	checkoutID := uuid.New()
	now := time.Now().UTC()
	var total int64
	rows := make([]store.NewOrder, 0, len(req.Items))
	lineMeta := make([]map[string]any, 0, len(req.Items))

	seen := map[string]bool{}
	for _, item := range req.Items {
		domainName := opensrs.NormalizeDomain(item.Domain)
		if domainName == "" || !strings.Contains(domainName, ".") {
			writeError(w, http.StatusBadRequest, "invalid_domain", "invalid domain: "+item.Domain)
			return
		}
		if seen[domainName] {
			writeError(w, http.StatusBadRequest, "duplicate_domain", "duplicate domain in cart: "+domainName)
			return
		}
		seen[domainName] = true
		years := item.Years
		if years < 1 {
			years = 1
		}
		autoRenew := true
		if item.AutoRenew != nil {
			autoRenew = *item.AutoRenew
		}

		q := s.search.Quote(r.Context(), domainName, years, currency)
		if q.Available == nil || !*q.Available {
			msg := q.Message
			if msg == "" {
				msg = "domain not available"
			}
			writeError(w, http.StatusConflict, "domain_unavailable", domainName+": "+msg)
			return
		}
		if q.AmountMinor == nil || *q.AmountMinor < 1 {
			writeError(w, http.StatusConflict, "price_unavailable", domainName+": price unavailable")
			return
		}
		amount := *q.AmountMinor
		total += amount
		rows = append(rows, store.NewOrder{
			UserID:        u.ID,
			CheckoutID:    checkoutID,
			DomainName:    domainName,
			Type:          "register",
			Years:         years,
			PricePaise:    amount,
			MaxPricePaise: amount,
			Currency:      currency,
			AutoRenew:     autoRenew,
			TOSAcceptedAt: &now,
		})
		lineMeta = append(lineMeta, map[string]any{
			"domain": domainName, "years": years, "amount_minor": amount, "auto_renew": autoRenew,
		})
	}
	if total < 1 {
		writeError(w, http.StatusBadRequest, "invalid_amount", "cart total must be > 0")
		return
	}
	if err := validate.CheckCartLimits(len(req.Items), total, currency); err != nil {
		writeError(w, http.StatusBadRequest, "cart_limit", err.Error())
		return
	}

	receipt := strings.ReplaceAll(checkoutID.String(), "-", "")
	if len(receipt) > 40 {
		receipt = receipt[:40]
	}
	rzOrder, err := s.pay.CreateOrder(r.Context(), razorpay.CreateOrderRequest{
		Amount:   total,
		Currency: currency,
		Receipt:  receipt,
		Notes: map[string]string{
			"checkout_id": checkoutID.String(),
			"user_id":     u.ID.String(),
		},
	})
	if err != nil {
		log.Printf("razorpay create order: %v", err)
		writeError(w, http.StatusBadGateway, "razorpay_error", "could not create payment order")
		return
	}

	orders, err := s.store.CreateOrders(r.Context(), rows, rzOrder.ID)
	if err != nil {
		log.Printf("store create orders: %v", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "could not save checkout")
		return
	}

	uid := u.ID
	_ = s.store.InsertAudit(r.Context(), &uid, clientIP(r), "checkout.create", map[string]any{
		"checkout_id":       checkoutID.String(),
		"razorpay_order_id": rzOrder.ID,
		"amount":            total,
		"currency":          currency,
		"items":             lineMeta,
		"test_mode":         s.pay.IsTestMode(),
	})

	writeJSON(w, http.StatusCreated, map[string]any{
		"checkout_id":       checkoutID,
		"razorpay_order_id": rzOrder.ID,
		"amount":            total,
		"currency":          currency,
		"key_id":            s.pay.KeyID(),
		"test_mode":         s.pay.IsTestMode(),
		"orders":            orders,
		"prefill": map[string]string{
			"email": u.Email,
			"name":  strings.TrimSpace(c.FirstName + " " + c.LastName),
			"contact": c.Phone,
		},
	})
}

func (s *Server) handleCheckoutVerify(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusBadRequest, "invalid_request", "razorpay_order_id, razorpay_payment_id, razorpay_signature required")
		return
	}
	if !s.pay.VerifyPaymentSignature(req.RazorpayOrderID, req.RazorpayPaymentID, req.RazorpaySignature) {
		writeError(w, http.StatusBadRequest, "invalid_signature", "payment signature verification failed")
		return
	}

	orders, err := s.store.ListOrdersByRazorpayOrderID(r.Context(), req.RazorpayOrderID)
	if err != nil || len(orders) == 0 {
		writeError(w, http.StatusNotFound, "order_not_found", "checkout not found")
		return
	}
	for _, o := range orders {
		if o.UserID != u.ID {
			writeError(w, http.StatusForbidden, "forbidden", "checkout does not belong to this user")
			return
		}
	}

	results, err := s.fulfillPaidCheckout(r, orders, req.RazorpayOrderID, req.RazorpayPaymentID)
	if err != nil {
		if s.notify != nil {
			s.notify.Notify(context.Background(), "⚠️ <b>Checkout fulfill error</b>\nOrder: <code>"+req.RazorpayOrderID+"</code>\n"+err.Error())
		}
		writeError(w, http.StatusInternalServerError, "fulfillment_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"results": results,
	})
}

type razorpayWebhookEvent struct {
	Event   string `json:"event"`
	Payload struct {
		Payment struct {
			Entity struct {
				ID       string          `json:"id"`
				OrderID  string          `json:"order_id"`
				Status   string          `json:"status"`
				Amount   int64           `json:"amount"`
				Currency string          `json:"currency"`
				Captured bool            `json:"captured"`
				Email    string          `json:"email"`
				// Notes can be object or [] in Razorpay payloads.
				Notes json.RawMessage `json:"notes"`
			} `json:"entity"`
		} `json:"payment"`
		Refund struct {
			Entity struct {
				ID        string `json:"id"`
				PaymentID string `json:"payment_id"`
				Amount    int64  `json:"amount"`
				Currency  string `json:"currency"`
				Status    string `json:"status"`
			} `json:"entity"`
		} `json:"refund"`
		Dispute struct {
			Entity struct {
				ID         string `json:"id"`
				PaymentID  string `json:"payment_id"`
				Amount     int64  `json:"amount"`
				Currency   string `json:"currency"`
				ReasonCode string `json:"reason_code"`
				Status     string `json:"status"`
				Phase      string `json:"phase"`
			} `json:"entity"`
		} `json:"dispute"`
	} `json:"payload"`
}

func razorpayNote(notes json.RawMessage, key string) string {
	if len(notes) == 0 || string(notes) == "null" || string(notes) == "[]" {
		return ""
	}
	var m map[string]string
	if err := json.Unmarshal(notes, &m); err != nil {
		return ""
	}
	return m[key]
}

func (s *Server) handleRazorpayWebhook(w http.ResponseWriter, r *http.Request) {
	if s.pay == nil || !s.pay.Configured() {
		writeError(w, http.StatusServiceUnavailable, "razorpay_unavailable", "Razorpay is not configured")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not read body")
		return
	}
	sig := r.Header.Get("X-Razorpay-Signature")
	if s.cfg.RazorpayWebhookSecret != "" && !s.pay.VerifyWebhookSignature(body, sig) {
		writeError(w, http.StatusUnauthorized, "invalid_signature", "webhook signature mismatch")
		return
	}

	var evt razorpayWebhookEvent
	if err := json.Unmarshal(body, &evt); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid webhook payload")
		return
	}

	switch {
	case evt.Event == "payment.captured" || evt.Event == "order.paid":
		s.handleRazorpayPaidWebhook(w, r, evt)
	case strings.HasPrefix(evt.Event, "refund."):
		s.handleRazorpayRefundWebhook(w, r, evt)
	case strings.HasPrefix(evt.Event, "payment.dispute."):
		s.handleRazorpayDisputeWebhook(w, r, evt)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"ignored": true, "event": evt.Event})
	}
}

func (s *Server) handleRazorpayPaidWebhook(w http.ResponseWriter, r *http.Request, evt razorpayWebhookEvent) {
	orderID := evt.Payload.Payment.Entity.OrderID
	paymentID := evt.Payload.Payment.Entity.ID
	if orderID == "" || paymentID == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ignored": true, "reason": "missing ids"})
		return
	}
	// Paid domain watch
	if razorpayNote(evt.Payload.Payment.Entity.Notes, "type") == "watch" {
		if err := s.activateWatchPayment(r.Context(), orderID, paymentID); err != nil {
			log.Printf("razorpay webhook watch %s: %v", orderID, err)
			writeError(w, http.StatusInternalServerError, "watch_activate_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "type": "watch"})
		return
	}
	if _, err := s.store.GetWatchByRazorpayOrderID(r.Context(), orderID); err == nil {
		if err := s.activateWatchPayment(r.Context(), orderID, paymentID); err != nil {
			log.Printf("razorpay webhook watch %s: %v", orderID, err)
			writeError(w, http.StatusInternalServerError, "watch_activate_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "type": "watch"})
		return
	}
	orders, err := s.store.ListOrdersByRazorpayOrderID(r.Context(), orderID)
	if err != nil || len(orders) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"ignored": true, "reason": "unknown order"})
		return
	}
	if _, err := s.fulfillPaidCheckout(r, orders, orderID, paymentID); err != nil {
		log.Printf("razorpay webhook fulfill %s: %v", orderID, err)
		if s.notify != nil {
			s.notify.Notify(context.Background(), "⚠️ <b>Checkout fulfill error</b>\nOrder: <code>"+orderID+"</code>\n"+err.Error())
		}
		writeError(w, http.StatusInternalServerError, "fulfillment_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleRazorpayRefundWebhook(w http.ResponseWriter, r *http.Request, evt razorpayWebhookEvent) {
	// Skip Razorpay auto-refunds of uncaptured payments — never settled to you.
	if !evt.Payload.Payment.Entity.Captured && evt.Payload.Payment.Entity.ID != "" {
		writeJSON(w, http.StatusOK, map[string]any{"ignored": true, "reason": "uncaptured_auto_refund"})
		return
	}
	refund := evt.Payload.Refund.Entity
	pay := evt.Payload.Payment.Entity
	orderID := pay.OrderID
	paymentID := pay.ID
	if paymentID == "" {
		paymentID = refund.PaymentID
	}
	amount := refund.Amount
	currency := refund.Currency
	if currency == "" {
		currency = pay.Currency
	}
	if amount == 0 {
		amount = pay.Amount
	}

	var title string
	switch evt.Event {
	case "refund.created", "refund.processed":
		title = "💸 <b>Refund</b>"
	case "refund.failed":
		title = "⚠️ <b>Refund failed</b>"
	default:
		writeJSON(w, http.StatusOK, map[string]any{"ignored": true, "event": evt.Event})
		return
	}

	msg := title + "\n"
	msg += "Event: " + evt.Event + "\n"
	if amount > 0 {
		msg += "Amount: " + s.formatMoneyOps(amount, currency) + "\n"
	}
	if paymentID != "" {
		msg += "Payment: <code>" + paymentID + "</code>\n"
	}
	if refund.ID != "" {
		msg += "Refund: <code>" + refund.ID + "</code>\n"
	}
	if refund.Status != "" {
		msg += "Status: " + refund.Status + "\n"
	}
	msg += s.checkoutContextLine(r.Context(), orderID, pay.Email)
	if s.notify != nil {
		s.notify.Notify(context.Background(), msg)
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "event": evt.Event})
}

func (s *Server) handleRazorpayDisputeWebhook(w http.ResponseWriter, r *http.Request, evt razorpayWebhookEvent) {
	d := evt.Payload.Dispute.Entity
	pay := evt.Payload.Payment.Entity
	orderID := pay.OrderID
	paymentID := pay.ID
	if paymentID == "" {
		paymentID = d.PaymentID
	}
	amount := d.Amount
	currency := d.Currency
	if currency == "" {
		currency = pay.Currency
	}
	if amount == 0 {
		amount = pay.Amount
	}

	var title string
	switch evt.Event {
	case "payment.dispute.created":
		title = "⚖️ <b>Chargeback / dispute opened</b>"
	case "payment.dispute.lost":
		title = "❌ <b>Dispute lost</b>"
	case "payment.dispute.won":
		title = "✅ <b>Dispute won</b>"
	case "payment.dispute.action_required":
		title = "⏰ <b>Dispute needs evidence</b>"
	case "payment.dispute.under_review", "payment.dispute.closed":
		title = "⚖️ <b>Dispute update</b>"
	default:
		writeJSON(w, http.StatusOK, map[string]any{"ignored": true, "event": evt.Event})
		return
	}

	msg := title + "\n"
	msg += "Event: " + evt.Event + "\n"
	if amount > 0 {
		msg += "Amount: " + s.formatMoneyOps(amount, currency) + "\n"
	}
	if d.Phase != "" {
		msg += "Phase: " + d.Phase + "\n"
	}
	if d.ReasonCode != "" {
		msg += "Reason: " + d.ReasonCode + "\n"
	}
	if d.Status != "" {
		msg += "Status: " + d.Status + "\n"
	}
	if paymentID != "" {
		msg += "Payment: <code>" + paymentID + "</code>\n"
	}
	if d.ID != "" {
		msg += "Dispute: <code>" + d.ID + "</code>\n"
	}
	msg += s.checkoutContextLine(r.Context(), orderID, pay.Email)
	if s.notify != nil {
		s.notify.Notify(context.Background(), msg)
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "event": evt.Event})
}

// checkoutContextLine resolves user/domains for a Razorpay order (best-effort).
func (s *Server) checkoutContextLine(ctx context.Context, razorpayOrderID, fallbackEmail string) string {
	var parts []string
	if razorpayOrderID != "" {
		if w, err := s.store.GetWatchByRazorpayOrderID(ctx, razorpayOrderID); err == nil {
			parts = append(parts, "Type: watch", "Domain: "+w.Domain)
			if u, err := s.store.GetUserByID(ctx, w.UserID); err == nil && u.Email != "" {
				parts = append(parts, "User: "+u.Email)
			}
		} else if orders, err := s.store.ListOrdersByRazorpayOrderID(ctx, razorpayOrderID); err == nil && len(orders) > 0 {
			var domains []string
			for _, o := range orders {
				domains = append(domains, o.DomainName)
			}
			parts = append(parts, "Type: domain", "Domains: "+strings.Join(domains, ", "))
			if u, err := s.store.GetUserByID(ctx, orders[0].UserID); err == nil && u.Email != "" {
				parts = append(parts, "User: "+u.Email)
			}
		}
		parts = append(parts, "Order: <code>"+razorpayOrderID+"</code>")
	}
	if fallbackEmail != "" && !strings.Contains(strings.Join(parts, " "), "User:") {
		parts = append(parts, "Email: "+fallbackEmail)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}

func (s *Server) fulfillPaidCheckout(r *http.Request, orders []store.Order, razorpayOrderID, paymentID string) ([]map[string]any, error) {
	// Idempotent: mark pending → paid (no-op if already paid/registered).
	paidRows, _ := s.store.MarkOrdersPaid(r.Context(), razorpayOrderID, paymentID)

	fresh, err := s.store.ListOrdersByRazorpayOrderID(r.Context(), razorpayOrderID)
	if err != nil {
		return nil, err
	}
	results := make([]map[string]any, 0, len(fresh))
	newlyRegistered := 0
	var buyer store.User
	haveBuyer := false
	for _, o := range fresh {
		item := map[string]any{
			"order_id": o.ID,
			"domain":   o.DomainName,
			"status":   o.Status,
		}
		if o.Status == "registered" {
			item["ok"] = true
			results = append(results, item)
			continue
		}
		if o.Status != "paid" && o.Status != "fulfillment_failed" && o.Status != "pending_payment" {
			results = append(results, item)
			continue
		}
		// Ensure paid before register attempt.
		if o.Status == "pending_payment" {
			_, _ = s.store.MarkOrdersPaid(r.Context(), razorpayOrderID, paymentID)
		}

		u, err := s.store.GetUserByID(r.Context(), o.UserID)
		if err != nil {
			_ = s.store.UpdateOrderStatus(r.Context(), o.ID, "fulfillment_failed")
			item["ok"] = false
			item["error"] = "user missing"
			item["status"] = "fulfillment_failed"
			results = append(results, item)
			continue
		}
		buyer = u
		haveBuyer = true
		d, regErr := s.registerDomainForUser(r, u, o.DomainName, o.Years, o.AutoRenew)
		if regErr != nil {
			log.Printf("fulfill register %s: %v", o.DomainName, regErr)
			_ = s.store.UpdateOrderStatus(r.Context(), o.ID, "fulfillment_failed")
			item["ok"] = false
			item["error"] = regErr.Error()
			item["status"] = "fulfillment_failed"
			results = append(results, item)
			continue
		}
		_ = s.store.UpdateOrderStatus(r.Context(), o.ID, "registered")
		newlyRegistered++
		item["ok"] = true
		item["status"] = "registered"
		item["domain_record"] = d
		results = append(results, item)
	}

	uid := orders[0].UserID
	_ = s.store.InsertAudit(r.Context(), &uid, clientIP(r), "checkout.paid", map[string]any{
		"razorpay_order_id":   razorpayOrderID,
		"razorpay_payment_id": paymentID,
		"results":             results,
	})

	failCount := 0
	for _, r := range results {
		if st, _ := r["status"].(string); st == "fulfillment_failed" {
			failCount++
		}
	}

	// Receipt / registration email once per payment mark or new registration.
	if paidRows > 0 || newlyRegistered > 0 {
		if !haveBuyer {
			if u, err := s.store.GetUserByID(r.Context(), uid); err == nil {
				buyer = u
				haveBuyer = true
			}
		}
		if haveBuyer {
			s.sendDomainPurchaseEmail(r.Context(), buyer, fresh, results, paymentID)
		}
	}
	// Telegram: successful payments and fulfillment failures only.
	if paidRows > 0 || newlyRegistered > 0 || failCount > 0 {
		if !haveBuyer {
			if u, err := s.store.GetUserByID(r.Context(), uid); err == nil {
				buyer = u
			}
		}
		s.notifyDomainPurchase(buyer, fresh, results, paymentID, paidRows > 0, newlyRegistered)
	}
	return results, nil
}

func (s *Server) notifyDomainPurchase(buyer store.User, orders []store.Order, results []map[string]any, paymentID string, paidNow bool, newlyRegistered int) {
	if s.notify == nil {
		return
	}
	var total int64
	currency := "USD"
	var domains []string
	fail := 0
	var failDetail []string
	for _, o := range orders {
		total += o.PricePaise
		if o.Currency != "" {
			currency = o.Currency
		}
		domains = append(domains, o.DomainName)
	}
	for _, r := range results {
		if st, _ := r["status"].(string); st == "fulfillment_failed" {
			fail++
			dom, _ := r["domain"].(string)
			errMsg, _ := r["error"].(string)
			if dom != "" {
				if errMsg != "" {
					failDetail = append(failDetail, dom+": "+errMsg)
				} else {
					failDetail = append(failDetail, dom)
				}
			}
		}
	}
	email := buyer.Email
	if email == "" {
		email = "?"
	}
	title := "🛒 <b>Payment success</b>"
	if fail > 0 && newlyRegistered == 0 && !paidNow {
		title = "⚠️ <b>Fulfillment failure</b>"
	} else if fail > 0 {
		title = "🛒⚠️ <b>Payment OK — fulfillment issues</b>"
	}
	msg := title + "\n"
	msg += "User: " + email + "\n"
	msg += "Total: " + s.formatMoneyOps(total, currency) + "\n"
	msg += "Payment: <code>" + paymentID + "</code>\n"
	msg += "Domains: " + strings.Join(domains, ", ") + "\n"
	msg += "Registered now: " + strconv.Itoa(newlyRegistered)
	if fail > 0 {
		msg += "\nFailures: " + strconv.Itoa(fail)
		if len(failDetail) > 0 {
			msg += "\n" + strings.Join(failDetail, "\n")
		}
	}
	s.notify.Notify(context.Background(), msg)
}
