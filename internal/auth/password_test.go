package auth

import "testing"

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "password123" {
		t.Fatal("hash should not equal plaintext")
	}
	if !CheckPassword(hash, "password123") {
		t.Fatal("expected password to match")
	}
	if CheckPassword(hash, "wrong-password") {
		t.Fatal("expected wrong password to fail")
	}
}

func TestHashPasswordTooShort(t *testing.T) {
	_, err := HashPassword("short")
	if err == nil {
		t.Fatal("expected error for short password")
	}
}
