// Package oidc signs the owner in through any OpenID Connect provider.
package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/an4eetos/decision-room/internal/auth/port"
)

// httpTimeout bounds discovery, key fetches and the code exchange.
const httpTimeout = 15 * time.Second

// Provider discovers the issuer on first use rather than at boot, so a
// provider outage does not stop the room from starting, and password sign-in
// keeps working through one.
type Provider struct {
	issuer       string
	clientID     string
	clientSecret string
	redirectURL  string
	client       *http.Client

	mu       sync.Mutex
	oauth    *oauth2.Config
	verifier *gooidc.IDTokenVerifier
}

func NewProvider(issuer, clientID, clientSecret, redirectURL string) *Provider {
	return &Provider{
		issuer:       issuer,
		clientID:     clientID,
		clientSecret: clientSecret,
		redirectURL:  redirectURL,
		client:       &http.Client{Timeout: httpTimeout},
	}
}

func (p *Provider) AuthCodeURL(ctx context.Context, state, nonce, verifier string) (string, error) {
	cfg, _, err := p.discover()
	if err != nil {
		return "", err
	}
	return cfg.AuthCodeURL(state, gooidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), nil
}

func (p *Provider) Exchange(ctx context.Context, code, verifier, nonce string) (port.Identity, error) {
	cfg, idVerifier, err := p.discover()
	if err != nil {
		return port.Identity{}, err
	}

	ctx = context.WithValue(ctx, oauth2.HTTPClient, p.client)
	tok, err := cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return port.Identity{}, fmt.Errorf("oidc code exchange: %w", err)
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return port.Identity{}, errors.New("oidc: provider returned no id_token")
	}

	idToken, err := idVerifier.Verify(ctx, raw)
	if err != nil {
		return port.Identity{}, fmt.Errorf("oidc: verify id_token: %w", err)
	}
	if idToken.Nonce != nonce {
		return port.Identity{}, errors.New("oidc: id_token nonce mismatch")
	}

	var claims struct {
		Email         string          `json:"email"`
		EmailVerified json.RawMessage `json:"email_verified"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return port.Identity{}, fmt.Errorf("oidc: read claims: %w", err)
	}
	return port.Identity{
		Subject:       idToken.Subject,
		Email:         claims.Email,
		EmailVerified: isTrue(claims.EmailVerified),
	}, nil
}

func (p *Provider) discover() (*oauth2.Config, *gooidc.IDTokenVerifier, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.oauth != nil {
		return p.oauth, p.verifier, nil
	}

	// go-oidc keeps this context for fetching signing keys later, so it must
	// outlive the request: a background context carrying the timed client.
	ctx := gooidc.ClientContext(context.Background(), p.client)
	provider, err := gooidc.NewProvider(ctx, p.issuer)
	if err != nil {
		return nil, nil, fmt.Errorf("oidc discovery for %s: %w", p.issuer, err)
	}

	p.oauth = &oauth2.Config{
		ClientID:     p.clientID,
		ClientSecret: p.clientSecret,
		RedirectURL:  p.redirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{gooidc.ScopeOpenID, "email"},
	}
	p.verifier = provider.Verifier(&gooidc.Config{ClientID: p.clientID})
	return p.oauth, p.verifier, nil
}

// isTrue reads email_verified, which some providers send as the string "true".
func isTrue(raw json.RawMessage) bool {
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return b
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s == "true"
	}
	return false
}
