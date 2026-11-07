package ui

import (
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/an4eetos/decision-room/internal/memory/domain"
)

type Handler struct {
	tmpl   *template.Template
	static fs.FS
	// needsSetup reports whether the room has no password yet. Nil means it
	// always has one, so /setup is a 404.
	needsSetup func(context.Context) (bool, error)
}

// WithSetupCheck lets /login and /setup send the browser to whichever of the
// two applies.
func (h *Handler) WithSetupCheck(needsSetup func(context.Context) (bool, error)) *Handler {
	h.needsSetup = needsSetup
	return h
}

// NewHandler serves the web client from webRoot, a directory holding templates/
// and static/. The client lives outside the Go module (client/web at the
// repository root), so it is read from disk, never embedded; anything missing
// there is an error rather than a silent fallback.
func NewHandler(webRoot string) (*Handler, error) {
	templates, static, err := resolveAssets(webRoot)
	if err != nil {
		return nil, err
	}

	tmpl, err := template.New("").Funcs(templateFuncs()).ParseFS(templates, "*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	return &Handler{tmpl: tmpl, static: static}, nil
}

func resolveAssets(webRoot string) (templates fs.FS, static fs.FS, err error) {
	if webRoot == "" {
		return nil, nil, fmt.Errorf("WEB_ROOT is empty: point it at client/web")
	}

	templateDir := filepath.Join(webRoot, "templates")
	staticDir := filepath.Join(webRoot, "static")
	for _, dir := range []string{templateDir, staticDir} {
		if _, statErr := os.Stat(dir); statErr != nil {
			return nil, nil, fmt.Errorf("WEB_ROOT=%q: %w", webRoot, statErr)
		}
	}

	log.Printf("serving frontend from disk: %s", webRoot)
	return os.DirFS(templateDir), os.DirFS(staticDir), nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(h.static)))

	mux.HandleFunc("GET /{$}", h.chatPage)
	mux.HandleFunc("GET /ingest", h.ingestPage)
	mux.HandleFunc("GET /memories", h.memoriesPage)
	mux.HandleFunc("GET /relocation", h.relocationPage)
	mux.HandleFunc("GET /campaign", h.campaignPage)
	mux.HandleFunc("GET /login", h.loginPage)
	mux.HandleFunc("GET /setup", h.setupPage)
}

// loginPage is static; auth.js asks /api/auth/state which ways in to show,
// and goes on to /setup when a password is the only one and none is set.
func (h *Handler) loginPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, "login.html", map[string]any{"Title": "Sign in"})
}

// setupPage exists only while there is no password. Afterwards it is a 404,
// not a redirect, so it does not advertise that it ever existed.
func (h *Handler) setupPage(w http.ResponseWriter, r *http.Request) {
	needs, err := h.setupNeeded(r.Context())
	if err != nil {
		log.Printf("setup page: %v", err)
		http.Error(w, "could not reach the database", http.StatusServiceUnavailable)
		return
	}
	if !needs {
		http.NotFound(w, r)
		return
	}
	h.render(w, "setup.html", map[string]any{"Title": "Set up"})
}

func (h *Handler) setupNeeded(ctx context.Context) (bool, error) {
	if h.needsSetup == nil {
		return false, nil
	}
	return h.needsSetup(ctx)
}

func (h *Handler) chatPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, "chat.html", map[string]any{
		"Title":          "Briefing",
		"ContainerClass": "container--chat container--war",
	})
}

func (h *Handler) ingestPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, "ingest.html", map[string]any{
		"Title": "Ingest",
		"Kinds": allKinds(),
	})
}

func (h *Handler) memoriesPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, "memories.html", map[string]any{
		"Title": "Memories",
		"Kinds": allKinds(),
		"Query": r.URL.Query().Get("q"),
		"Kind":  r.URL.Query().Get("kind"),
		"Tags":  r.URL.Query().Get("tags"),
	})
}

func (h *Handler) relocationPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, "relocation.html", map[string]any{
		"Title":          "Relocation",
		"ContainerClass": "container--wide",
	})
}

func (h *Handler) campaignPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, "campaign.html", map[string]any{
		"Title":          "Campaign",
		"ContainerClass": "container--wide",
	})
}

func (h *Handler) render(w http.ResponseWriter, name string, data map[string]any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func allKinds() []domain.MemoryKind {
	return []domain.MemoryKind{
		domain.KindDecision,
		domain.KindPlan,
		domain.KindNote,
		domain.KindDailyLog,
	}
}
