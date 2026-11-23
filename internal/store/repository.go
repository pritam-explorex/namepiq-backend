package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Repository is the persistence surface used by the HTTP API.
type Repository interface {
	Ping(ctx context.Context) error
	CreateUser(ctx context.Context, email, passwordHash string) (User, error)
	GetUserByEmail(ctx context.Context, email string) (User, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (User, error)
	UpdateUserLocale(ctx context.Context, id uuid.UUID, currency, country string) (User, error)
	CreateEmailVerification(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) error
	ConsumeEmailVerification(ctx context.Context, tokenHash string) (uuid.UUID, error)
	UpsertContact(ctx context.Context, c Contact) error
	GetContact(ctx context.Context, userID uuid.UUID) (Contact, error)
	InsertAudit(ctx context.Context, userID *uuid.UUID, ip, action string, meta map[string]any) error
	ListDomainsByUser(ctx context.Context, userID uuid.UUID) ([]Domain, error)
	GetDomainForUser(ctx context.Context, userID, domainID uuid.UUID) (Domain, error)
	CreateDomain(ctx context.Context, d Domain) (Domain, error)
	UpdateDomainFlags(ctx context.Context, userID, domainID uuid.UUID, locked, autoRenew *bool) (Domain, error)
	UpdateDomainExpiry(ctx context.Context, userID, domainID uuid.UUID, expiresAt *time.Time, autoRenew *bool) (Domain, error)

	CreateOrders(ctx context.Context, rows []NewOrder, razorpayOrderID string) ([]Order, error)
	ListOrdersByRazorpayOrderID(ctx context.Context, razorpayOrderID string) ([]Order, error)
	ListOrdersByCheckoutID(ctx context.Context, checkoutID uuid.UUID) ([]Order, error)
	MarkOrdersPaid(ctx context.Context, razorpayOrderID, paymentID string) (int64, error)
	UpdateOrderStatus(ctx context.Context, id uuid.UUID, status string) error
	GetActivePendingCheckout(ctx context.Context, userID uuid.UUID, since time.Time) (PendingCheckout, bool, error)
	AbandonPendingCheckouts(ctx context.Context, userID uuid.UUID) (int64, error)

	CreateWatch(ctx context.Context, w DomainWatch) (DomainWatch, error)
	ListWatchesByUser(ctx context.Context, userID uuid.UUID) ([]DomainWatch, error)
	GetWatchByID(ctx context.Context, id uuid.UUID) (DomainWatch, error)
	GetWatchByRazorpayOrderID(ctx context.Context, orderID string) (DomainWatch, error)
	DeleteWatch(ctx context.Context, userID, id uuid.UUID) error
	CountActiveWatchesByTier(ctx context.Context, userID uuid.UUID, tier string) (int64, error)
	CountActivePaidWatches(ctx context.Context, userID uuid.UUID) (int64, error)
	CountWatchersByDomain(ctx context.Context, domains []string) (map[string]int64, error)
	ListTopWatched(ctx context.Context, limit int) ([]TopWatched, error)
	GetWatchDomainStatus(ctx context.Context, domain string) (WatchDomainStatus, error)
	ActivatePaidWatch(ctx context.Context, razorpayOrderID, paymentID string) (DomainWatch, error)
	ClaimDueWatches(ctx context.Context, limit int) ([]DomainWatch, error)
	ExpireDueWatches(ctx context.Context) (int64, error)
	MarkWatchChecked(ctx context.Context, id uuid.UUID, available bool, errMsg string) (DomainWatch, error)
	UserOwnsDomainName(ctx context.Context, userID uuid.UUID, name string) (bool, error)

	CreateComment(ctx context.Context, domain string, userID uuid.UUID, body string) (DomainComment, error)
	ListCommentsByDomain(ctx context.Context, domain string, limit, offset int) ([]DomainComment, error)
	GetComment(ctx context.Context, id uuid.UUID) (DomainComment, error)
	SoftDeleteComment(ctx context.Context, id, userID uuid.UUID, asAdmin bool) error
	CountRecentCommentsByUser(ctx context.Context, userID uuid.UUID, since time.Time) (int64, error)

	AdminStats(ctx context.Context) (AdminStats, error)
	ListUsersAdmin(ctx context.Context, q string, limit, offset int) ([]AdminUserRow, error)
	ListDomainsAdmin(ctx context.Context, q, status string, limit, offset int) ([]AdminDomainRow, error)
	ListOrdersAdmin(ctx context.Context, status string, limit, offset int) ([]Order, error)
	ListAuditAdmin(ctx context.Context, limit, offset int) ([]AuditEvent, error)
	PatchUserAdmin(ctx context.Context, id uuid.UUID, role *string, forceVerify bool) (User, error)
	PromoteAdminsByEmail(ctx context.Context, emails []string) (int64, error)
}
