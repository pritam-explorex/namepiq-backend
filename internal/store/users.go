package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("conflict")

const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

type User struct {
	ID                 uuid.UUID
	Email              string
	PasswordHash       string
	Role               string
	PreferredCurrency  string
	Country            string
	EmailVerifiedAt    *time.Time
	CreatedAt          time.Time
}

func (u User) IsAdmin() bool {
	return u.Role == RoleAdmin
}

type Contact struct {
	UserID       uuid.UUID `json:"user_id"`
	FirstName    string    `json:"first_name"`
	LastName     string    `json:"last_name"`
	Organization string    `json:"organization"`
	Email        string    `json:"email"`
	Phone        string    `json:"phone"`
	Address1     string    `json:"address1"`
	Address2     string    `json:"address2"`
	City         string    `json:"city"`
	State        string    `json:"state"`
	PostalCode   string    `json:"postal_code"`
	Country      string    `json:"country"`
}

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.PreferredCurrency, &u.Country, &u.EmailVerifiedAt, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	if u.Role == "" {
		u.Role = RoleUser
	}
	if u.PreferredCurrency == "" {
		u.PreferredCurrency = "USD"
	}
	return u, nil
}

func (s *Store) CreateUser(ctx context.Context, email, passwordHash string) (User, error) {
	u, err := scanUser(s.Pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash)
		VALUES ($1, $2)
		RETURNING id, email, password_hash, role, preferred_currency, country, email_verified_at, created_at`,
		email, passwordHash,
	))
	if err != nil {
		if isUniqueViolation(err) {
			return User{}, ErrConflict
		}
		return User{}, err
	}
	return u, nil
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (User, error) {
	return scanUser(s.Pool.QueryRow(ctx, `
		SELECT id, email, password_hash, role, preferred_currency, country, email_verified_at, created_at
		FROM users WHERE email = $1`, email))
}

func (s *Store) GetUserByID(ctx context.Context, id uuid.UUID) (User, error) {
	return scanUser(s.Pool.QueryRow(ctx, `
		SELECT id, email, password_hash, role, preferred_currency, country, email_verified_at, created_at
		FROM users WHERE id = $1`, id))
}

func (s *Store) UpdateUserLocale(ctx context.Context, id uuid.UUID, currency, country string) (User, error) {
	_, err := s.Pool.Exec(ctx, `
		UPDATE users SET preferred_currency = COALESCE(NULLIF($2, ''), preferred_currency),
		                 country = COALESCE($3, country),
		                 updated_at = now()
		WHERE id = $1`, id, currency, country)
	if err != nil {
		return User{}, err
	}
	return s.GetUserByID(ctx, id)
}

// PromoteAdminsByEmail sets role=admin for matching emails (case-insensitive).
func (s *Store) PromoteAdminsByEmail(ctx context.Context, emails []string) (int64, error) {
	if len(emails) == 0 {
		return 0, nil
	}
	normalized := make([]string, 0, len(emails))
	for _, e := range emails {
		e = strings.TrimSpace(strings.ToLower(e))
		if e != "" {
			normalized = append(normalized, e)
		}
	}
	if len(normalized) == 0 {
		return 0, nil
	}
	tag, err := s.Pool.Exec(ctx, `
		UPDATE users SET role = $1, updated_at = now()
		WHERE lower(email) = ANY($2::text[]) AND role <> $1`,
		RoleAdmin, normalized,
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *Store) CreateEmailVerification(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO email_verifications (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)`, userID, tokenHash, expiresAt)
	return err
}

func (s *Store) ConsumeEmailVerification(ctx context.Context, tokenHash string) (uuid.UUID, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)

	var id uuid.UUID
	var userID uuid.UUID
	var expiresAt time.Time
	var usedAt *time.Time
	err = tx.QueryRow(ctx, `
		SELECT id, user_id, expires_at, used_at
		FROM email_verifications
		WHERE token_hash = $1
		FOR UPDATE`, tokenHash,
	).Scan(&id, &userID, &expiresAt, &usedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}
	if time.Now().UTC().After(expiresAt) {
		return uuid.Nil, ErrNotFound
	}
	// Idempotent: already-consumed tokens still succeed (React Strict Mode double-fetch).
	if usedAt == nil {
		if _, err := tx.Exec(ctx, `UPDATE email_verifications SET used_at = now() WHERE id = $1`, id); err != nil {
			return uuid.Nil, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE users SET email_verified_at = COALESCE(email_verified_at, now()), updated_at = now()
		WHERE id = $1`, userID); err != nil {
		return uuid.Nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}
	return userID, nil
}

func (s *Store) UpsertContact(ctx context.Context, c Contact) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO contacts (
			user_id, first_name, last_name, organization, email, phone,
			address1, address2, city, state, postal_code, country, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12, now()
		)
		ON CONFLICT (user_id) DO UPDATE SET
			first_name = EXCLUDED.first_name,
			last_name = EXCLUDED.last_name,
			organization = EXCLUDED.organization,
			email = EXCLUDED.email,
			phone = EXCLUDED.phone,
			address1 = EXCLUDED.address1,
			address2 = EXCLUDED.address2,
			city = EXCLUDED.city,
			state = EXCLUDED.state,
			postal_code = EXCLUDED.postal_code,
			country = EXCLUDED.country,
			updated_at = now()`,
		c.UserID, c.FirstName, c.LastName, c.Organization, c.Email, c.Phone,
		c.Address1, c.Address2, c.City, c.State, c.PostalCode, c.Country,
	)
	return err
}

func (s *Store) GetContact(ctx context.Context, userID uuid.UUID) (Contact, error) {
	var c Contact
	err := s.Pool.QueryRow(ctx, `
		SELECT user_id, first_name, last_name, organization, email, phone,
		       address1, address2, city, state, postal_code, country
		FROM contacts WHERE user_id = $1`, userID,
	).Scan(
		&c.UserID, &c.FirstName, &c.LastName, &c.Organization, &c.Email, &c.Phone,
		&c.Address1, &c.Address2, &c.City, &c.State, &c.PostalCode, &c.Country,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Contact{UserID: userID}, nil
	}
	return c, err
}

func (s *Store) InsertAudit(ctx context.Context, userID *uuid.UUID, ip, action string, meta map[string]any) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO audit_events (user_id, ip, action, meta)
		VALUES ($1, $2, $3, COALESCE($4::jsonb, '{}'::jsonb))`,
		userID, ip, action, mustJSON(meta),
	)
	return err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
