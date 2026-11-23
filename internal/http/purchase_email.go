package httpapi

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"

	"github.com/namepiq/api/internal/fx"
	"github.com/namepiq/api/internal/store"
)

func formatMoneyMinor(amount int64, currency string) string {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		currency = "USD"
	}
	exp := fx.Exponent(currency)
	scale := math.Pow10(exp)
	major := float64(amount) / scale
	if exp == 0 {
		return fmt.Sprintf("%s %.0f", currency, major)
	}
	return fmt.Sprintf("%s %.*f", currency, exp, major)
}

// formatMoneyOps is for Telegram ops alerts: charged currency + INR equivalent.
func (s *Server) formatMoneyOps(amount int64, currency string) string {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		currency = "USD"
	}
	base := formatMoneyMinor(amount, currency)
	if currency == "INR" {
		return base
	}
	inrMinor, ok := s.approxINRMinor(amount, currency)
	if !ok {
		return base
	}
	return base + " (~" + formatMoneyMinor(inrMinor, "INR") + ")"
}

func (s *Server) approxINRMinor(amount int64, currency string) (int64, bool) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" || currency == "INR" {
		return amount, currency == "INR"
	}
	if s == nil || s.rates == nil {
		return 0, false
	}
	ctx := context.Background()
	fromPerUSD, _, err := s.rates.Rate(ctx, currency)
	if err != nil || fromPerUSD <= 0 {
		return 0, false
	}
	inrPerUSD, _, err := s.rates.Rate(ctx, "INR")
	if err != nil || inrPerUSD <= 0 {
		return 0, false
	}
	major := float64(amount) / math.Pow10(fx.Exponent(currency))
	usdMajor := major
	if currency != "USD" {
		usdMajor = major / fromPerUSD
	}
	inrMajor := usdMajor * inrPerUSD
	return int64(math.Round(inrMajor * 100)), true
}

func (s *Server) sendDomainPurchaseEmail(
	ctx context.Context,
	u store.User,
	orders []store.Order,
	results []map[string]any,
	paymentID string,
) {
	if u.Email == "" || s.mailer == nil {
		return
	}
	dashboard := strings.TrimRight(s.cfg.AppBaseURL, "/") + "/dashboard"
	var total int64
	currency := "USD"
	var linesHTML, linesText strings.Builder
	statusByDomain := map[string]string{}
	for _, r := range results {
		d, _ := r["domain"].(string)
		st, _ := r["status"].(string)
		if d != "" {
			statusByDomain[d] = st
		}
	}
	okCount := 0
	failCount := 0
	for _, o := range orders {
		total += o.PricePaise
		if o.Currency != "" {
			currency = o.Currency
		}
		st := statusByDomain[o.DomainName]
		if st == "" {
			st = o.Status
		}
		label := st
		switch st {
		case "registered":
			label = "Registered"
			okCount++
		case "fulfillment_failed":
			label = "Registration pending / failed — we’ll follow up"
			failCount++
		case "paid":
			label = "Paid — registering"
		}
		amt := formatMoneyMinor(o.PricePaise, o.Currency)
		years := o.Years
		if years < 1 {
			years = 1
		}
		linesHTML.WriteString(fmt.Sprintf(
			`<li style="margin:0 0 8px"><strong>%s</strong> · %d yr · %s · %s</li>`,
			o.DomainName, years, amt, label,
		))
		linesText.WriteString(fmt.Sprintf("- %s (%d yr) %s — %s\n", o.DomainName, years, amt, label))
	}
	totalStr := formatMoneyMinor(total, currency)

	subject := "Your Namepiq domain order"
	if okCount > 0 && failCount == 0 {
		subject = "Domain registered — Namepiq"
	} else if okCount > 0 {
		subject = "Domain order update — Namepiq"
	} else {
		subject = "Payment received — Namepiq"
	}

	text := fmt.Sprintf(
		"Hi,\n\nThanks for your purchase on Namepiq.\n\nPayment ID: %s\nTotal: %s\n\nDomains:\n%s\nManage your domains: %s\n\n— Namepiq\n",
		paymentID, totalStr, linesText.String(), dashboard,
	)
	html := fmt.Sprintf(
		`<div style="font-family:system-ui,sans-serif;line-height:1.5;color:#111">
<p>Hi,</p>
<p>Thanks for your purchase on <strong>Namepiq</strong>.</p>
<p><strong>Payment ID:</strong> %s<br/><strong>Total:</strong> %s</p>
<p><strong>Domains</strong></p>
<ul>%s</ul>
<p><a href="%s">Open your dashboard</a></p>
<p style="color:#666;font-size:13px">If a registration is still pending, our team (or a retry) will complete it — reply to this email if you need help.</p>
<p>— Namepiq</p>
</div>`,
		paymentID, totalStr, linesHTML.String(), dashboard,
	)
	if err := s.mailer.Send(ctx, u.Email, subject, html, text); err != nil {
		log.Printf("purchase email %s: %v", u.Email, err)
	}
}

func (s *Server) sendWatchPurchaseEmail(ctx context.Context, u store.User, w store.DomainWatch) {
	if u.Email == "" || s.mailer == nil {
		return
	}
	link := strings.TrimRight(s.cfg.AppBaseURL, "/") + "/domain/" + w.Domain
	dash := strings.TrimRight(s.cfg.AppBaseURL, "/") + "/dashboard"
	tierLabel := w.Tier
	interval := "daily"
	switch w.Tier {
	case store.WatchTierStandard:
		tierLabel = "Standard"
		interval = "every 6 hours"
	case store.WatchTierPriority:
		tierLabel = "Priority"
		interval = "hourly"
	case store.WatchTierFree:
		tierLabel = "Free"
	}
	amount := ""
	if w.PriceMinor != nil && w.Currency != nil {
		amount = formatMoneyMinor(*w.PriceMinor, *w.Currency)
	}
	expires := ""
	if w.ExpiresAt != nil {
		expires = w.ExpiresAt.Format("2 Jan 2006")
	}
	subject := fmt.Sprintf("Watching %s — Namepiq", w.Domain)
	text := fmt.Sprintf(
		"Hi,\n\nYou're now watching %s on Namepiq (%s · checks %s).\n",
		w.Domain, tierLabel, interval,
	)
	if amount != "" {
		text += fmt.Sprintf("Paid: %s\n", amount)
	}
	if expires != "" {
		text += fmt.Sprintf("Watch ends: %s\n", expires)
	}
	text += fmt.Sprintf("\nWe'll email you if this domain becomes available.\nDiscuss: %s\nDashboard: %s\n\n— Namepiq\n", link, dash)

	html := fmt.Sprintf(
		`<div style="font-family:system-ui,sans-serif;line-height:1.5;color:#111">
<p>Hi,</p>
<p>You're now watching <strong>%s</strong> on Namepiq.</p>
<p><strong>Plan:</strong> %s · checks %s<br/>`,
		w.Domain, tierLabel, interval,
	)
	if amount != "" {
		html += fmt.Sprintf("<strong>Paid:</strong> %s<br/>", amount)
	}
	if expires != "" {
		html += fmt.Sprintf("<strong>Ends:</strong> %s<br/>", expires)
	}
	html += fmt.Sprintf(
		`</p>
<p>We'll email you if this domain becomes available.</p>
<p><a href="%s">Discuss this domain</a> · <a href="%s">Dashboard</a></p>
<p>— Namepiq</p>
</div>`, link, dash)

	if err := s.mailer.Send(ctx, u.Email, subject, html, text); err != nil {
		log.Printf("watch purchase email %s: %v", u.Email, err)
	}
}
