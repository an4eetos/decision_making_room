package authapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/an4eetos/decision-room/internal/auth/authtest"
	"github.com/an4eetos/decision-room/internal/auth/port"
	"github.com/an4eetos/decision-room/internal/auth/usecase"
	httpserver "github.com/an4eetos/decision-room/internal/infra/http"
)

const password = "a long enough password"

type authAdapter struct{ svc *usecase.Service }

func (a authAdapter) Authenticate(ctx context.Context, token string) (string, error) {
	s, err := a.svc.Authenticate(ctx, token)
	if err != nil {
		return "", httpserver.ErrUnauthenticated
	}
	return s.ID.String(), nil
}

// room is the auth routes plus one private route, behind the real guard.
func room(t *testing.T) (http.Handler, *usecase.Service, *authtest.IdentityProvider) {
	t.Helper()
	svc := usecase.NewService(authtest.NewRepository(), true)
	if err := svc.Bootstrap(context.Background(), password); err != nil {
		t.Fatal(err)
	}
	idp := &authtest.IdentityProvider{Identity: port.Identity{Email: "owner@example.com", EmailVerified: true}}
	oidc := usecase.NewOIDC(svc, idp, []string{"owner@example.com"}, []string{"decisionroom://auth"})

	mux := http.NewServeMux()
	NewHandler(svc, oidc, "Google").Register(mux)
	mux.HandleFunc("GET /api/private", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("secret")) })
	return httpserver.RequireAuth(authAdapter{svc}, mux), svc, idp
}

func do(h http.Handler, method, target, body string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://room.test"+target, strings.NewReader(body))
	req.RemoteAddr = "198.51.100.7:1234"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if mutate != nil {
		mutate(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func sessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == httpserver.SessionCookie {
			return c
		}
	}
	return nil
}

func TestBrowserLoginUsesCookieAndHidesToken(t *testing.T) {
	t.Parallel()
	h, _, _ := room(t)

	rec := do(h, "POST", "/api/auth/login", `{"password":"`+password+`","cookie":true}`, nil)
	if rec.Code != 200 {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), `"token"`) {
		t.Fatal("cookie login leaked the token to script")
	}
	c := sessionCookie(rec)
	if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie %+v", c)
	}
	if c.Secure {
		t.Fatal("Secure set on plain http; the browser would drop it")
	}

	withCookie := func(r *http.Request) { r.AddCookie(c) }
	if rec := do(h, "GET", "/api/private", "", withCookie); rec.Code != 200 {
		t.Fatalf("private with cookie: %d", rec.Code)
	}

	rec = do(h, "GET", "/api/auth/sessions", "", withCookie)
	var sessions []sessionDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &sessions)
	if len(sessions) != 1 || !sessions[0].Current {
		t.Fatalf("sessions %s", rec.Body)
	}

	if rec := do(h, "POST", "/api/auth/logout", "", withCookie); rec.Code != 204 {
		t.Fatalf("logout: %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/private", "", withCookie); rec.Code != 401 {
		t.Fatalf("after logout: %d", rec.Code)
	}
}

func TestAppLoginGetsToken(t *testing.T) {
	t.Parallel()
	h, _, _ := room(t)

	rec := do(h, "POST", "/api/auth/login", `{"password":"`+password+`","client":"Pixel 9"}`, nil)
	var resp signInResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if rec.Code != 200 || resp.Token == "" || resp.Session.Client != "Pixel 9" {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	if sessionCookie(rec) != nil {
		t.Fatal("token login also set a cookie")
	}
	bearer := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+resp.Token) }
	if rec := do(h, "GET", "/api/private", "", bearer); rec.Code != 200 {
		t.Fatalf("bearer: %d", rec.Code)
	}
}

func TestLoginRateLimited(t *testing.T) {
	t.Parallel()
	h, _, _ := room(t)

	for i := 0; i < attemptsPerAddress; i++ {
		if rec := do(h, "POST", "/api/auth/login", `{"password":"nope nope nope"}`, nil); rec.Code != 401 {
			t.Fatalf("attempt %d: %d", i, rec.Code)
		}
	}
	rec := do(h, "POST", "/api/auth/login", `{"password":"`+password+`"}`, nil)
	if rec.Code != 429 || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("over budget: %d %v", rec.Code, rec.Header())
	}
}

func TestSetupIsClosedOnceAPasswordExists(t *testing.T) {
	t.Parallel()
	h, _, _ := room(t)

	rec := do(h, "POST", "/api/auth/setup", `{"code":"x","password":"`+password+`"}`, nil)
	if rec.Code != 404 {
		t.Fatalf("setup after bootstrap: %d", rec.Code)
	}
	rec = do(h, "GET", "/api/auth/state", "", nil)
	if !strings.Contains(rec.Body.String(), `"setup_required":false`) ||
		!strings.Contains(rec.Body.String(), `"name":"Google"`) {
		t.Fatalf("state %s", rec.Body)
	}
}

func TestOIDCCallbackNeedsTheBrowserThatStarted(t *testing.T) {
	t.Parallel()
	h, _, _ := room(t)

	start := do(h, "GET", "/auth/oidc/start?next=/campaign", "", nil)
	if start.Code != 302 {
		t.Fatalf("start: %d", start.Code)
	}
	var stateCookie *http.Cookie
	for _, c := range start.Result().Cookies() {
		if c.Name == oidcStateCookie {
			stateCookie = c
		}
	}
	loc, _ := url.Parse(start.Header().Get("Location"))
	state := loc.Query().Get("state")
	if stateCookie == nil || stateCookie.Value != state {
		t.Fatal("state not bound to the browser")
	}

	// Someone else's browser, holding a callback link: refused.
	rec := do(h, "GET", "/auth/oidc/callback?code=c&state="+state, "", nil)
	if rec.Code != 303 || !strings.HasPrefix(rec.Header().Get("Location"), "/login?error=") || sessionCookie(rec) != nil {
		t.Fatalf("unbound callback: %d %v", rec.Code, rec.Header())
	}

	// The refusal did not spend the state; the real browser still completes.
	rec = do(h, "GET", "/auth/oidc/callback?code=c&state="+state, "", func(r *http.Request) { r.AddCookie(stateCookie) })
	if rec.Code != 303 || rec.Header().Get("Location") != "/campaign" || sessionCookie(rec) == nil {
		t.Fatalf("bound callback: %d %v", rec.Code, rec.Header())
	}
}

func TestOIDCProviderErrorShownOnLogin(t *testing.T) {
	t.Parallel()
	h, _, _ := room(t)

	rec := do(h, "GET", "/auth/oidc/callback?error=access_denied&state=x", "", nil)
	if rec.Code != 303 || !strings.Contains(rec.Header().Get("Location"), "access_denied") {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
}
