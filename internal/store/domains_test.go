package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestDomainCRUDMemory(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	u, err := m.CreateUser(ctx, "owner@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	d, err := m.CreateDomain(ctx, Domain{
		UserID:    u.ID,
		Name:      "acme.test",
		Status:    "active",
		Locked:    true,
		AutoRenew: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.ID == uuid.Nil || !d.AutoRenew {
		t.Fatalf("unexpected domain %#v", d)
	}

	list, err := m.ListDomainsByUser(ctx, u.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("list %#v err=%v", list, err)
	}

	got, err := m.GetDomainForUser(ctx, u.ID, d.ID)
	if err != nil || got.Name != "acme.test" {
		t.Fatalf("get %#v err=%v", got, err)
	}

	off := false
	updated, err := m.UpdateDomainFlags(ctx, u.ID, d.ID, nil, &off)
	if err != nil || updated.AutoRenew {
		t.Fatalf("update %#v err=%v", updated, err)
	}

	other := uuid.New()
	if _, err := m.GetDomainForUser(ctx, other, d.ID); err != ErrNotFound {
		t.Fatalf("expected not found for other user, got %v", err)
	}
}
