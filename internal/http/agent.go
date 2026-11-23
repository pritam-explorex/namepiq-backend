package httpapi

import (
	"net/http"
	"strings"

	"github.com/namepiq/api/internal/fx"
)

// handleAgentDiscovery is a machine-readable capability map for GEO / agents.
func (s *Server) handleAgentDiscovery(w http.ResponseWriter, r *http.Request) {
	app := strings.TrimRight(s.cfg.AppBaseURL, "/")
	api := strings.TrimRight(s.cfg.APIBaseURL, "/")
	if api == "" {
		api = "http://localhost:8080"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":        "Namepiq",
		"description": "Domain registration API and MCP for people and agents",
		"website":     app,
		"api_base":    api,
		"llms_txt":    app + "/llms.txt",
		"llms_full":   app + "/llms-full.txt",
		"docs": map[string]string{
			"overview": app + "/docs/index.md",
			"api":      app + "/docs/api.md",
			"mcp":      app + "/docs/mcp.md",
			"domains":  app + "/docs/domains.md",
		},
		"legal": map[string]string{
			"index":          app + "/legal",
			"terms":          app + "/legal/terms",
			"privacy":        app + "/legal/privacy",
			"refunds":        app + "/legal/refunds",
			"acceptable_use": app + "/legal/acceptable-use",
		},
		"auth": map[string]any{
			"type":     "bearer_jwt",
			"login":    "/v1/auth/login",
			"register": "/v1/auth/register",
		},
		"currencies": fx.Supported,
		"pricing": map[string]any{
			"markup_percent":           s.cfg.RetailMarkupPercent,
			"display_discount_percent": s.cfg.DisplayDiscountPercent,
			"fx":                       "frankfurter",
			"payments":                 "razorpay_orders_checkout",
			"checkout":                 "POST /v1/checkout",
			"checkout_verify":          "POST /v1/checkout/verify",
			"razorpay_webhook":         "POST /v1/webhooks/razorpay",
		},
		"public_endpoints": []string{
			"GET /healthz",
			"GET /v1/geo",
			"GET /v1/domains/search",
			"GET /v1/domains/suggest",
			"GET /v1/domains/quote",
			"GET /v1/agent",
		},
		"mcp": map[string]any{
			"transport": "stdio",
			"package":   "github.com/namepiq/api/cmd/mcp",
			"env":       []string{"NAMEPIQ_API_BASE", "NAMEPIQ_TOKEN"},
			"tools": []string{
				"search_domains", "suggest_domains", "quote_domain",
				"login", "get_me", "upsert_contact",
				"list_domains", "get_domain", "register_domain", "renew_domain",
				"update_domain", "push_domain_contacts",
				"get_nameservers", "set_nameservers", "get_dns", "set_dns",
			},
		},
		"policies": []string{
			"Always call search/quote for availability and price; do not invent them.",
			"Confirm with the user before register_domain or renew_domain.",
			"Prefer /docs/*.md and llms.txt over scraping HTML.",
		},
	})
}
