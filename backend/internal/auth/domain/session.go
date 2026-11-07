package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// SessionTTL is how long a device stays signed in without being used. Every
// use pushes the expiry forward, so a phone in daily use never signs out.
const SessionTTL = 90 * 24 * time.Hour

// TouchEvery is how stale last_seen_at may get before a request refreshes it,
// so an authenticated request is one indexed read, not a read and a write.
const TouchEvery = 5 * time.Minute

// MaxClientRunes matches the client column.
const MaxClientRunes = 80

// tokenBytes is the session token's entropy: 256 bits.
const tokenBytes = 32

// Session is one signed-in device.
type Session struct {
	ID         uuid.UUID
	TokenHash  []byte
	Client     string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
}

// NewToken returns a fresh session token and the hash that is stored for it.
func NewToken() (token string, hash []byte, err error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("session token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, HashToken(token), nil
}

// HashToken is SHA-256. The token is 256 random bits, so a fast hash is
// enough: there is nothing to guess, only a database row to not leak.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// CleanClient makes a client-supplied device name safe to store and show:
// control characters dropped, whitespace collapsed, length capped.
func CleanClient(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		case unicode.IsControl(r):
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if runes := []rune(out); len(runes) > MaxClientRunes {
		out = strings.TrimSpace(string(runes[:MaxClientRunes]))
	}
	return out
}

// setupAlphabet is Crockford's base32: no I, L, O or U, so a code read off a
// log line cannot be mistyped as a lookalike.
const setupAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// setupCodeLen gives 60 bits, behind the login rate limit.
const setupCodeLen = 12

// NewSetupCode returns a one-time code, grouped as XXXX-XXXX-XXXX.
func NewSetupCode() (string, error) {
	raw := make([]byte, setupCodeLen)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("setup code: %w", err)
	}
	var b strings.Builder
	for i, v := range raw {
		if i > 0 && i%4 == 0 {
			b.WriteByte('-')
		}
		// 256 is a multiple of 32, so the modulo is unbiased.
		b.WriteByte(setupAlphabet[int(v)%len(setupAlphabet)])
	}
	return b.String(), nil
}

// SetupCodeMatches compares a typed code against the issued one in constant
// time, ignoring case, dashes and spaces.
func SetupCodeMatches(typed, issued string) bool {
	a, b := normalizeSetupCode(typed), normalizeSetupCode(issued)
	if b == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func normalizeSetupCode(code string) string {
	code = strings.ToUpper(code)
	return strings.Map(func(r rune) rune {
		if r == '-' || unicode.IsSpace(r) {
			return -1
		}
		return r
	}, code)
}
