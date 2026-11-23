package httpapi

import (
	"net/http"

	"github.com/namepiq/api/internal/fx"
)

func (s *Server) handleGeo(w http.ResponseWriter, r *http.Request) {
	suggested := s.requestCurrency(r)
	currencies := make([]map[string]any, 0, len(fx.Supported))
	for _, code := range fx.Supported {
		currencies = append(currencies, map[string]any{
			"code":     code,
			"exponent": fx.Exponent(code),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"suggested_currency": suggested,
		"currencies":         currencies,
		"country_hint":       r.Header.Get("CF-IPCountry"),
	})
}
