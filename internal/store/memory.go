package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Memory is an in-memory Repository for unit/handler tests.
type Memory struct {
	mu       sync.Mutex
	users    map[uuid.UUID]User
	byEmail  map[string]uuid.UUID
	verify   map[string]memVerify
	contacts map[uuid.UUID]Contact
	domains  map[uuid.UUID]Domain
	orders   map[uuid.UUID]Order
	watches  map[uuid.UUID]DomainWatch
	comments map[uuid.UUID]DomainComment
	audits   []memAudit
}

type memVerify struct {
	userID    uuid.UUID
	expiresAt time.Time
	usedAt    *time.Time
}

type memAudit struct {
	UserID *uuid.UUID
	IP     string
	Action string
	Meta   map[string]any
}

func NewMemory() *Memory {
	return &Memory{
		users:    map[uuid.UUID]User{},
		byEmail:  map[string]uuid.UUID{},
		verify:   map[string]memVerify{},
		contacts: map[uuid.UUID]Contact{},
		domains:  map[uuid.UUID]Domain{},
		orders:   map[uuid.UUID]Order{},
		watches:  map[uuid.UUID]DomainWatch{},
		comments: map[uuid.UUID]DomainComment{},
	}
}

func (m *Memory) Ping(ctx context.Context) error { return nil }

func (m *Memory) CreateUser(ctx context.Context, email, passwordHash string) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byEmail[email]; ok {
		return User{}, ErrConflict
	}
	u := User{
		ID:                uuid.New(),
		Email:             email,
		PasswordHash:      passwordHash,
		Role:              RoleUser,
		PreferredCurrency: "USD",
		CreatedAt:         time.Now().UTC(),
	}
	m.users[u.ID] = u
	m.byEmail[email] = u.ID
	return u, nil
}

func (m *Memory) GetUserByEmail(ctx context.Context, email string) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.byEmail[email]
	if !ok {
		return User{}, ErrNotFound
	}
	return m.users[id], nil
}

func (m *Memory) GetUserByID(ctx context.Context, id uuid.UUID) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}

func (m *Memory) CreateEmailVerification(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.verify[tokenHash] = memVerify{userID: userID, expiresAt: expiresAt}
	return nil
}

func (m *Memory) ConsumeEmailVerification(ctx context.Context, tokenHash string) (uuid.UUID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.verify[tokenHash]
	if !ok || time.Now().UTC().After(v.expiresAt) {
		return uuid.Nil, ErrNotFound
	}
	now := time.Now().UTC()
	if v.usedAt == nil {
		v.usedAt = &now
		m.verify[tokenHash] = v
	}
	u := m.users[v.userID]
	if u.EmailVerifiedAt == nil {
		u.EmailVerifiedAt = &now
		m.users[v.userID] = u
	}
	return v.userID, nil
}

func (m *Memory) UpsertContact(ctx context.Context, c Contact) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.contacts[c.UserID] = c
	return nil
}

func (m *Memory) GetContact(ctx context.Context, userID uuid.UUID) (Contact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.contacts[userID]; ok {
		return c, nil
	}
	return Contact{UserID: userID}, nil
}

func (m *Memory) InsertAudit(ctx context.Context, userID *uuid.UUID, ip, action string, meta map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.audits = append(m.audits, memAudit{UserID: userID, IP: ip, Action: action, Meta: meta})
	return nil
}

func (m *Memory) AuditActions() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.audits))
	for i, a := range m.audits {
		out[i] = a.Action
	}
	return out
}

func (m *Memory) ListDomainsByUser(ctx context.Context, userID uuid.UUID) ([]Domain, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Domain
	for _, d := range m.domains {
		if d.UserID == userID {
			out = append(out, d)
		}
	}
	return out, nil
}

func (m *Memory) GetDomainForUser(ctx context.Context, userID, domainID uuid.UUID) (Domain, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.domains[domainID]
	if !ok || d.UserID != userID {
		return Domain{}, ErrNotFound
	}
	return d, nil
}

