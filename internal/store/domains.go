package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Domain struct {
	ID            uuid.UUID  `json:"id"`
	UserID        uuid.UUID  `json:"user_id"`
	Name          string     `json:"name"`
	OpenSRSOrderID *string   `json:"opensrs_order_id,omitempty"`
	Status        string     `json:"status"`
	Locked        bool       `json:"locked"`
	AutoRenew     bool       `json:"auto_renew"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (s *Store) ListDomainsByUser(ctx context.Context, userID uuid.UUID) ([]Domain, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, user_id, name, opensrs_order_id, status, locked, auto_renew, expires_at, created_at, updated_at
		FROM domains WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Domain
	for rows.Next() {
		var d Domain
		if err := rows.Scan(&d.ID, &d.UserID, &d.Name, &d.OpenSRSOrderID, &d.Status, &d.Locked, &d.AutoRenew, &d.ExpiresAt, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) GetDomainForUser(ctx context.Context, userID, domainID uuid.UUID) (Domain, error) {
	var d Domain
	err := s.Pool.QueryRow(ctx, `
		SELECT id, user_id, name, opensrs_order_id, status, locked, auto_renew, expires_at, created_at, updated_at
		FROM domains WHERE id = $1 AND user_id = $2`, domainID, userID,
	).Scan(&d.ID, &d.UserID, &d.Name, &d.OpenSRSOrderID, &d.Status, &d.Locked, &d.AutoRenew, &d.ExpiresAt, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Domain{}, ErrNotFound
	}
	return d, err
}

func (s *Store) CreateDomain(ctx context.Context, d Domain) (Domain, error) {
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO domains (user_id, name, opensrs_order_id, status, locked, auto_renew, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id, user_id, name, opensrs_order_id, status, locked, auto_renew, expires_at, created_at, updated_at`,
		d.UserID, d.Name, d.OpenSRSOrderID, d.Status, d.Locked, d.AutoRenew, d.ExpiresAt,
	).Scan(&d.ID, &d.UserID, &d.Name, &d.OpenSRSOrderID, &d.Status, &d.Locked, &d.AutoRenew, &d.ExpiresAt, &d.CreatedAt, &d.UpdatedAt)
	if isUniqueViolation(err) {
		return Domain{}, ErrConflict
	}
	return d, err
}

func (s *Store) UpdateDomainFlags(ctx context.Context, userID, domainID uuid.UUID, locked, autoRenew *bool) (Domain, error) {
	d, err := s.GetDomainForUser(ctx, userID, domainID)
	if err != nil {
		return Domain{}, err
	}
	if locked != nil {
		d.Locked = *locked
	}
	if autoRenew != nil {
		d.AutoRenew = *autoRenew
	}
	_, err = s.Pool.Exec(ctx, `
		UPDATE domains SET locked=$1, auto_renew=$2, updated_at=now() WHERE id=$3 AND user_id=$4`,
		d.Locked, d.AutoRenew, domainID, userID)
	if err != nil {
		return Domain{}, err
	}
	return s.GetDomainForUser(ctx, userID, domainID)
}

func (s *Store) UpdateDomainExpiry(ctx context.Context, userID, domainID uuid.UUID, expiresAt *time.Time, autoRenew *bool) (Domain, error) {
	d, err := s.GetDomainForUser(ctx, userID, domainID)
	if err != nil {
		return Domain{}, err
	}
	if expiresAt != nil {
		d.ExpiresAt = expiresAt
	}
	if autoRenew != nil {
		d.AutoRenew = *autoRenew
	}
	_, err = s.Pool.Exec(ctx, `
		UPDATE domains SET expires_at=$1, auto_renew=$2, updated_at=now() WHERE id=$3 AND user_id=$4`,
		d.ExpiresAt, d.AutoRenew, domainID, userID)
	if err != nil {
		return Domain{}, err
	}
	return s.GetDomainForUser(ctx, userID, domainID)
}
