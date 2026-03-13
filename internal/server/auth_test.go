package server

import "testing"

func TestGenerateNumericCode(t *testing.T) {
	code, err := generateNumericCode(6)
	if err != nil {
		t.Fatalf("generate code failed: %v", err)
	}
	if len(code) != 6 {
		t.Fatalf("expected length 6, got %d", len(code))
	}
	for _, ch := range code {
		if ch < '0' || ch > '9' {
			t.Fatalf("code should contain digits only, got %q", ch)
		}
	}
}

func TestHashVerificationCodeDeterministic(t *testing.T) {
	h1 := hashVerificationCode("123456", "a@example.com", "register")
	h2 := hashVerificationCode("123456", "a@example.com", "register")
	h3 := hashVerificationCode("123456", "b@example.com", "register")
	if h1 != h2 {
		t.Fatal("same input should produce same hash")
	}
	if h1 == h3 {
		t.Fatal("different input should produce different hash")
	}
}

func TestValidatePassword(t *testing.T) {
	if err := validatePassword("1234567"); err == nil {
		t.Fatal("expected short password to be rejected")
	}
	if err := validatePassword("12345678"); err != nil {
		t.Fatalf("expected valid password, got %v", err)
	}
}

func TestGenerateSessionToken(t *testing.T) {
	token, tokenHash, err := generateSessionToken()
	if err != nil {
		t.Fatalf("generate session token failed: %v", err)
	}
	if token == "" || tokenHash == "" {
		t.Fatal("token and hash should not be empty")
	}
	if hashSessionToken(token) != tokenHash {
		t.Fatal("token hash mismatch")
	}
}
