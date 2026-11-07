// Package domain holds the room's password, session and setup-code rules.
package domain

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters: RFC 9106's second recommended option, sized for a small
// host. They are written into every hash, so raising them later only affects
// passwords set afterwards.
const (
	argonTime      = 3
	argonMemoryKiB = 64 * 1024
	argonThreads   = 4
	argonKeyLen    = 32
	argonSaltLen   = 16

	// Ceilings on parameters read back from a stored hash, so a corrupted row
	// cannot ask for gigabytes of memory.
	maxArgonTime      = 10
	maxArgonMemoryKiB = 1024 * 1024
	maxArgonThreads   = 16
	maxArgonKeyLen    = 64
)

// MinPasswordRunes is the shortest password the room accepts. The login is
// rate limited, but the password is the only thing between the internet and
// the journal.
const MinPasswordRunes = 10

// MaxPasswordBytes bounds the input to the hash.
const MaxPasswordBytes = 1024

var (
	ErrPasswordTooShort = fmt.Errorf("password must be at least %d characters", MinPasswordRunes)
	ErrPasswordTooLong  = fmt.Errorf("password must be at most %d bytes", MaxPasswordBytes)
	ErrMalformedHash    = errors.New("malformed password hash")
)

// ValidatePassword checks a new password before it is hashed.
func ValidatePassword(password string) error {
	if len(password) > MaxPasswordBytes {
		return ErrPasswordTooLong
	}
	if utf8.RuneCountInString(strings.TrimSpace(password)) < MinPasswordRunes {
		return ErrPasswordTooShort
	}
	return nil
}

// HashPassword returns an argon2id PHC string:
// $argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>.
func HashPassword(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword reports whether password matches an encoded hash, in
// constant time. An over-long password is a mismatch, not an error.
func VerifyPassword(password, encoded string) (bool, error) {
	p, salt, want, err := decodeHash(encoded)
	if err != nil {
		return false, err
	}
	if len(password) > MaxPasswordBytes {
		return false, nil
	}
	got := argon2.IDKey([]byte(password), salt, p.time, p.memory, p.threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

type argonParams struct {
	time    uint32
	memory  uint32
	threads uint8
}

func decodeHash(encoded string) (argonParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return argonParams{}, nil, nil, ErrMalformedHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return argonParams{}, nil, nil, ErrMalformedHash
	}

	var p argonParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.threads); err != nil {
		return argonParams{}, nil, nil, ErrMalformedHash
	}
	if p.time == 0 || p.time > maxArgonTime ||
		p.memory == 0 || p.memory > maxArgonMemoryKiB ||
		p.threads == 0 || p.threads > maxArgonThreads {
		return argonParams{}, nil, nil, ErrMalformedHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return argonParams{}, nil, nil, ErrMalformedHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 || len(key) > maxArgonKeyLen {
		return argonParams{}, nil, nil, ErrMalformedHash
	}
	return p, salt, key, nil
}
