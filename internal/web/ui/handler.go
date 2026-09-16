package ui

import (
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/web/assets"
)

type Handler struct {
	tmpl   *template.Template
	static fs.FS
}

// NewHandler serves the embedded frontend. A non-empty webRoot overrides it with
// that directory on disk, so the templates and CSS can be edited without a
// rebuild; anything missing there is an error rather than a silent fallback.
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
		return assets.Templates(), assets.Static(), nil
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
}

func (h *Handler) chatPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, "chat.html", map[string]any{
		"Title":          "Consult",
		"ContainerClass": "container--chat",
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
