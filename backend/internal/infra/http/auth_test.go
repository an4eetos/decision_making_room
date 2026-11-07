package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeAuth struct {
	valid string
	err   error
}

func (f fakeAuth) Authenticate(_ context.Context, token string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if token == f.valid {
		return "session-1", nil
	}
	return "", ErrUnauthenticated
}

// echo answers 200 with the session id it was handed.
var echo = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	id, _ := SessionID(r.Context())
	_, _ = w.Write([]byte("ok:" + id))
})

func TestRequireAuth(t *testing.T) {
	t.Parallel()
	h := RequireAuth(fakeAuth{valid: "good"}, echo)

	tests := []struct {
		name     string
		method   string
		path     string
		headers  map[string]string
		cookie   string
		want     int
		wantBody string
	}{
		{name: "health is public", method: "GET", path: "/health", want: 200},
		{name: "static is public", method: "GET", path: "/static/style.css", want: 200},
		{name: "login api is public", method: "POST", path: "/api/auth/login", want: 200},
		{name: "oidc callback is public", method: "GET", path: "/auth/oidc/callback", want: 200},

		{name: "api without token", method: "GET", path: "/api/campaign", want: 401},
		{name: "api with bad token", method: "GET", path: "/api/campaign",
			headers: map[string]string{"Authorization": "Bearer nope"}, want: 401},
		{name: "api with bearer", method: "GET", path: "/api/campaign",
			headers: map[string]string{"Authorization": "Bearer good"}, want: 200, wantBody: "ok:session-1"},
		{name: "basic auth is not a bearer", method: "GET", path: "/api/campaign",
			headers: map[string]string{"Authorization": "Basic good"}, want: 401},
		{name: "api with cookie", method: "GET", path: "/api/campaign", cookie: "good", want: 200},

		{name: "page redirects to login", method: "GET", path: "/campaign?x=1",
			headers: map[string]string{"Accept": "text/html"}, want: 303},
		{name: "sessions list is private", method: "GET", path: "/api/auth/sessions", want: 401},

		// The mux would route these to /api/...; the guard must judge them so.
		{name: "dot-dot out of static", method: "GET", path: "/static/../api/campaign", want: 401},

		{name: "cookie POST cross-site", method: "POST", path: "/api/capture", cookie: "good",
			headers: map[string]string{"Sec-Fetch-Site": "cross-site"}, want: 403},
		{name: "cookie POST foreign origin", method: "POST", path: "/api/capture", cookie: "good",
			headers: map[string]string{"Origin": "https://evil.example"}, want: 403},
		{name: "cookie POST same origin", method: "POST", path: "/api/capture", cookie: "good",
			headers: map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://room.test"}, want: 200},
		{name: "bearer POST from anywhere", method: "POST", path: "/api/capture",
			headers: map[string]string{"Authorization": "Bearer good", "Sec-Fetch-Site": "cross-site"}, want: 200},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, "http://room.test"+tc.path, nil)
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			if tc.cookie != "" {
				req.AddCookie(&http.Cookie{Name: SessionCookie, Value: tc.cookie})
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.want, rec.Body.String())
			}
			if tc.wantBody != "" && rec.Body.String() != tc.wantBody {
				t.Fatalf("body = %q", rec.Body.String())
			}
			if tc.want == 303 {
				if loc := rec.Header().Get("Location"); loc != "/login?next=%2Fcampaign%3Fx%3D1" {
					t.Fatalf("Location = %q", loc)
				}
			}
			if tc.want == 401 && !strings.Contains(rec.Header().Get("WWW-Authenticate"), "Bearer") {
				t.Fatal("401 without WWW-Authenticate")
			}
		})
	}
}

func TestRequireAuthFailsClosed(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/campaign", nil)
	req.Header.Set("Authorization", "Bearer anything")
	RequireAuth(nil, echo).ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("nil authenticator: status %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	RequireAuth(fakeAuth{err: errors.New("db down")}, echo).ServeHTTP(rec, req)
	if rec.Code != 503 {
		t.Fatalf("authenticator error: status %d", rec.Code)
	}
}

func TestLimiter(t *testing.T) {
	t.Parallel()

	l := NewLimiter(2, 3, time.Minute)
	clock := time.Unix(0, 0)
	l.now = func() time.Time { return clock }

	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("first")
	}
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("second")
	}
	if ok, retry := l.Allow("a"); ok || retry != time.Minute {
		t.Fatalf("third: ok=%v retry=%v", ok, retry)
	}

	// The total is shared: b gets one before the backstop.
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("b first")
	}
	if ok, _ := l.Allow("c"); ok {
		t.Fatal("total exceeded")
	}

	l.Reset("a")
	clock = clock.Add(time.Minute)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("window did not roll")
	}
}

func TestClientIP(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:5555"
	if got := ClientIP(r); got != "10.0.0.1" {
		t.Fatalf("RemoteAddr: %q", got)
	}
	// The client wrote the first entry; the proxy appended the last.
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.9")
	if got := ClientIP(r); got != "203.0.113.9" {
		t.Fatalf("X-Forwarded-For: %q", got)
	}
}
