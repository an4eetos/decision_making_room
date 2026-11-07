// Package authapi signs devices in and out over JSON.
package authapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/auth/domain"
	"github.com/an4eetos/decision-room/internal/auth/port"
	"github.com/an4eetos/decision-room/internal/auth/usecase"
	httpserver "github.com/an4eetos/decision-room/internal/infra/http"
)

const (
	// maxBodyBytes is far above a password and a device name.
	maxBodyBytes = 8 << 10

	// Ten tries per address and a hundred in all per quarter hour. A
	// success clears the address. The total is reachable by an attacker
	// rotating addresses, which locks out new logins for the window; the
	// devices already signed in keep working.
	attemptsPerAddress = 10
	attemptsTotal      = 100
	attemptWindow      = 15 * time.Minute

	// cookieMaxAge is Chrome's ceiling. The server decides when a session
	// ends; the cookie only has to outlive it.
	cookieMaxAge = 400 * 24 * time.Hour
)

type Handler struct {
	svc      *usecase.Service
	oidc     *usecase.OIDC // nil when OIDC is not configured
	oidcName string
	limiter  *httpserver.Limiter
}

func NewHandler(svc *usecase.Service, oidc *usecase.OIDC, oidcName string) *Handler {
	return &Handler{
		svc:      svc,
		oidc:     oidc,
		oidcName: oidcName,
		limiter:  httpserver.NewLimiter(attemptsPerAddress, attemptsTotal, attemptWindow),
	}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/auth/state", h.state)
	mux.HandleFunc("POST /api/auth/setup", h.setup)
	mux.HandleFunc("POST /api/auth/login", h.login)
	mux.HandleFunc("POST /api/auth/logout", h.logout)
	mux.HandleFunc("GET /api/auth/sessions", h.listSessions)
	mux.HandleFunc("DELETE /api/auth/sessions/{id}", h.revokeSession)

	// Browser navigations, not API calls: the provider sends the browser
	// back here, not a script.
	mux.HandleFunc("GET /auth/oidc/start", h.oidcStart)
	mux.HandleFunc("GET /auth/oidc/callback", h.oidcCallback)
	mux.HandleFunc("POST /api/auth/oidc/exchange", h.oidcExchange)
}

