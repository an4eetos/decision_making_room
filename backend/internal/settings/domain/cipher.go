package domain

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// MinKeyLength is the shortest SETTINGS_KEY accepted. `openssl rand -base64
// 32` gives 44 characters.
const MinKeyLength = 32

// sealedPrefix versions the format, so the scheme can change without
// guessing what an old row holds.
const sealedPrefix = "v1:"

var (
	ErrNoSettingsKey = errors.New("SETTINGS_KEY is not set, so secrets cannot be stored: set it on the server (openssl rand -base64 32) and restart")
	ErrShortKey      = fmt.Errorf("SETTINGS_KEY must be at least %d characters", MinKeyLength)
	ErrUndecryptable = errors.New("stored under a different SETTINGS_KEY")
)

// Cipher seals secrets with AES-256-GCM. The encryption key is derived from
// SETTINGS_KEY with HKDF, so any long random string works as the variable.
// The setting's name is the associated data: a sealed Gemini key copied onto
// the Anthropic row does not open.
type Cipher struct {
	aead cipher.AEAD
}

func NewCipher(settingsKey string) (*Cipher, error) {
	if settingsKey == "" {
		return nil, ErrNoSettingsKey
	}
	if len(settingsKey) < MinKeyLength {
		return nil, ErrShortKey
	}
	key, err := hkdf.Key(sha256.New, []byte(settingsKey), nil, "decision-room settings v1", 32)
	if err != nil {
		return nil, fmt.Errorf("derive settings key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("settings cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("settings cipher: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Seal returns "v1:" and base64 of nonce then ciphertext.
func (c *Cipher) Seal(name, plaintext string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("settings nonce: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), []byte(name))
	return sealedPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Open reverses Seal. A wrong key, a tampered value or a value moved to
// another name all fail as ErrUndecryptable.
func (c *Cipher) Open(name, sealed string) (string, error) {
	raw, ok := strings.CutPrefix(sealed, sealedPrefix)
	if !ok {
		return "", ErrUndecryptable
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(data) < c.aead.NonceSize() {
		return "", ErrUndecryptable
	}
	nonce, ct := data[:c.aead.NonceSize()], data[c.aead.NonceSize():]
	plain, err := c.aead.Open(nil, nonce, ct, []byte(name))
	if err != nil {
		return "", ErrUndecryptable
	}
	return string(plain), nil
}

// Mask shows the last four characters of a secret, enough to tell two keys
// apart and nothing more.
func Mask(secret string) string {
	if len(secret) <= 8 {
		return "••••"
	}
	return "••••" + secret[len(secret)-4:]
}
