package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAccessTokenRoundTrip(t *testing.T) {
	secret := "test-secret"
	id := uuid.New()
	token, err := NewAccessToken(secret, time.Hour, id, "a@b.com")
	if err != nil {
		t.Fatal(err)
	}
	gotID, email, err := ParseAccessToken(secret, token)
	if err != nil {
		t.Fatal(err)
	}
	if gotID != id {
		t.Fatalf("id: got %s want %s", gotID, id)
	}
	if email != "a@b.com" {
		t.Fatalf("email: got %s", email)
	}
}

func TestAccessTokenRejectsBadSecret(t *testing.T) {
	id := uuid.New()
	token, err := NewAccessToken("secret-a", time.Hour, id, "a@b.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ParseAccessToken("secret-b", token); err == nil {
		t.Fatal("expected signature error")
	}
}

func TestAccessTokenExpired(t *testing.T) {
	id := uuid.New()
	token, err := NewAccessToken("secret", -time.Hour, id, "a@b.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ParseAccessToken("secret", token); err == nil {
		t.Fatal("expected expired token error")
	}
}

func TestVerificationTokenHashStable(t *testing.T) {
	raw, hash, err := NewVerificationToken()
	if err != nil {
		t.Fatal(err)
	}
	if raw == "" || hash == "" {
		t.Fatal("expected non-empty token")
	}
	if HashToken(raw) != hash {
		t.Fatal("hash mismatch")
	}
	if HashToken(raw+"x") == hash {
		t.Fatal("different input should not match hash")
	}
}
