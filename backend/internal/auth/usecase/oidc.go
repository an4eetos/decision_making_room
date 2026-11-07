package usecase

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/an4eetos/decision-room/internal/auth/domain"
	"github.com/an4eetos/decision-room/internal/auth/port"
)

var (
	ErrLoginExpired       = errors.New("this sign-in expired or was already used; start again")
	ErrNotAllowed         = errors.New("that account is not allowed into this room")
	ErrUnknownAppRedirect = errors.New("unknown app redirect")
	ErrBadChallenge       = errors.New("code_challenge must be a base64url S256 challenge")
	ErrTooManyLogins      = errors.New("too many sign-ins in progress; try again shortly")
)

const (
	// pendingTTL is how long the provider's consent screen may take.
	pendingTTL = 10 * time.Minute
	// handoffTTL is how long the app has to redeem its code: it is redeemed
	// the instant the app reopens.
	handoffTTL = 2 * time.Minute
	// maxPending bounds the in-memory maps; starting a sign-in is public.
	maxPending = 256
)

// OIDC signs the owner in through an OpenID Connect provider. In-flight
// sign-ins are held in memory: the room is one process, and a restart in the
// middle of one only means clicking the button again.
type OIDC struct {
	sessions     *Service
	idp          port.IdentityProvider
	allowed      map[string]bool
	appRedirects map[string]bool
	now          func() time.Time

	mu       sync.Mutex
	pending  map[string]pendingLogin // by state
	handoffs map[string]handoff      // by SHA-256 of the code
}

type pendingLogin struct {
	nonce        string
	verifier     string
	next         string
	appRedirect  string
	appChallenge string
	created      time.Time
}

type handoff struct {
	challenge string
	email     string
	created   time.Time
}

func NewOIDC(sessions *Service, idp port.IdentityProvider, allowedEmails, appRedirects []string) *OIDC {
	o := &OIDC{
		sessions:     sessions,
		idp:          idp,
		allowed:      make(map[string]bool, len(allowedEmails)),
		appRedirects: make(map[string]bool, len(appRedirects)),
		now:          time.Now,
		pending:      make(map[string]pendingLogin),
		handoffs:     make(map[string]handoff),
	}
	for _, e := range allowedEmails {
		o.allowed[strings.ToLower(strings.TrimSpace(e))] = true
	}
	for _, r := range appRedirects {
		o.appRedirects[r] = true
	}
	return o
}

// StartRequest begins a sign-in. A browser sets Next; the app sets
// AppRedirect and its own PKCE challenge instead.
type StartRequest struct {
	Next         string
	AppRedirect  string
	AppChallenge string
}

// Start returns the provider URL to send the browser to, and the state the
// caller binds to that browser.
func (o *OIDC) Start(ctx context.Context, req StartRequest) (authURL, state string, err error) {
	if req.AppRedirect != "" {
		if !o.appRedirects[req.AppRedirect] {
			return "", "", ErrUnknownAppRedirect
		}
		if !validChallenge(req.AppChallenge) {
			return "", "", ErrBadChallenge
		}
	}

	state, err = randomString()
	if err != nil {
		return "", "", err
	}
	nonce, err := randomString()
	if err != nil {
		return "", "", err
	}
	verifier, err := randomString()
	if err != nil {
		return "", "", err
	}

	o.mu.Lock()
	o.prune()
	if len(o.pending) >= maxPending {
		o.mu.Unlock()
		return "", "", ErrTooManyLogins
	}
	o.pending[state] = pendingLogin{
		nonce:        nonce,
		verifier:     verifier,
		next:         LocalPath(req.Next),
		appRedirect:  req.AppRedirect,
		appChallenge: req.AppChallenge,
		created:      o.now(),
	}
	o.mu.Unlock()

	authURL, err = o.idp.AuthCodeURL(ctx, state, nonce, verifier)
	if err != nil {
		o.mu.Lock()
		delete(o.pending, state)
		o.mu.Unlock()
		return "", "", err
	}
	return authURL, state, nil
}