type sessionDTO struct {
	ID         uuid.UUID `json:"id"`
	Client     string    `json:"client"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	// Current marks the session making this request.
	Current bool `json:"current"`
}

func toDTO(s domain.Session, current string) sessionDTO {
	return sessionDTO{
		ID:         s.ID,
		Client:     s.Client,
		CreatedAt:  s.CreatedAt,
		LastSeenAt: s.LastSeenAt,
		ExpiresAt:  s.ExpiresAt,
		Current:    s.ID.String() == current,
	}
}

// signInResponse carries the token only to a client that keeps it itself.
// The browser asks for a cookie instead, so page script never sees it.
type signInResponse struct {
	Token   string     `json:"token,omitempty"`
	Session sessionDTO `json:"session"`
}

// stateDTO tells a client which ways in to offer before anyone is signed in.
type stateDTO struct {
	SetupRequired bool    `json:"setup_required"`
	Password      bool    `json:"password"`
	OIDC          oidcDTO `json:"oidc"`
}

type oidcDTO struct {
	Enabled bool   `json:"enabled"`
	Name    string `json:"name,omitempty"`
	// StartURL is opened in a browser; the app adds app_redirect and
	// code_challenge to it.
	StartURL string `json:"start_url,omitempty"`
}

func (h *Handler) state(w http.ResponseWriter, r *http.Request) {
	needs, err := h.svc.NeedsSetup(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	out := stateDTO{SetupRequired: needs, Password: h.svc.PasswordLogin()}
	if h.oidc != nil {
		out.OIDC = oidcDTO{Enabled: true, Name: h.oidcName, StartURL: "/auth/oidc/start"}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) setup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code     string `json:"code"`
		Password string `json:"password"`
		Client   string `json:"client"`
		Cookie   bool   `json:"cookie"`
	}
	if !decode(w, r, &req) || !h.allow(w, r) {
		return
	}

	token, sess, err := h.svc.Setup(r.Context(), req.Code, req.Password, clientName(r, req.Client))
	switch {
	case err == nil:
		h.limiter.Reset(httpserver.ClientIP(r))
		h.signedIn(w, r, token, sess, req.Cookie)
	case errors.Is(err, usecase.ErrSetupClosed):
		httpError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, usecase.ErrWrongSetupCode):
		httpError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, domain.ErrPasswordTooShort), errors.Is(err, domain.ErrPasswordTooLong):
		httpError(w, http.StatusBadRequest, err.Error())
	default:
		internalError(w, err)
	}
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
		Client   string `json:"client"`
		Cookie   bool   `json:"cookie"`
	}
	if !decode(w, r, &req) || !h.allow(w, r) {
		return
	}

	token, sess, err := h.svc.Login(r.Context(), req.Password, clientName(r, req.Client))
	switch {
	case err == nil:
		h.limiter.Reset(httpserver.ClientIP(r))
		h.signedIn(w, r, token, sess, req.Cookie)
	case errors.Is(err, usecase.ErrWrongPassword):
		httpError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, usecase.ErrSetupRequired):
		httpError(w, http.StatusConflict, err.Error())
	case errors.Is(err, usecase.ErrPasswordOff):
		httpError(w, http.StatusForbidden, err.Error())
	default:
		internalError(w, err)
	}
}

// oidcStateCookie binds a sign-in to the browser that started it, so a
// callback link planted in someone else's browser cannot sign them into
// this room.
const (
	oidcStateCookie = "room_oidc_state"
	oidcCookiePath  = "/auth/oidc"
	oidcCookieTTL   = 10 * time.Minute
)

func (h *Handler) oidcStart(w http.ResponseWriter, r *http.Request) {
	if h.oidc == nil {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	authURL, state, err := h.oidc.Start(r.Context(), usecase.StartRequest{
		Next:         q.Get("next"),
		AppRedirect:  q.Get("app_redirect"),
		AppChallenge: q.Get("code_challenge"),
	})
	switch {
	case err == nil:
	case errors.Is(err, usecase.ErrUnknownAppRedirect), errors.Is(err, usecase.ErrBadChallenge):
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	case errors.Is(err, usecase.ErrTooManyLogins):
		http.Error(w, err.Error(), http.StatusTooManyRequests)
		return
	default:
		log.Printf("auth: oidc start: %v", err)
		loginError(w, r, "could not reach the sign-in provider")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookie,
		Value:    state,
		Path:     oidcCookiePath,
		MaxAge:   int(oidcCookieTTL.Seconds()),
		HttpOnly: true,
		Secure:   httpserver.IsHTTPS(r),
		// Lax, not Strict: the callback is a top-level navigation from the
		// provider's site, and Strict would leave the cookie behind.
		SameSite: http.SameSiteLaxMode,
	})
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (h *Handler) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if h.oidc == nil {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	http.SetCookie(w, &http.Cookie{
		Name: oidcStateCookie, Value: "", Path: oidcCookiePath, MaxAge: -1,
		HttpOnly: true, Secure: httpserver.IsHTTPS(r), SameSite: http.SameSiteLaxMode,
	})

	if e := q.Get("error"); e != "" {
		// The provider's own reason, e.g. access_denied when you cancel.
		loginError(w, r, "sign-in was not completed ("+e+")")
		return
	}
	state := q.Get("state")
	bound, err := r.Cookie(oidcStateCookie)
	if state == "" || err != nil || bound.Value != state {
		loginError(w, r, usecase.ErrLoginExpired.Error())
		return
	}

	res, err := h.oidc.Callback(r.Context(), state, q.Get("code"), clientName(r, ""))
	switch {
	case err == nil:
	case errors.Is(err, usecase.ErrLoginExpired), errors.Is(err, usecase.ErrNotAllowed):
		loginError(w, r, err.Error())
		return
	default:
		log.Printf("auth: oidc callback: %v", err)
		loginError(w, r, "the sign-in provider's answer could not be verified")
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	if res.AppRedirect != "" {
		http.Redirect(w, r, res.AppRedirect, http.StatusSeeOther)
		return
	}
	setSessionCookie(w, r, res.Token)
	http.Redirect(w, r, res.Next, http.StatusSeeOther)
}

// oidcExchange is the app's half of an OIDC sign-in: the one-time code from
// its redirect, and the PKCE verifier only it holds.
func (h *Handler) oidcExchange(w http.ResponseWriter, r *http.Request) {
	if h.oidc == nil {
		httpError(w, http.StatusNotFound, "oidc sign-in is not configured")
		return
	}
	var req struct {
		Code         string `json:"code"`
		CodeVerifier string `json:"code_verifier"`
		Client       string `json:"client"`
	}
	if !decode(w, r, &req) || !h.allow(w, r) {
		return
	}
	token, sess, err := h.oidc.Exchange(r.Context(), req.Code, req.CodeVerifier, clientName(r, req.Client))
	switch {
	case err == nil:
		h.limiter.Reset(httpserver.ClientIP(r))
		writeJSON(w, http.StatusOK, signInResponse{Token: token, Session: toDTO(sess, sess.ID.String())})
	case errors.Is(err, usecase.ErrLoginExpired):
		httpError(w, http.StatusUnauthorized, err.Error())
	default:
		internalError(w, err)
	}
}

// loginError sends the browser back to the login page with a reason.
func loginError(w http.ResponseWriter, r *http.Request, msg string) {
	http.Redirect(w, r, "/login?error="+url.QueryEscape(msg), http.StatusSeeOther)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if current, ok := currentSession(r); ok {
		if err := h.svc.Revoke(r.Context(), current); err != nil && !errors.Is(err, port.ErrSessionNotFound) {
			internalError(w, err)
			return
		}
	}
	clearCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := h.svc.Sessions(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	current, _ := httpserver.SessionID(r.Context())
	out := make([]sessionDTO, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, toDTO(s, current))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) revokeSession(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	if err := h.svc.Revoke(r.Context(), id); err != nil {
		if errors.Is(err, port.ErrSessionNotFound) {
			httpError(w, http.StatusNotFound, err.Error())
			return
		}
		internalError(w, err)
		return
	}
	if current, ok := currentSession(r); ok && current == id {
		clearCookie(w, r)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) signedIn(w http.ResponseWriter, r *http.Request, token string, sess domain.Session, cookie bool) {
	resp := signInResponse{Session: toDTO(sess, sess.ID.String())}
	if cookie {
		setSessionCookie(w, r, token)
	} else {
		resp.Token = token
	}
	writeJSON(w, http.StatusOK, resp)
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     httpserver.SessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(cookieMaxAge.Seconds()),
		HttpOnly: true,
		Secure:   httpserver.IsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// allow spends one attempt from the caller's budget, answering 429 when it is
// gone.
func (h *Handler) allow(w http.ResponseWriter, r *http.Request) bool {
	ok, retry := h.limiter.Allow(httpserver.ClientIP(r))
	if ok {
		return true
	}
	secs := int(retry.Seconds()) + 1
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	httpError(w, http.StatusTooManyRequests, "too many attempts; try again in "+(time.Duration(secs)*time.Second).String())
	return false
}

func currentSession(r *http.Request) (uuid.UUID, bool) {
	raw, ok := httpserver.SessionID(r.Context())
	if !ok {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	return id, err == nil
}

// clientName is the device name the client gave, or its User-Agent.
func clientName(r *http.Request, given string) string {
	if name := domain.CleanClient(given); name != "" {
		return name
	}
	return domain.CleanClient(r.UserAgent())
}

func clearCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     httpserver.SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   httpserver.IsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func internalError(w http.ResponseWriter, err error) {
	log.Printf("auth: %v", err)
	httpError(w, http.StatusInternalServerError, "internal error")
}

func httpError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
