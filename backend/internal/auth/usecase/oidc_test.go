package usecase

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/an4eetos/decision-room/internal/auth/authtest"
	"github.com/an4eetos/decision-room/internal/auth/port"
)

const appRedirect = "decisionroom://auth"

func newOIDC(t *testing.T, id port.Identity) (*OIDC, *Service, *authtest.IdentityProvider, *time.Time) {
	t.Helper()
	svc, _, clock := newService(t, true)
	idp := &authtest.IdentityProvider{Identity: id}
	o := NewOIDC(svc, idp, []string{"Owner@Example.com"}, []string{appRedirect})
	o.now = func() time.Time { return *clock }
	return o, svc, idp, clock
}

var owner = port.Identity{Subject: "123", Email: "owner@example.com", EmailVerified: true}

func TestOIDCBrowserSignIn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	o, svc, idp, _ := newOIDC(t, owner)

	_, state, err := o.Start(ctx, StartRequest{Next: "/campaign?tab=map"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := o.Callback(ctx, state, "provider-code", "Safari")
	if err != nil {
		t.Fatalf("Callback: %v", err)
	}
	if res.AppRedirect != "" || res.Next != "/campaign?tab=map" {
		t.Fatalf("result %+v", res)
	}
	// The verifier and nonce sent to the provider are the ones issued at start.
	if idp.LastVerifier != idp.Verifiers[state] || idp.LastNonce != idp.Nonces[state] {
		t.Fatal("exchange did not use the start's verifier and nonce")
	}
	if _, err := svc.Authenticate(ctx, res.Token); err != nil {
		t.Fatalf("issued token does not authenticate: %v", err)
	}

	// A state is good once.
	if _, err := o.Callback(ctx, state, "provider-code", "Safari"); !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("replayed state: %v", err)
	}
}

func TestOIDCRefusesOtherAccounts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for name, id := range map[string]port.Identity{
		"stranger":   {Subject: "9", Email: "someone@example.com", EmailVerified: true},
		"unverified": {Subject: "1", Email: "owner@example.com", EmailVerified: false},
		"no email":   {Subject: "1"},
	} {
		o, _, _, _ := newOIDC(t, id)
		_, state, err := o.Start(ctx, StartRequest{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := o.Callback(ctx, state, "c", "x"); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("%s: err = %v, want ErrNotAllowed", name, err)
		}
	}
}

func TestOIDCPendingExpires(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	o, _, _, clock := newOIDC(t, owner)

	_, state, _ := o.Start(ctx, StartRequest{})
	*clock = clock.Add(pendingTTL + time.Second)
	if _, err := o.Callback(ctx, state, "c", "x"); !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("stale state: %v", err)
	}
	if _, err := o.Callback(ctx, "never-issued", "c", "x"); !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("unknown state: %v", err)
	}
}

func TestOIDCAppHandoff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	o, svc, _, clock := newOIDC(t, owner)

	verifier := "app-held-verifier-0123456789-abcdefghijklmnop"
	challenge := s256(verifier)

	if _, _, err := o.Start(ctx, StartRequest{AppRedirect: "https://evil.example/cb", AppChallenge: challenge}); !errors.Is(err, ErrUnknownAppRedirect) {
		t.Fatalf("foreign redirect: %v", err)
	}
	if _, _, err := o.Start(ctx, StartRequest{AppRedirect: appRedirect, AppChallenge: "plain"}); !errors.Is(err, ErrBadChallenge) {
		t.Fatalf("plain challenge: %v", err)
	}

	_, state, err := o.Start(ctx, StartRequest{AppRedirect: appRedirect, AppChallenge: challenge})
	if err != nil {
		t.Fatal(err)
	}
	res, err := o.Callback(ctx, state, "c", "x")
	if err != nil {
		t.Fatal(err)
	}
	if res.Token != "" {
		t.Fatal("app flow must not open a session in the browser")
	}
	u, err := url.Parse(res.AppRedirect)
	if err != nil || u.Scheme != "decisionroom" {
		t.Fatalf("redirect %q", res.AppRedirect)
	}
	code := u.Query().Get("code")

	// Without the verifier the code is worthless, and spent anyway.
	if _, _, err := o.Exchange(ctx, code, "wrong-verifier", "Pixel"); !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("wrong verifier: %v", err)
	}
	if _, _, err := o.Exchange(ctx, code, verifier, "Pixel"); !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("code reused after a failed attempt: %v", err)
	}

	// A fresh round with the right verifier signs the app in.
	_, state, _ = o.Start(ctx, StartRequest{AppRedirect: appRedirect, AppChallenge: challenge})
	res, _ = o.Callback(ctx, state, "c", "x")
	u, _ = url.Parse(res.AppRedirect)
	token, sess, err := o.Exchange(ctx, u.Query().Get("code"), verifier, "Pixel")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if sess.Client != "Pixel" {
		t.Fatalf("client %q", sess.Client)
	}
	if _, err := svc.Authenticate(ctx, token); err != nil {
		t.Fatal(err)
	}

	// Codes expire fast.
	_, state, _ = o.Start(ctx, StartRequest{AppRedirect: appRedirect, AppChallenge: challenge})
	res, _ = o.Callback(ctx, state, "c", "x")
	u, _ = url.Parse(res.AppRedirect)
	*clock = clock.Add(handoffTTL + time.Second)
	if _, _, err := o.Exchange(ctx, u.Query().Get("code"), verifier, "Pixel"); !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("stale code: %v", err)
	}
}

func TestOIDCPropagatesProviderErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	o, _, idp, _ := newOIDC(t, owner)
	idp.Err = errors.New("id_token signature invalid")

	_, state, _ := o.Start(ctx, StartRequest{})
	if _, err := o.Callback(ctx, state, "c", "x"); err == nil || errors.Is(err, ErrNotAllowed) {
		t.Fatalf("provider error not surfaced: %v", err)
	}
}

func TestLocalPath(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"":                     "/",
		"/campaign":            "/campaign",
		"/memories?q=x":        "/memories?q=x",
		"//evil.example":       "/",
		"/\\evil.example":      "/",
		"https://evil.example": "/",
		"campaign":             "/",
	} {
		if got := LocalPath(in); got != want {
			t.Errorf("LocalPath(%q) = %q, want %q", in, got, want)
		}
	}
}