// CallbackResult is a browser session, or a redirect back into the app.
type CallbackResult struct {
	Token   string
	Session domain.Session
	Next    string
	// AppRedirect, when set, is the app URL carrying the one-time code; no
	// session has been opened yet.
	AppRedirect string
}

// Callback finishes the sign-in the provider sent the browser back from.
func (o *OIDC) Callback(ctx context.Context, state, code, client string) (CallbackResult, error) {
	o.mu.Lock()
	p, ok := o.pending[state]
	delete(o.pending, state)
	o.mu.Unlock()
	if !ok || o.now().Sub(p.created) > pendingTTL {
		return CallbackResult{}, ErrLoginExpired
	}

	id, err := o.idp.Exchange(ctx, code, p.verifier, p.nonce)
	if err != nil {
		return CallbackResult{}, err
	}
	email := strings.ToLower(strings.TrimSpace(id.Email))
	if !id.EmailVerified || !o.allowed[email] {
		// The address goes to the log so the owner can see a typo in the
		// allow-list; the browser is told nothing about which check failed.
		log.Printf("auth: oidc sign-in refused for %q (verified=%t)", email, id.EmailVerified)
		return CallbackResult{}, ErrNotAllowed
	}

	if p.appRedirect != "" {
		handoffCode, err := randomString()
		if err != nil {
			return CallbackResult{}, err
		}
		o.mu.Lock()
		o.prune()
		o.handoffs[hashString(handoffCode)] = handoff{challenge: p.appChallenge, email: email, created: o.now()}
		o.mu.Unlock()

		u, err := url.Parse(p.appRedirect)
		if err != nil {
			return CallbackResult{}, fmt.Errorf("app redirect: %w", err)
		}
		q := u.Query()
		q.Set("code", handoffCode)
		u.RawQuery = q.Encode()
		return CallbackResult{AppRedirect: u.String()}, nil
	}

	token, sess, err := o.sessions.IssueSession(ctx, client)
	if err != nil {
		return CallbackResult{}, err
	}
	log.Printf("auth: %s signed in through oidc", email)
	return CallbackResult{Token: token, Session: sess, Next: p.next}, nil
}

// Exchange redeems the app's one-time code. The verifier proves the caller is
// the app that started the sign-in, not whatever else intercepted the
// redirect.
func (o *OIDC) Exchange(ctx context.Context, code, verifier, client string) (string, domain.Session, error) {
	key := hashString(code)
	o.mu.Lock()
	h, ok := o.handoffs[key]
	delete(o.handoffs, key)
	o.mu.Unlock()
	if !ok || o.now().Sub(h.created) > handoffTTL {
		return "", domain.Session{}, ErrLoginExpired
	}
	if subtle.ConstantTimeCompare([]byte(s256(verifier)), []byte(h.challenge)) != 1 {
		return "", domain.Session{}, ErrLoginExpired
	}

	token, sess, err := o.sessions.IssueSession(ctx, client)
	if err != nil {
		return "", domain.Session{}, err
	}
	log.Printf("auth: %s signed in through oidc (app)", h.email)
	return token, sess, nil
}

// prune drops expired entries; the caller holds the lock.
func (o *OIDC) prune() {
	now := o.now()
	for k, p := range o.pending {
		if now.Sub(p.created) > pendingTTL {
			delete(o.pending, k)
		}
	}
	for k, h := range o.handoffs {
		if now.Sub(h.created) > handoffTTL {
			delete(o.handoffs, k)
		}
	}
}

// LocalPath keeps a post-sign-in destination on this site: a path, never
// "//host" or a full URL.
func LocalPath(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return "/"
	}
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" {
		return "/"
	}
	return next
}

// validChallenge accepts an S256 challenge: 43 base64url characters.
func validChallenge(c string) bool {
	if len(c) != 43 {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(c)
	return err == nil
}

func s256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return string(sum[:])
}

// randomString is 256 bits, base64url: 43 characters, which is also a valid
// PKCE verifier.
func randomString() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