func (m *Memory) CreateDomain(ctx context.Context, d Domain) (Domain, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.domains {
		if existing.Name == d.Name {
			return Domain{}, ErrConflict
		}
	}
	d.ID = uuid.New()
	now := time.Now().UTC()
	d.CreatedAt = now
	d.UpdatedAt = now
	if d.Status == "" {
		d.Status = "active"
	}
	m.domains[d.ID] = d
	return d, nil
}

func (m *Memory) UpdateDomainFlags(ctx context.Context, userID, domainID uuid.UUID, locked, autoRenew *bool) (Domain, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.domains[domainID]
	if !ok || d.UserID != userID {
		return Domain{}, ErrNotFound
	}
	if locked != nil {
		d.Locked = *locked
	}
	if autoRenew != nil {
		d.AutoRenew = *autoRenew
	}
	d.UpdatedAt = time.Now().UTC()
	m.domains[domainID] = d
	return d, nil
}

func (m *Memory) UpdateDomainExpiry(ctx context.Context, userID, domainID uuid.UUID, expiresAt *time.Time, autoRenew *bool) (Domain, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.domains[domainID]
	if !ok || d.UserID != userID {
		return Domain{}, ErrNotFound
	}
	if expiresAt != nil {
		d.ExpiresAt = expiresAt
	}
	if autoRenew != nil {
		d.AutoRenew = *autoRenew
	}
	d.UpdatedAt = time.Now().UTC()
	m.domains[domainID] = d
	return d, nil
}

func (m *Memory) PromoteAdminsByEmail(ctx context.Context, emails []string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	set := map[string]struct{}{}
	for _, e := range emails {
		set[strings.ToLower(strings.TrimSpace(e))] = struct{}{}
	}
	var n int64
	for id, u := range m.users {
		if _, ok := set[strings.ToLower(u.Email)]; ok && u.Role != RoleAdmin {
			u.Role = RoleAdmin
			m.users[id] = u
			n++
		}
	}
	return n, nil
}

func (m *Memory) AdminStats(ctx context.Context) (AdminStats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var active int64
	for _, d := range m.domains {
		if d.Status == "active" {
			active++
		}
	}
	return AdminStats{
		Users:         int64(len(m.users)),
		Domains:       int64(len(m.domains)),
		ActiveDomains: active,
	}, nil
}

