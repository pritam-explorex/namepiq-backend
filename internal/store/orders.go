package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type NewOrder struct {
	UserID        uuid.UUID
	CheckoutID    uuid.UUID
	DomainName    string
	Type          string // register | renew
	Years         int
	PricePaise    int64
	MaxPricePaise int64
	Currency      string
	AutoRenew     bool
	TOSAcceptedAt *time.Time
}

func (s *Store) CreateOrders(ctx context.Context, rows []NewOrder, razorpayOrderID string) ([]Order, error) {
	if len(rows) == 0 {
		return nil, errors.New("no orders")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	out := make([]Order, 0, len(rows))
	for _, row := range rows {
		if row.Type == "" {
			row.Type = "register"
		}
		if row.Years < 1 {
			row.Years = 1
		}
		if row.Currency == "" {
			row.Currency = "INR"
		}
		var o Order
		err := tx.QueryRow(ctx, `
			INSERT INTO orders (
				user_id, checkout_id, domain_name, type, years,
				price_paise, max_price_paise, currency, status,
				razorpay_order_id, auto_renew, tos_accepted_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'pending_payment',$9,$10,$11)
			RETURNING id, user_id, domain_name, type, years, price_paise, max_price_paise,
			          currency, status, razorpay_order_id, razorpay_payment_id,
			          checkout_id, auto_renew, created_at, updated_at`,
			row.UserID, row.CheckoutID, row.DomainName, row.Type, row.Years,
			row.PricePaise, row.MaxPricePaise, row.Currency, razorpayOrderID,
			row.AutoRenew, row.TOSAcceptedAt,
		).Scan(
			&o.ID, &o.UserID, &o.DomainName, &o.Type, &o.Years, &o.PricePaise, &o.MaxPricePaise,
			&o.Currency, &o.Status, &o.RazorpayOrderID, &o.RazorpayPaymentID,
			&o.CheckoutID, &o.AutoRenew, &o.CreatedAt, &o.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) ListOrdersByRazorpayOrderID(ctx context.Context, razorpayOrderID string) ([]Order, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, user_id, domain_name, type, years, price_paise, max_price_paise,
		       currency, status, razorpay_order_id, razorpay_payment_id,
		       checkout_id, auto_renew, created_at, updated_at
		FROM orders WHERE razorpay_order_id = $1
		ORDER BY created_at`, razorpayOrderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOrders(rows)
}

func (s *Store) ListOrdersByCheckoutID(ctx context.Context, checkoutID uuid.UUID) ([]Order, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, user_id, domain_name, type, years, price_paise, max_price_paise,
		       currency, status, razorpay_order_id, razorpay_payment_id,
		       checkout_id, auto_renew, created_at, updated_at
		FROM orders WHERE checkout_id = $1
		ORDER BY created_at`, checkoutID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOrders(rows)
}

func (s *Store) MarkOrdersPaid(ctx context.Context, razorpayOrderID, paymentID string) (int64, error) {
	tag, err := s.Pool.Exec(ctx, `
		UPDATE orders
		SET status = 'paid',
		    razorpay_payment_id = $2,
		    updated_at = now()
		WHERE razorpay_order_id = $1
		  AND status = 'pending_payment'`, razorpayOrderID, paymentID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *Store) UpdateOrderStatus(ctx context.Context, id uuid.UUID, status string) error {
	_, err := s.Pool.Exec(ctx, `
		UPDATE orders SET status = $2, updated_at = now() WHERE id = $1`, id, status)
	return err
}

// PendingCheckout is a still-open payment for a user.
type PendingCheckout struct {
	CheckoutID      uuid.UUID
	RazorpayOrderID string
	Amount          int64
	Currency        string
	CreatedAt       time.Time
}

// GetActivePendingCheckout returns the newest pending_payment checkout newer than since.
func (s *Store) GetActivePendingCheckout(ctx context.Context, userID uuid.UUID, since time.Time) (PendingCheckout, bool, error) {
	var p PendingCheckout
	err := s.Pool.QueryRow(ctx, `
		SELECT checkout_id, razorpay_order_id, SUM(price_paise)::bigint, currency, MIN(created_at)
		FROM orders
		WHERE user_id = $1
		  AND status = 'pending_payment'
		  AND created_at >= $2
		  AND checkout_id IS NOT NULL
		  AND razorpay_order_id IS NOT NULL
		GROUP BY checkout_id, razorpay_order_id, currency
		ORDER BY MIN(created_at) DESC
		LIMIT 1`, userID, since,
	).Scan(&p.CheckoutID, &p.RazorpayOrderID, &p.Amount, &p.Currency, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return PendingCheckout{}, false, nil
	}
	if err != nil {
		return PendingCheckout{}, false, err
	}
	return p, true, nil
}

// AbandonPendingCheckouts marks a user's pending_payment orders as abandoned.
func (s *Store) AbandonPendingCheckouts(ctx context.Context, userID uuid.UUID) (int64, error) {
	tag, err := s.Pool.Exec(ctx, `
		UPDATE orders
		SET status = 'abandoned', updated_at = now()
		WHERE user_id = $1 AND status = 'pending_payment'`, userID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func scanOrders(rows pgx.Rows) ([]Order, error) {
	var out []Order
	for rows.Next() {
		var o Order
		if err := rows.Scan(
			&o.ID, &o.UserID, &o.DomainName, &o.Type, &o.Years, &o.PricePaise, &o.MaxPricePaise,
			&o.Currency, &o.Status, &o.RazorpayOrderID, &o.RazorpayPaymentID,
			&o.CheckoutID, &o.AutoRenew, &o.CreatedAt, &o.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
