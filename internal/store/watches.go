package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	WatchTierFree     = "free"
	WatchTierStandard = "standard"
	WatchTierPriority = "priority"

	WatchStatusPending   = "pending_payment"
	WatchStatusTaken     = "taken"
	WatchStatusAvailable = "available"
	WatchStatusAbandoned = "abandoned"

	FreeWatchLimit     = 3
	MaxPaidWatches     = 50
	StandardPriceMinor = int64(100) // $1.00 USD · every 6h
	PriorityPriceMinor = int64(500) // $5.00 USD · every 1h
	WatchDuration      = 30 * 24 * time.Hour
)

func WatchCheckInterval(tier string) time.Duration {
	switch tier {
	case WatchTierPriority:
		return time.Hour
	case WatchTierStandard:
		return 6 * time.Hour
	default:
		return 24 * time.Hour
	}
}

type DomainWatch struct {
	ID                uuid.UUID  `json:"id"`
	UserID            uuid.UUID  `json:"user_id"`
	Domain            string     `json:"domain"`
	Tier              string     `json:"tier"`
	Status            string     `json:"status"`
	LastCheckedAt     *time.Time `json:"last_checked_at,omitempty"`
	NextCheckAt       *time.Time `json:"next_check_at,omitempty"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	NotifiedAt        *time.Time `json:"notified_at,omitempty"`
	RazorpayOrderID   *string    `json:"razorpay_order_id,omitempty"`
	RazorpayPaymentID *string    `json:"razorpay_payment_id,omitempty"`
	PriceMinor        *int64     `json:"price_minor,omitempty"`
	Currency          *string    `json:"currency,omitempty"`
	LastError         *string    `json:"last_error,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type TopWatched struct {
	Domain        string     `json:"domain"`
	WatchCount    int64      `json:"watch_count"`
	LastCheckedAt *time.Time `json:"last_checked_at,omitempty"`
}

type WatchDomainStatus struct {
	Domain        string     `json:"domain"`
	WatchCount    int64      `json:"watch_count"`
	LastCheckedAt *time.Time `json:"last_checked_at,omitempty"`
}

func (s *Store) CreateWatch(ctx context.Context, w DomainWatch) (DomainWatch, error) {
	if w.Tier == "" {
		w.Tier = WatchTierFree
	}
	if w.Status == "" {
		w.Status = WatchStatusTaken
	}
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO domain_watches (
			user_id, domain, tier, status, last_checked_at, next_check_at, expires_at,
			razorpay_order_id, price_minor, currency
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id, user_id, domain, tier, status, last_checked_at, next_check_at, expires_at,
		          notified_at, razorpay_order_id, razorpay_payment_id, price_minor, currency,
		          last_error, created_at, updated_at`,
		w.UserID, w.Domain, w.Tier, w.Status, w.LastCheckedAt, w.NextCheckAt, w.ExpiresAt,
		w.RazorpayOrderID, w.PriceMinor, w.Currency,
	).Scan(
		&w.ID, &w.UserID, &w.Domain, &w.Tier, &w.Status, &w.LastCheckedAt, &w.NextCheckAt, &w.ExpiresAt,
		&w.NotifiedAt, &w.RazorpayOrderID, &w.RazorpayPaymentID, &w.PriceMinor, &w.Currency,
		&w.LastError, &w.CreatedAt, &w.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return DomainWatch{}, ErrConflict
		}
		return DomainWatch{}, err
	}
	return w, nil
}

func (s *Store) ListWatchesByUser(ctx context.Context, userID uuid.UUID) ([]DomainWatch, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, user_id, domain, tier, status, last_checked_at, next_check_at, expires_at,
		       notified_at, razorpay_order_id, razorpay_payment_id, price_minor, currency,
		       last_error, created_at, updated_at
		FROM domain_watches
		WHERE user_id = $1 AND status <> 'abandoned'
		ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWatches(rows)
}

func (s *Store) GetWatchByID(ctx context.Context, id uuid.UUID) (DomainWatch, error) {
	var w DomainWatch
	err := s.Pool.QueryRow(ctx, `
		SELECT id, user_id, domain, tier, status, last_checked_at, next_check_at, expires_at,
		       notified_at, razorpay_order_id, razorpay_payment_id, price_minor, currency,
		       last_error, created_at, updated_at
		FROM domain_watches WHERE id = $1`, id,
	).Scan(
		&w.ID, &w.UserID, &w.Domain, &w.Tier, &w.Status, &w.LastCheckedAt, &w.NextCheckAt, &w.ExpiresAt,
		&w.NotifiedAt, &w.RazorpayOrderID, &w.RazorpayPaymentID, &w.PriceMinor, &w.Currency,
		&w.LastError, &w.CreatedAt, &w.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return DomainWatch{}, ErrNotFound
	}
	return w, err
}

func (s *Store) GetWatchByRazorpayOrderID(ctx context.Context, orderID string) (DomainWatch, error) {
	var w DomainWatch
	err := s.Pool.QueryRow(ctx, `
		SELECT id, user_id, domain, tier, status, last_checked_at, next_check_at, expires_at,
		       notified_at, razorpay_order_id, razorpay_payment_id, price_minor, currency,
		       last_error, created_at, updated_at
		FROM domain_watches WHERE razorpay_order_id = $1`, orderID,
	).Scan(
		&w.ID, &w.UserID, &w.Domain, &w.Tier, &w.Status, &w.LastCheckedAt, &w.NextCheckAt, &w.ExpiresAt,
		&w.NotifiedAt, &w.RazorpayOrderID, &w.RazorpayPaymentID, &w.PriceMinor, &w.Currency,
		&w.LastError, &w.CreatedAt, &w.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return DomainWatch{}, ErrNotFound
	}
	return w, err
}

func (s *Store) DeleteWatch(ctx context.Context, userID, id uuid.UUID) error {
	tag, err := s.Pool.Exec(ctx, `
		UPDATE domain_watches SET status = 'abandoned', updated_at = now()
		WHERE id = $1 AND user_id = $2 AND status <> 'abandoned'`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CountActiveWatchesByTier(ctx context.Context, userID uuid.UUID, tier string) (int64, error) {
	var n int64
	err := s.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM domain_watches
		WHERE user_id = $1 AND tier = $2 AND status IN ('taken', 'available')
		  AND (expires_at IS NULL OR expires_at > now())`, userID, tier,
	).Scan(&n)
	return n, err
}

