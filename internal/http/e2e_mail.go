package httpapi

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/namepiq/api/internal/mail"
)

var verifyTokenRE = regexp.MustCompile(`token=([A-Za-z0-9\-_]+)`)

func (s *Server) handleE2ELastEmail(w http.ResponseWriter, r *http.Request) {
	if s.mailCapture == nil {
		writeError(w, http.StatusNotFound, "not_found", "mail capture disabled")
		return
	}
	to := strings.TrimSpace(r.URL.Query().Get("to"))
	var email mail.Message
	var ok bool
	if to != "" {
		email, ok = s.mailCapture.LastFor(to)
	} else {
		email, ok = s.mailCapture.Last()
	}
	if !ok {
		writeError(w, http.StatusNotFound, "no_email", "no captured email yet")
		return
	}
	token := ""
	if match := verifyTokenRE.FindStringSubmatch(email.Text); len(match) > 1 {
		token = match[1]
	}
	if token == "" {
		if match := verifyTokenRE.FindStringSubmatch(email.HTML); len(match) > 1 {
			token = match[1]
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"to":           email.To,
		"subject":      email.Subject,
		"text":         email.Text,
		"html":         email.HTML,
		"verify_token": token,
		"verify_path":  "/verify-email?token=" + token,
	})
}

func (s *Server) handleE2EEmails(w http.ResponseWriter, r *http.Request) {
	if s.mailCapture == nil {
		writeError(w, http.StatusNotFound, "not_found", "mail capture disabled")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"emails": s.mailCapture.All()})
}
