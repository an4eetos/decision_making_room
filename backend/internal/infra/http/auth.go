package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// SessionCookie carries the browser's session token. The phone sends the same
// token as "Authorization: Bearer".
const SessionCookie = "room_session"

// ErrUnauthenticated is what an Authenticator returns for a missing, unknown
// or expired token. Any other error means the check itself failed.
var ErrUnauthenticated = errors.New("unauthenticated")

// Authenticator resolves a session token to the session's id.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (sessionID string, err error)
}

type sessionKey struct{}

// SessionID returns the id of the session that authenticated the request.
func SessionID(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(sessionKey{}).(string)
	return id, ok
}

// WithSessionID marks a context as authenticated; for handler tests.
func WithSessionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, sessionKey{}, id)
}

// publicPaths are reachable without a session: the health check, the pages
// and endpoints that create one, and the static files those pages need.
var publicPaths = map[string]bool{
	"/health":         true,
	"/login":          true,
	"/setup":          true,
	"/api/auth/login": true,
	"/api/auth/setup": true,
	"/api/auth/state": true,

	"/auth/oidc/start":        true,
	"/auth/oidc/callback":     true,
	"/api/auth/oidc/exchange": true,
}

func isPublic(r *http.Request) bool {
	// Cleaned, because the mux routes on the cleaned path: /static/../api/x
	// must be judged as /api/x.
	p := path.Clean("/" + r.URL.Path)
	return publicPaths[p] || strings.HasPrefix(p, "/static/")
}

// RequireAuth guards every route except the public ones. API calls without a
// valid session get 401; a page gets a redirect to /login. With a nil
// Authenticator nothing private is served at all.
func RequireAuth(auth Authenticator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublic(r) {
			next.ServeHTTP(w, r)
			return
		}

		token, fromCookie := credential(r)
		if token == "" || auth == nil {
			deny(w, r)
			return
		}
		// A cookie is sent by the browser on its own, so a state-changing
		// request carried by one must come from this site. A bearer token
		// is only ever attached on purpose.
		if fromCookie && !safeMethod(r.Method) && !sameOrigin(r) {
			writeError(w, http.StatusForbidden, "cross-origin request refused")
			return
		}

		id, err := auth.Authenticate(r.Context(), token)
		if errors.Is(err, ErrUnauthenticated) {
			deny(w, r)
			return
		}
		if err != nil {
			log.Printf("auth: %v", err)
			writeError(w, http.StatusServiceUnavailable, "could not check the session")
			return
		}

		next.ServeHTTP(w, r.WithContext(WithSessionID(r.Context(), id)))
	})
}

// credential takes the bearer token first, then the cookie.
func credential(r *http.Request) (token string, fromCookie bool) {
	if h := r.Header.Get("Authorization"); h != "" {
		scheme, value, ok := strings.Cut(h, " ")
		if ok && strings.EqualFold(scheme, "Bearer") {
			return strings.TrimSpace(value), false
		}
		return "", false
	}
	if c, err := r.Cookie(SessionCookie); err == nil {
		return c.Value, true
	}
	return "", false
}

func deny(w http.ResponseWriter, r *http.Request) {
	if isPage(r) {
		next := r.URL.RequestURI()
		http.Redirect(w, r, "/login?next="+url.QueryEscape(next), http.StatusSeeOther)
		return
	}
	w.Header().Set("WWW-Authenticate", `Bearer realm="decision-room"`)
	writeError(w, http.StatusUnauthorized, "unauthorized")
}

// isPage is a browser navigation to an HTML page, which is better answered
// with the login form than with a JSON error.
func isPage(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if strings.HasPrefix(path.Clean("/"+r.URL.Path), "/api/") {
		return false
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// sameOrigin trusts Sec-Fetch-Site when the browser sends it, then Origin.
// A request with neither did not come from a modern browser's page, so the
// cookie was not ridden by another site.
func sameOrigin(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "":
	default:
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// IsHTTPS reports whether the client reached the server over TLS, directly or
// through a proxy such as Railway's, which terminates TLS in front of it.
func IsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