func (m *Memory) ListUsersAdmin(ctx context.Context, q string, limit, offset int) ([]AdminUserRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	limit, offset = pageBounds(limit, offset)
	q = strings.ToLower(strings.TrimSpace(q))
	var all []AdminUserRow
	for _, u := range m.users {
		if q != "" && !strings.Contains(strings.ToLower(u.Email), q) {
			continue
		}
		var count int64
		for _, d := range m.domains {
			if d.UserID == u.ID {
				count++
			}
		}
		all = append(all, AdminUserRow{
			ID: u.ID, Email: u.Email, Role: u.Role,
			EmailVerifiedAt: u.EmailVerifiedAt, CreatedAt: u.CreatedAt, DomainCount: count,
		})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	return slicePage(all, limit, offset), nil
}

func (m *Memory) ListDomainsAdmin(ctx context.Context, q, status string, limit, offset int) ([]AdminDomainRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	limit, offset = pageBounds(limit, offset)
	q = strings.ToLower(strings.TrimSpace(q))
	status = strings.TrimSpace(status)
	var all []AdminDomainRow
	for _, d := range m.domains {
		email := ""
		if u, ok := m.users[d.UserID]; ok {
			email = u.Email
		}
		if status != "" && d.Status != status {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(d.Name), q) && !strings.Contains(strings.ToLower(email), q) {
			continue
		}
		all = append(all, AdminDomainRow{Domain: d, OwnerEmail: email})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	return slicePage(all, limit, offset), nil
}

func (m *Memory) CreateOrders(ctx context.Context, rows []NewOrder, razorpayOrderID string) ([]Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Order, 0, len(rows))
	rp := razorpayOrderID
	for _, row := range rows {
		id := uuid.New()
		cid := row.CheckoutID
		o := Order{
			ID: id, UserID: row.UserID, DomainName: row.DomainName, Type: row.Type,
			Years: row.Years, PricePaise: row.PricePaise, MaxPricePaise: row.MaxPricePaise,
			Currency: row.Currency, Status: "pending_payment", RazorpayOrderID: &rp,
			CheckoutID: &cid, AutoRenew: row.AutoRenew,
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
		if o.Type == "" {
			o.Type = "register"
		}
		m.orders[id] = o
		out = append(out, o)
	}
	return out, nil
}

func (m *Memory) ListOrdersByRazorpayOrderID(ctx context.Context, razorpayOrderID string) ([]Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Order
	for _, o := range m.orders {
		if o.RazorpayOrderID != nil && *o.RazorpayOrderID == razorpayOrderID {
			out = append(out, o)
		}
	}
	return out, nil
}

func (m *Memory) ListOrdersByCheckoutID(ctx context.Context, checkoutID uuid.UUID) ([]Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Order
	for _, o := range m.orders {
		if o.CheckoutID != nil && *o.CheckoutID == checkoutID {
			out = append(out, o)
		}
	}
	return out, nil
}

func (m *Memory) MarkOrdersPaid(ctx context.Context, razorpayOrderID, paymentID string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for id, o := range m.orders {
		if o.RazorpayOrderID != nil && *o.RazorpayOrderID == razorpayOrderID && o.Status == "pending_payment" {
			o.Status = "paid"
			pid := paymentID
			o.RazorpayPaymentID = &pid
			o.UpdatedAt = time.Now().UTC()
			m.orders[id] = o
			n++
		}
	}
	return n, nil
}

func (m *Memory) UpdateOrderStatus(ctx context.Context, id uuid.UUID, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.orders[id]
	if !ok {
		return ErrNotFound
	}
	o.Status = status
	o.UpdatedAt = time.Now().UTC()
	m.orders[id] = o
	return nil
}

func (m *Memory) GetActivePendingCheckout(ctx context.Context, userID uuid.UUID, since time.Time) (PendingCheckout, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	type agg struct {
		p     PendingCheckout
		total int64
	}
	byCheckout := map[uuid.UUID]*agg{}
	for _, o := range m.orders {
		if o.UserID != userID || o.Status != "pending_payment" || o.CheckoutID == nil || o.RazorpayOrderID == nil {
			continue
		}
		if o.CreatedAt.Before(since) {
			continue
		}
		a, ok := byCheckout[*o.CheckoutID]
		if !ok {
			a = &agg{p: PendingCheckout{
				CheckoutID:      *o.CheckoutID,
				RazorpayOrderID: *o.RazorpayOrderID,
				Currency:        o.Currency,
				CreatedAt:       o.CreatedAt,
			}}
			byCheckout[*o.CheckoutID] = a
		}
		a.total += o.PricePaise
		if o.CreatedAt.Before(a.p.CreatedAt) {
			a.p.CreatedAt = o.CreatedAt
		}
	}
	var best *agg
	for _, a := range byCheckout {
		a.p.Amount = a.total
		if best == nil || a.p.CreatedAt.After(best.p.CreatedAt) {
			best = a
		}
	}
	if best == nil {
		return PendingCheckout{}, false, nil
	}
	return best.p, true, nil
}

func (m *Memory) AbandonPendingCheckouts(ctx context.Context, userID uuid.UUID) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for id, o := range m.orders {
		if o.UserID == userID && o.Status == "pending_payment" {
			o.Status = "abandoned"
			o.UpdatedAt = time.Now().UTC()
			m.orders[id] = o
			n++
		}
	}
	return n, nil
}

func (m *Memory) ListOrdersAdmin(ctx context.Context, status string, limit, offset int) ([]Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	limit, offset = pageBounds(limit, offset)
	var all []Order
	for _, o := range m.orders {
		if status != "" && o.Status != status {
			continue
		}
		all = append(all, o)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	return slicePage(all, limit, offset), nil
}

func (m *Memory) ListAuditAdmin(ctx context.Context, limit, offset int) ([]AuditEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	limit, offset = pageBounds(limit, offset)
	n := len(m.audits)
	start := n - offset - limit
	end := n - offset
	if end > n {
		end = n
	}
	if start < 0 {
		start = 0
	}
	if start >= end {
		return nil, nil
	}
	var out []AuditEvent
	for i := end - 1; i >= start; i-- {
		a := m.audits[i]
		email := ""
		if a.UserID != nil {
			if u, ok := m.users[*a.UserID]; ok {
				email = u.Email
			}
		}
		out = append(out, AuditEvent{
			ID: uuid.New(), UserID: a.UserID, Email: email, IP: a.IP,
			Action: a.Action, Meta: a.Meta, CreatedAt: time.Now().UTC(),
		})
	}
	return out, nil
}

func (m *Memory) UpdateUserLocale(ctx context.Context, id uuid.UUID, currency, country string) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	if currency != "" {
		u.PreferredCurrency = currency
	}
	if country != "" {
		u.Country = country
	}
	m.users[id] = u
	return u, nil
}

func (m *Memory) PatchUserAdmin(ctx context.Context, id uuid.UUID, role *string, forceVerify bool) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return User{}, ErrNotFound
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
	m.users[id] = u
	return u, nil
}

