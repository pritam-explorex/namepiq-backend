package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type AdminStats struct {
	Users          int64 `json:"users"`
	Domains        int64 `json:"domains"`
	PendingOrders  int64 `json:"pending_orders"`
	ActiveDomains  int64 `json:"active_domains"`
}

type AdminUserRow struct {
	ID              uuid.UUID  `json:"id"`
	Email           string     `json:"email"`
	Role            string     `json:"role"`
	EmailVerifiedAt *time.Time `json:"email_verified_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	DomainCount     int64      `json:"domain_count"`
}

type AdminDomainRow struct {
	Domain
	OwnerEmail string `json:"owner_email"`
}

type Order struct {
	ID                uuid.UUID  `json:"id"`
	UserID            uuid.UUID  `json:"user_id"`
	OwnerEmail        string     `json:"owner_email,omitempty"`
	DomainName        string     `json:"domain_name"`
	Type              string     `json:"type"`
	Years             int        `json:"years"`
	PricePaise        int64      `json:"price_paise"`
	MaxPricePaise     int64      `json:"max_price_paise"`
	Currency          string     `json:"currency"`
	Status            string     `json:"status"`
	RazorpayOrderID   *string    `json:"razorpay_order_id,omitempty"`
	RazorpayPaymentID *string    `json:"razorpay_payment_id,omitempty"`
	CheckoutID        *uuid.UUID `json:"checkout_id,omitempty"`
	AutoRenew         bool       `json:"auto_renew"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type AuditEvent struct {
	ID        uuid.UUID      `json:"id"`
	UserID    *uuid.UUID     `json:"user_id,omitempty"`
	Email     string         `json:"email,omitempty"`
	IP        string         `json:"ip,omitempty"`
	Action    string         `json:"action"`
	Meta      map[string]any `json:"meta"`
	CreatedAt time.Time      `json:"created_at"`
}

func (s *Store) AdminStats(ctx context.Context) (AdminStats, error) {
	var st AdminStats
	err := s.Pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM users),
			(SELECT COUNT(*) FROM domains),
			(SELECT COUNT(*) FROM domains WHERE status = 'active'),
			(SELECT COUNT(*) FROM orders WHERE status = 'pending_payment')`).
		Scan(&st.Users, &st.Domains, &st.ActiveDomains, &st.PendingOrders)
	return st, err
}

func (s *Store) ListUsersAdmin(ctx context.Context, q string, limit, offset int) ([]AdminUserRow, error) {
	limit, offset = pageBounds(limit, offset)
	q = strings.TrimSpace(q)
	rows, err := s.Pool.Query(ctx, `
		SELECT u.id, u.email, u.role, u.email_verified_at, u.created_at,
		       COALESCE((SELECT COUNT(*) FROM domains d WHERE d.user_id = u.id), 0)
		FROM users u
		WHERE ($1 = '' OR u.email ILIKE '%' || $1 || '%')
		ORDER BY u.created_at DESC
		LIMIT $2 OFFSET $3`, q, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AdminUserRow
	for rows.Next() {
		var u AdminUserRow
		if err := rows.Scan(&u.ID, &u.Email, &u.Role, &u.EmailVerifiedAt, &u.CreatedAt, &u.DomainCount); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) ListDomainsAdmin(ctx context.Context, q, status string, limit, offset int) ([]AdminDomainRow, error) {
	limit, offset = pageBounds(limit, offset)
	q = strings.TrimSpace(q)
	status = strings.TrimSpace(status)
	rows, err := s.Pool.Query(ctx, `
		SELECT d.id, d.user_id, d.name, d.opensrs_order_id, d.status, d.locked, d.auto_renew,
		       d.expires_at, d.created_at, d.updated_at, u.email
		FROM domains d
		JOIN users u ON u.id = d.user_id
		WHERE ($1 = '' OR d.name ILIKE '%' || $1 || '%' OR u.email ILIKE '%' || $1 || '%')
		  AND ($2 = '' OR d.status = $2)
		ORDER BY d.created_at DESC
		LIMIT $3 OFFSET $4`, q, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AdminDomainRow
	for rows.Next() {
		var d AdminDomainRow
		if err := rows.Scan(
			&d.ID, &d.UserID, &d.Name, &d.OpenSRSOrderID, &d.Status, &d.Locked, &d.AutoRenew,
			&d.ExpiresAt, &d.CreatedAt, &d.UpdatedAt, &d.OwnerEmail,
		); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) ListOrdersAdmin(ctx context.Context, status string, limit, offset int) ([]Order, error) {
	limit, offset = pageBounds(limit, offset)
	status = strings.TrimSpace(status)
	rows, err := s.Pool.Query(ctx, `
		SELECT o.id, o.user_id, u.email, o.domain_name, o.type, o.years, o.price_paise, o.max_price_paise,
		       o.currency, o.status, o.razorpay_order_id, o.razorpay_payment_id,
		       o.checkout_id, o.auto_renew, o.created_at, o.updated_at
		FROM orders o
		JOIN users u ON u.id = o.user_id
		WHERE ($1 = '' OR o.status = $1)
		ORDER BY o.created_at DESC
		LIMIT $2 OFFSET $3`, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Order
	for rows.Next() {
		var o Order
		if err := rows.Scan(
			&o.ID, &o.UserID, &o.OwnerEmail, &o.DomainName, &o.Type, &o.Years, &o.PricePaise, &o.MaxPricePaise,
			&o.Currency, &o.Status, &o.RazorpayOrderID, &o.RazorpayPaymentID,
			&o.CheckoutID, &o.AutoRenew, &o.CreatedAt, &o.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) ListAuditAdmin(ctx context.Context, limit, offset int) ([]AuditEvent, error) {
	limit, offset = pageBounds(limit, offset)
	rows, err := s.Pool.Query(ctx, `
		SELECT a.id, a.user_id, COALESCE(u.email, ''), COALESCE(a.ip, ''), a.action, a.meta, a.created_at
		FROM audit_events a
		LEFT JOIN users u ON u.id = a.user_id
		ORDER BY a.created_at DESC
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var e AuditEvent
		var meta []byte
		if err := rows.Scan(&e.ID, &e.UserID, &e.Email, &e.IP, &e.Action, &meta, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Meta = decodeJSONMap(meta)
		out = append(out, e)
	}
	return out, rows.Err()
}

// PatchUserAdmin updates role and/or forces email verified.
func (s *Store) PatchUserAdmin(ctx context.Context, id uuid.UUID, role *string, forceVerify bool) (User, error) {
	u, err := s.GetUserByID(ctx, id)
	if err != nil {
		return User{}, err
	}
	if role != nil {
		r := strings.TrimSpace(*role)
		if r != RoleUser && r != RoleAdmin {
			return User{}, fmt.Errorf("invalid role")
		}
		u.Role = r
	}
	if forceVerify && u.EmailVerifiedAt == nil {
		now := time.Now().UTC()
		u.EmailVerifiedAt = &now
	}
	_, err = s.Pool.Exec(ctx, `
		UPDATE users SET role = $2, email_verified_at = $3, updated_at = now()
		WHERE id = $1`, u.ID, u.Role, u.EmailVerifiedAt)
	if err != nil {
		return User{}, err
	}
	return u, nil
}

func pageBounds(limit, offset int) (int, int) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func decodeJSONMap(b []byte) map[string]any {
	if len(b) == 0 {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil || m == nil {
		return map[string]any{}
	}
	return m
}
