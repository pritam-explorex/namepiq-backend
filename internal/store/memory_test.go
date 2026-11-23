package store

import (
	"context"
	"testing"
	"time"

	"github.com/namepiq/api/internal/auth"
)

func TestMemoryUserLifecycle(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()

	u, err := m.CreateUser(ctx, "a@b.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.CreateUser(ctx, "a@b.com", "hash"); err != ErrConflict {
		t.Fatalf("expected conflict, got %v", err)
	}

	got, err := m.GetUserByEmail(ctx, "a@b.com")
	if err != nil || got.ID != u.ID {
		t.Fatalf("get by email: %#v %v", got, err)
	}

	raw, hash, err := auth.NewVerificationToken()
	if err != nil {
		t.Fatal(err)
	}
	_ = raw
	if err := m.CreateEmailVerification(ctx, u.ID, hash, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	uid, err := m.ConsumeEmailVerification(ctx, hash)
	if err != nil || uid != u.ID {
		t.Fatalf("consume: %v %s", err, uid)
	}
	if _, err := m.ConsumeEmailVerification(ctx, hash); err != ErrNotFound {
		t.Fatalf("expected second consume to fail, got %v", err)
	}

	verified, err := m.GetUserByID(ctx, u.ID)
	if err != nil || verified.EmailVerifiedAt == nil {
		t.Fatalf("expected verified user, got %#v %v", verified, err)
	}

	c := Contact{UserID: u.ID, FirstName: "Ada", Country: "IN"}
	if err := m.UpsertContact(ctx, c); err != nil {
		t.Fatal(err)
	}
	gotC, err := m.GetContact(ctx, u.ID)
	if err != nil || gotC.FirstName != "Ada" {
		t.Fatalf("contact: %#v %v", gotC, err)
	}
}