func (m *Memory) CreateWatch(ctx context.Context, w DomainWatch) (DomainWatch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.watches {
		if existing.UserID == w.UserID && existing.Domain == w.Domain && existing.Status != WatchStatusAbandoned {
			return DomainWatch{}, ErrConflict
		}
	}
	w.ID = uuid.New()
	w.CreatedAt = time.Now().UTC()
	w.UpdatedAt = w.CreatedAt
	if w.Tier == "" {
		w.Tier = WatchTierFree
	}
	if w.Status == "" {
		w.Status = WatchStatusTaken
	}
	if w.Status != WatchStatusPending && w.ExpiresAt == nil {
		exp := w.CreatedAt.Add(WatchDuration)
		w.ExpiresAt = &exp
	}
	m.watches[w.ID] = w
	return w, nil
}

func (m *Memory) ListWatchesByUser(ctx context.Context, userID uuid.UUID) ([]DomainWatch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []DomainWatch
	for _, w := range m.watches {
		if w.UserID == userID && w.Status != WatchStatusAbandoned {
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (m *Memory) GetWatchByID(ctx context.Context, id uuid.UUID) (DomainWatch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.watches[id]
	if !ok {
		return DomainWatch{}, ErrNotFound
	}
	return w, nil
}

func (m *Memory) GetWatchByRazorpayOrderID(ctx context.Context, orderID string) (DomainWatch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, w := range m.watches {
		if w.RazorpayOrderID != nil && *w.RazorpayOrderID == orderID {
			return w, nil
		}
	}
	return DomainWatch{}, ErrNotFound
}

func (m *Memory) DeleteWatch(ctx context.Context, userID, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.watches[id]
	if !ok || w.UserID != userID || w.Status == WatchStatusAbandoned {
		return ErrNotFound
	}
	w.Status = WatchStatusAbandoned
	w.UpdatedAt = time.Now().UTC()
	m.watches[id] = w
	return nil
}

func watchActive(w DomainWatch, now time.Time) bool {
	if w.Status != WatchStatusTaken && w.Status != WatchStatusAvailable {
		return false
	}
	if w.ExpiresAt != nil && !w.ExpiresAt.After(now) {
		return false
	}
	return true
}

func (m *Memory) CountActiveWatchesByTier(ctx context.Context, userID uuid.UUID, tier string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	var n int64
	for _, w := range m.watches {
		if w.UserID == userID && w.Tier == tier && watchActive(w, now) {
			n++
		}
	}
	return n, nil
}

func (m *Memory) CountActivePaidWatches(ctx context.Context, userID uuid.UUID) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	var n int64
	for _, w := range m.watches {
		if w.UserID == userID && (w.Tier == WatchTierStandard || w.Tier == WatchTierPriority) && watchActive(w, now) {
			n++
		}
	}
	return n, nil
}

func (m *Memory) CountWatchersByDomain(ctx context.Context, domains []string) (map[string]int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	want := map[string]bool{}
	for _, d := range domains {
		want[d] = true
	}
	users := map[string]map[uuid.UUID]bool{}
	now := time.Now().UTC()
	for _, w := range m.watches {
		if !want[w.Domain] || !watchActive(w, now) {
			continue
		}
		if users[w.Domain] == nil {
			users[w.Domain] = map[uuid.UUID]bool{}
		}
		users[w.Domain][w.UserID] = true
	}
	out := map[string]int64{}
	for d, set := range users {
		out[d] = int64(len(set))
	}
	return out, nil
}

func (m *Memory) ListTopWatched(ctx context.Context, limit int) ([]TopWatched, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit < 1 || limit > 10 {
		limit = 10
	}
	type agg struct {
		count int64
		last  *time.Time
	}
	by := map[string]*agg{}
	users := map[string]map[uuid.UUID]bool{}
	now := time.Now().UTC()
	for _, w := range m.watches {
		if !watchActive(w, now) {
			continue
		}
		if users[w.Domain] == nil {
			users[w.Domain] = map[uuid.UUID]bool{}
			by[w.Domain] = &agg{}
		}
		if !users[w.Domain][w.UserID] {
			users[w.Domain][w.UserID] = true
			by[w.Domain].count++
		}
		if w.LastCheckedAt != nil && (by[w.Domain].last == nil || w.LastCheckedAt.After(*by[w.Domain].last)) {
			by[w.Domain].last = w.LastCheckedAt
		}
	}
	var out []TopWatched
	for d, a := range by {
		out = append(out, TopWatched{Domain: d, WatchCount: a.count, LastCheckedAt: a.last})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].WatchCount == out[j].WatchCount {
			return out[i].Domain < out[j].Domain
		}
		return out[i].WatchCount > out[j].WatchCount
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Memory) GetWatchDomainStatus(ctx context.Context, domain string) (WatchDomainStatus, error) {
	counts, _ := m.CountWatchersByDomain(ctx, []string{domain})
	m.mu.Lock()
	defer m.mu.Unlock()
	st := WatchDomainStatus{Domain: domain, WatchCount: counts[domain]}
	now := time.Now().UTC()
	for _, w := range m.watches {
		if w.Domain != domain || !watchActive(w, now) {
			continue
		}
		if w.LastCheckedAt != nil && (st.LastCheckedAt == nil || w.LastCheckedAt.After(*st.LastCheckedAt)) {
			st.LastCheckedAt = w.LastCheckedAt
		}
	}
	return st, nil
}

func (m *Memory) ActivatePaidWatch(ctx context.Context, razorpayOrderID, paymentID string) (DomainWatch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, w := range m.watches {
		if w.RazorpayOrderID != nil && *w.RazorpayOrderID == razorpayOrderID {
			if w.Status == WatchStatusPending {
				now := time.Now().UTC()
				w.Status = WatchStatusTaken
				pid := paymentID
				w.RazorpayPaymentID = &pid
				w.NextCheckAt = &now
				exp := now.Add(WatchDuration)
				w.ExpiresAt = &exp
				w.UpdatedAt = now
				m.watches[id] = w
			}
			return w, nil
		}
	}
	return DomainWatch{}, ErrNotFound
}

func (m *Memory) ExpireDueWatches(ctx context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	var n int64
	for id, w := range m.watches {
		if w.Status == WatchStatusAbandoned {
			continue
		}
		if w.ExpiresAt != nil && !w.ExpiresAt.After(now) {
			w.Status = WatchStatusAbandoned
			w.UpdatedAt = now
			m.watches[id] = w
			n++
		}
	}
	return n, nil
}

func (m *Memory) ClaimDueWatches(ctx context.Context, limit int) ([]DomainWatch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	var due []DomainWatch
	for id, w := range m.watches {
		if w.Status != WatchStatusTaken {
			continue
		}
		if w.ExpiresAt != nil && !w.ExpiresAt.After(now) {
			continue
		}
		if w.NextCheckAt != nil && w.NextCheckAt.After(now) {
			continue
		}
		next := now.Add(15 * time.Minute)
		w.NextCheckAt = &next
		w.UpdatedAt = now
		m.watches[id] = w
		due = append(due, w)
		if len(due) >= limit {
			break
		}
	}
	return due, nil
}

func (m *Memory) MarkWatchChecked(ctx context.Context, id uuid.UUID, available bool, errMsg string) (DomainWatch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.watches[id]
	if !ok {
		return DomainWatch{}, ErrNotFound
	}
	now := time.Now().UTC()
	w.LastCheckedAt = &now
	next := now.Add(WatchCheckInterval(w.Tier))
	if errMsg != "" {
		w.LastError = &errMsg
		next = now.Add(6 * time.Hour)
	} else {
		w.LastError = nil
		if available {
			w.Status = WatchStatusAvailable
			if w.NotifiedAt == nil {
				w.NotifiedAt = &now
			}
			next = now.Add(30 * 24 * time.Hour)
		} else {
			if w.Status == WatchStatusAvailable {
				w.NotifiedAt = nil
			}
			w.Status = WatchStatusTaken
		}
	}
	w.NextCheckAt = &next
	w.UpdatedAt = now
	m.watches[id] = w
	return w, nil
}

func (m *Memory) UserOwnsDomainName(ctx context.Context, userID uuid.UUID, name string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name = strings.ToLower(name)
	for _, d := range m.domains {
		if d.UserID == userID && strings.ToLower(d.Name) == name {
			return true, nil
		}
	}
	return false, nil
}

func (m *Memory) CreateComment(ctx context.Context, domain string, userID uuid.UUID, body string) (DomainComment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := DomainComment{
		ID: uuid.New(), Domain: domain, UserID: userID, Body: body, CreatedAt: time.Now().UTC(),
	}
	if u, ok := m.users[userID]; ok {
		c.Author = displayHandle(u.Email)
	}
	m.comments[c.ID] = c
	return c, nil
}

func (m *Memory) ListCommentsByDomain(ctx context.Context, domain string, limit, offset int) ([]DomainComment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit < 1 || limit > 100 {
		limit = 50
	}
	var all []DomainComment
	for _, c := range m.comments {
		if c.Domain == domain && c.HiddenAt == nil {
			if u, ok := m.users[c.UserID]; ok {
				c.Author = displayHandle(u.Email)
			}
			all = append(all, c)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	return slicePage(all, limit, offset), nil
}

func (m *Memory) GetComment(ctx context.Context, id uuid.UUID) (DomainComment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.comments[id]
	if !ok {
		return DomainComment{}, ErrNotFound
	}
	return c, nil
}

func (m *Memory) SoftDeleteComment(ctx context.Context, id, userID uuid.UUID, asAdmin bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.comments[id]
	if !ok || c.HiddenAt != nil {
		return ErrNotFound
	}
	if !asAdmin && c.UserID != userID {
		return ErrNotFound
	}
	now := time.Now().UTC()
	c.HiddenAt = &now
	m.comments[id] = c
	return nil
}

func (m *Memory) CountRecentCommentsByUser(ctx context.Context, userID uuid.UUID, since time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for _, c := range m.comments {
		if c.UserID == userID && !c.CreatedAt.Before(since) {
			n++
		}
	}
	return n, nil
}

func slicePage[T any](all []T, limit, offset int) []T {
	if offset >= len(all) {
		return nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end]
}
