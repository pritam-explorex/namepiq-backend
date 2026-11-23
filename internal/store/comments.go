package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type DomainComment struct {
	ID        uuid.UUID  `json:"id"`
	Domain    string     `json:"domain"`
	UserID    uuid.UUID  `json:"user_id"`
	Body      string     `json:"body"`
	CreatedAt time.Time  `json:"created_at"`
	HiddenAt  *time.Time `json:"hidden_at,omitempty"`
	Author    string     `json:"author,omitempty"` // display handle, filled by API
}

func (s *Store) CreateComment(ctx context.Context, domain string, userID uuid.UUID, body string) (DomainComment, error) {
	var c DomainComment
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO domain_comments (domain, user_id, body)
		VALUES ($1, $2, $3)
		RETURNING id, domain, user_id, body, created_at, hidden_at`,
		domain, userID, body,
	).Scan(&c.ID, &c.Domain, &c.UserID, &c.Body, &c.CreatedAt, &c.HiddenAt)
	return c, err
}

func (s *Store) ListCommentsByDomain(ctx context.Context, domain string, limit, offset int) ([]DomainComment, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT c.id, c.domain, c.user_id, c.body, c.created_at, c.hidden_at, u.email
		FROM domain_comments c
		JOIN users u ON u.id = c.user_id
		WHERE c.domain = $1 AND c.hidden_at IS NULL
		ORDER BY c.created_at DESC
		LIMIT $2 OFFSET $3`, domain, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DomainComment
	for rows.Next() {
		var c DomainComment
		var email string
		if err := rows.Scan(&c.ID, &c.Domain, &c.UserID, &c.Body, &c.CreatedAt, &c.HiddenAt, &email); err != nil {
			return nil, err
		}
		c.Author = displayHandle(email)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetComment(ctx context.Context, id uuid.UUID) (DomainComment, error) {
	var c DomainComment
	err := s.Pool.QueryRow(ctx, `
		SELECT id, domain, user_id, body, created_at, hidden_at
		FROM domain_comments WHERE id = $1`, id,
	).Scan(&c.ID, &c.Domain, &c.UserID, &c.Body, &c.CreatedAt, &c.HiddenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return DomainComment{}, ErrNotFound
	}
	return c, err
}

func (s *Store) SoftDeleteComment(ctx context.Context, id, userID uuid.UUID, asAdmin bool) error {
	var tag interface{ RowsAffected() int64 }
	var err error
	if asAdmin {
		t, e := s.Pool.Exec(ctx, `
			UPDATE domain_comments SET hidden_at = now()
			WHERE id = $1 AND hidden_at IS NULL`, id)
		tag, err = t, e
	} else {
		t, e := s.Pool.Exec(ctx, `
			UPDATE domain_comments SET hidden_at = now()
			WHERE id = $1 AND user_id = $2 AND hidden_at IS NULL`, id, userID)
		tag, err = t, e
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CountRecentCommentsByUser(ctx context.Context, userID uuid.UUID, since time.Time) (int64, error) {
	var n int64
	err := s.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM domain_comments
		WHERE user_id = $1 AND created_at >= $2`, userID, since,
	).Scan(&n)
	return n, err
}

func displayHandle(email string) string {
	at := -1
	for i, r := range email {
		if r == '@' {
			at = i
			break
		}
	}
	if at <= 0 {
		return "user"
	}
	local := email[:at]
	if len(local) > 16 {
		local = local[:16]
	}
	return local + "@…"
}