func (s *Store) CountActivePaidWatches(ctx context.Context, userID uuid.UUID) (int64, error) {
	var n int64
	err := s.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM domain_watches
		WHERE user_id = $1 AND tier IN ('standard', 'priority') AND status IN ('taken', 'available')
		  AND (expires_at IS NULL OR expires_at > now())`, userID,
	).Scan(&n)
	return n, err
}

func (s *Store) CountWatchersByDomain(ctx context.Context, domains []string) (map[string]int64, error) {
	out := map[string]int64{}
	if len(domains) == 0 {
		return out, nil
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT domain, COUNT(DISTINCT user_id)
		FROM domain_watches
		WHERE domain = ANY($1) AND status IN ('taken', 'available')
		  AND (expires_at IS NULL OR expires_at > now())
		GROUP BY domain`, domains)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d string
		var n int64
		if err := rows.Scan(&d, &n); err != nil {
			return nil, err
		}
		out[d] = n
	}
	return out, rows.Err()
}

func (s *Store) ListTopWatched(ctx context.Context, limit int) ([]TopWatched, error) {
	if limit < 1 {
		limit = 10
	}
	if limit > 10 {
		limit = 10
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT domain, COUNT(DISTINCT user_id) AS watch_count, MAX(last_checked_at) AS last_checked_at
		FROM domain_watches
		WHERE status IN ('taken', 'available')
		  AND (expires_at IS NULL OR expires_at > now())
		GROUP BY domain
		HAVING COUNT(DISTINCT user_id) > 0
		ORDER BY watch_count DESC, domain ASC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TopWatched
	for rows.Next() {
		var t TopWatched
		if err := rows.Scan(&t.Domain, &t.WatchCount, &t.LastCheckedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) GetWatchDomainStatus(ctx context.Context, domain string) (WatchDomainStatus, error) {
	var st WatchDomainStatus
	st.Domain = domain
	err := s.Pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT user_id), MAX(last_checked_at)
		FROM domain_watches
		WHERE domain = $1 AND status IN ('taken', 'available')
		  AND (expires_at IS NULL OR expires_at > now())`, domain,
	).Scan(&st.WatchCount, &st.LastCheckedAt)
	return st, err
}

func (s *Store) ActivatePaidWatch(ctx context.Context, razorpayOrderID, paymentID string) (DomainWatch, error) {
	now := time.Now().UTC()
	expires := now.Add(WatchDuration)
	var w DomainWatch
	err := s.Pool.QueryRow(ctx, `
		UPDATE domain_watches
		SET status = 'taken',
		    razorpay_payment_id = $2,
		    next_check_at = $3,
		    expires_at = $4,
		    updated_at = now()
		WHERE razorpay_order_id = $1
		  AND status = 'pending_payment'
		RETURNING id, user_id, domain, tier, status, last_checked_at, next_check_at, expires_at,
		          notified_at, razorpay_order_id, razorpay_payment_id, price_minor, currency,
		          last_error, created_at, updated_at`,
		razorpayOrderID, paymentID, now, expires,
	).Scan(
		&w.ID, &w.UserID, &w.Domain, &w.Tier, &w.Status, &w.LastCheckedAt, &w.NextCheckAt, &w.ExpiresAt,
		&w.NotifiedAt, &w.RazorpayOrderID, &w.RazorpayPaymentID, &w.PriceMinor, &w.Currency,
		&w.LastError, &w.CreatedAt, &w.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		// already activated or missing
		return s.GetWatchByRazorpayOrderID(ctx, razorpayOrderID)
	}
	return w, err
}

func (s *Store) ClaimDueWatches(ctx context.Context, limit int) ([]DomainWatch, error) {
	if limit < 1 {
		limit = 20
	}
	rows, err := s.Pool.Query(ctx, `
		UPDATE domain_watches
		SET next_check_at = now() + interval '15 minutes',
		    updated_at = now()
		WHERE id IN (
			SELECT id FROM domain_watches
			WHERE status = 'taken'
			  AND (expires_at IS NULL OR expires_at > now())
			  AND (next_check_at IS NULL OR next_check_at <= now())
			ORDER BY next_check_at NULLS FIRST
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, user_id, domain, tier, status, last_checked_at, next_check_at, expires_at,
		          notified_at, razorpay_order_id, razorpay_payment_id, price_minor, currency,
		          last_error, created_at, updated_at`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWatches(rows)
}

// ExpireDueWatches marks watches past expires_at as abandoned.
func (s *Store) ExpireDueWatches(ctx context.Context) (int64, error) {
	tag, err := s.Pool.Exec(ctx, `
		UPDATE domain_watches
		SET status = 'abandoned', updated_at = now()
		WHERE status IN ('taken', 'available', 'pending_payment')
		  AND expires_at IS NOT NULL
		  AND expires_at <= now()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *Store) MarkWatchChecked(ctx context.Context, id uuid.UUID, available bool, errMsg string) (DomainWatch, error) {
	now := time.Now().UTC()
	var w DomainWatch
	cur, err := s.GetWatchByID(ctx, id)
	if err != nil {
		return DomainWatch{}, err
	}
	next := now.Add(WatchCheckInterval(cur.Tier))
	status := cur.Status
	var notified *time.Time
	if available {
		status = WatchStatusAvailable
		if cur.NotifiedAt == nil {
			notified = &now
		} else {
			notified = cur.NotifiedAt
		}
		next = now.Add(30 * 24 * time.Hour)
	} else if cur.Status == WatchStatusAvailable {
		// became taken again
		status = WatchStatusTaken
		notified = nil
	}
	var lastErr *string
	if errMsg != "" {
		lastErr = &errMsg
		next = now.Add(6 * time.Hour)
	}
	err = s.Pool.QueryRow(ctx, `
		UPDATE domain_watches
		SET status = $2,
		    last_checked_at = $3,
		    next_check_at = $4,
		    notified_at = $5,
		    last_error = $6,
		    updated_at = now()
		WHERE id = $1
		RETURNING id, user_id, domain, tier, status, last_checked_at, next_check_at, expires_at,
		          notified_at, razorpay_order_id, razorpay_payment_id, price_minor, currency,
		          last_error, created_at, updated_at`,
		id, status, now, next, notified, lastErr,
	).Scan(
		&w.ID, &w.UserID, &w.Domain, &w.Tier, &w.Status, &w.LastCheckedAt, &w.NextCheckAt, &w.ExpiresAt,
		&w.NotifiedAt, &w.RazorpayOrderID, &w.RazorpayPaymentID, &w.PriceMinor, &w.Currency,
		&w.LastError, &w.CreatedAt, &w.UpdatedAt,
	)
	return w, err
}

func (s *Store) UserOwnsDomainName(ctx context.Context, userID uuid.UUID, name string) (bool, error) {
	var n int64
	err := s.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM domains WHERE user_id = $1 AND lower(name) = lower($2)`, userID, name,
	).Scan(&n)
	return n > 0, err
}

func scanWatches(rows pgx.Rows) ([]DomainWatch, error) {
	var out []DomainWatch
	for rows.Next() {
		var w DomainWatch
		if err := rows.Scan(
			&w.ID, &w.UserID, &w.Domain, &w.Tier, &w.Status, &w.LastCheckedAt, &w.NextCheckAt, &w.ExpiresAt,
			&w.NotifiedAt, &w.RazorpayOrderID, &w.RazorpayPaymentID, &w.PriceMinor, &w.Currency,
			&w.LastError, &w.CreatedAt, &w.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}
