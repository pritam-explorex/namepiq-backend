package httpapi

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/namepiq/api/internal/store"
)

// StartWatchChecker polls due watches and emails when a domain becomes available.
func (s *Server) StartWatchChecker(ctx context.Context) {
	go func() {
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		s.runWatchPass(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.runWatchPass(ctx)
			}
		}
	}()
}

func (s *Server) runWatchPass(ctx context.Context) {
	if n, err := s.store.ExpireDueWatches(ctx); err != nil {
		log.Printf("watch expire: %v", err)
	} else if n > 0 {
		log.Printf("watch expire: abandoned %d expired watch(es)", n)
	}
	if s.reg == nil || !s.reg.Configured() {
		return
	}
	due, err := s.store.ClaimDueWatches(ctx, 20)
	if err != nil {
		log.Printf("watch claim: %v", err)
		return
	}
	for _, w := range due {
		if ctx.Err() != nil {
			return
		}
		res, err := s.reg.Lookup(ctx, w.Domain)
		if err != nil {
			_, _ = s.store.MarkWatchChecked(ctx, w.ID, false, err.Error())
			continue
		}
		wasAvailable := w.Status == store.WatchStatusAvailable
		updated, err := s.store.MarkWatchChecked(ctx, w.ID, res.Available, "")
		if err != nil {
			log.Printf("watch mark %s: %v", w.Domain, err)
			continue
		}
		if res.Available && !wasAvailable && (w.NotifiedAt == nil) {
			s.sendWatchAvailableEmail(ctx, updated)
		}
	}
}

func (s *Server) sendWatchAvailableEmail(ctx context.Context, w store.DomainWatch) {
	u, err := s.store.GetUserByID(ctx, w.UserID)
	if err != nil {
		return
	}
	link := fmt.Sprintf("%s/?q=%s", strings.TrimRight(s.cfg.AppBaseURL, "/"), w.Domain)
	subject := fmt.Sprintf("%s is available on Namepiq", w.Domain)
	text := fmt.Sprintf("%s just became available.\n\nRegister it: %s\n", w.Domain, link)
	html := fmt.Sprintf(
		`<p><strong>%s</strong> just became available on Namepiq.</p><p><a href="%s">Search & register</a></p>`,
		w.Domain, link,
	)
	if err := s.mailer.Send(ctx, u.Email, subject, html, text); err != nil {
		log.Printf("watch email %s: %v", w.Domain, err)
	}
}
