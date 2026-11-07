package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestPasswordRoundTrip(t *testing.T) {
	t.Parallel()

	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=3,p=4$") {
		t.Fatalf("unexpected encoding %q", hash)
	}

	if ok, err := VerifyPassword("correct horse battery", hash); err != nil || !ok {
		t.Fatalf("right password: ok=%v err=%v", ok, err)
	}
	if ok, err := VerifyPassword("correct horse batterY", hash); err != nil || ok {
		t.Fatalf("wrong password: ok=%v err=%v", ok, err)
	}
	if ok, _ := VerifyPassword(strings.Repeat("x", MaxPasswordBytes+1), hash); ok {
		t.Fatal("over-long password must not match")
	}

	again, _ := HashPassword("correct horse battery")
	if again == hash {
		t.Fatal("two hashes of one password must differ by salt")
	}
}

func TestValidatePassword(t *testing.T) {
	t.Parallel()

	if !errors.Is(ValidatePassword("short"), ErrPasswordTooShort) {
		t.Fatal("short password accepted")
	}
	// Padding with spaces does not make a password long enough.
	if !errors.Is(ValidatePassword("   abc     "), ErrPasswordTooShort) {
		t.Fatal("space-padded password accepted")
	}
	if !errors.Is(ValidatePassword(strings.Repeat("a", MaxPasswordBytes+1)), ErrPasswordTooLong) {
		t.Fatal("over-long password accepted")
	}
	if err := ValidatePassword("ten chars!"); err != nil {
		t.Fatalf("ten characters rejected: %v", err)
	}
}

func TestVerifyRejectsMalformedHashes(t *testing.T) {
	t.Parallel()

	for _, h := range []string{
		"",
		"plaintext",
		"$argon2i$v=19$m=65536,t=3,p=4$c2FsdA$aGFzaA",
		"$argon2id$v=18$m=65536,t=3,p=4$c2FsdA$aGFzaA",
		// Absurd parameters from a corrupted row must not be honoured.
		"$argon2id$v=19$m=99999999,t=3,p=4$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=65536,t=0,p=4$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=65536,t=3,p=4$!!$aGFzaA",
	} {
		if _, err := VerifyPassword("whatever-long", h); !errors.Is(err, ErrMalformedHash) {
			t.Errorf("%q: err = %v, want ErrMalformedHash", h, err)
		}
	}
}

func TestTokenHashIsStable(t *testing.T) {
	t.Parallel()

	token, hash, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 43 || len(hash) != 32 {
		t.Fatalf("token %d chars, hash %d bytes", len(token), len(hash))
	}
	if string(HashToken(token)) != string(hash) {
		t.Fatal("HashToken disagrees with NewToken")
	}
}

func TestCleanClient(t *testing.T) {
	t.Parallel()

	if got := CleanClient("  Safari\ton\n macOS\x00 "); got != "Safari on macOS" {
		t.Fatalf("got %q", got)
	}
	if got := CleanClient(strings.Repeat("é", 200)); len([]rune(got)) != MaxClientRunes {
		t.Fatalf("not capped: %d runes", len([]rune(got)))
	}
}

func TestSetupCode(t *testing.T) {
	t.Parallel()

	code, err := NewSetupCode()
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 14 || code[4] != '-' || code[9] != '-' {
		t.Fatalf("unexpected shape %q", code)
	}
	if strings.ContainsAny(code, "ILOU") {
		t.Fatalf("lookalike characters in %q", code)
	}

	typed := strings.ToLower(strings.ReplaceAll(code, "-", " "))
	if !SetupCodeMatches(typed, code) {
		t.Fatal("case and separators should not matter")
	}
	if SetupCodeMatches("AAAA-AAAA-AAAA", code) {
		t.Fatal("wrong code matched")
	}
	if SetupCodeMatches("", "") {
		t.Fatal("no issued code must never match")
	}
}
