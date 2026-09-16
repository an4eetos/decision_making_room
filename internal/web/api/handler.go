package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	genport "github.com/an4eetos/decision-room/internal/generals/port"
	journalusecase "github.com/an4eetos/decision-room/internal/journal/usecase"
	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
	"github.com/an4eetos/decision-room/internal/memory/service"
	"github.com/an4eetos/decision-room/internal/memory/usecase"
	modeport "github.com/an4eetos/decision-room/internal/modes/port"
)

type Handler struct {
	modes     modeport.Registry
	generals  genport.Registry
	captureUC *journalusecase.Capture
	ingestUC  *usecase.Ingest
	searchUC  *usecase.Search
	consultUC *usecase.Consult
	chatUC    *usecase.Chat
}

func NewHandler(
	ingest *usecase.Ingest,
	search *usecase.Search,
	consult *usecase.Consult,
	chat *usecase.Chat,
	capture *journalusecase.Capture,
	generals genport.Registry,
	modes modeport.Registry,
) *Handler {
	return &Handler{
		modes:     modes,
		generals:  generals,
		ingestUC:  ingest,
		searchUC:  search,
		consultUC: consult,
		chatUC:    chat,
		captureUC: capture,
	}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("POST /api/memories", h.createMemory)
	mux.HandleFunc("POST /api/consult", h.consult)
	mux.HandleFunc("POST /api/capture", h.capture)
	mux.HandleFunc("GET /api/generals", h.listGenerals)
	mux.HandleFunc("GET /api/modes", h.listModes)
	mux.HandleFunc("GET /api/memories/search", h.searchMemories)
	mux.HandleFunc("GET /api/chat/sessions", h.listChatSessions)
	mux.HandleFunc("POST /api/chat/sessions", h.createChatSession)
	mux.HandleFunc("GET /api/chat/sessions/{id}", h.getChatSession)
	mux.HandleFunc("DELETE /api/chat/sessions/{id}", h.deleteChatSession)
	mux.HandleFunc("POST /api/chat/sessions/{id}/messages", h.sendChatMessage)
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type createMemoryRequest struct {
	Kind  string   `json:"kind"`
	Title string   `json:"title"`
	Body  string   `json:"body"`
	Tags  []string `json:"tags"`
}

func (h *Handler) createMemory(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")

	if strings.HasPrefix(contentType, "multipart/form-data") {
		h.createMemoryMultipart(w, r)
		return
	}

	var req createMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	h.ingestAndRespond(w, r, req.Kind, req.Title, req.Body, req.Tags)
}

func (h *Handler) createMemoryMultipart(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "parse form", http.StatusBadRequest)
		return
	}

	title := r.FormValue("title")
	body := r.FormValue("body")
	tags := service.ParseTags(r.FormValue("tags"))

	if file, _, err := r.FormFile("file"); err == nil {
		defer file.Close()
		content, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, "read file", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(body) == "" {
			body = string(content)
		}
	}

	h.ingestAndRespond(w, r, r.FormValue("kind"), title, body, tags)
}

func (h *Handler) ingestAndRespond(w http.ResponseWriter, r *http.Request, kindRaw, title, body string, tags []string) {
	kind, ok := domain.ParseMemoryKind(kindRaw)
	if !ok {
		http.Error(w, "invalid kind", http.StatusBadRequest)
		return
	}

	result, err := h.ingestUC.Execute(r.Context(), usecase.IngestInput{
		Kind:  kind,
		Title: title,
		Body:  body,
		Tags:  tags,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, result)
}

// listModes serves the mode list for the chip's dropdown, grouped by family.
func (h *Handler) listModes(w http.ResponseWriter, r *http.Request) {
	type modeDTO struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Family  string `json:"family"`
		Summary string `json:"summary,omitempty"`
	}

	modes := h.modes.List()
	out := make([]modeDTO, 0, len(modes))
	for _, m := range modes {
		out = append(out, modeDTO{
			ID: m.ID, Name: m.Name, Family: string(m.Family), Summary: m.Summary,
		})
	}

	writeJSON(w, http.StatusOK, out)
}

// listGenerals serves the roster for the picker. It deliberately omits the full
// doctrine: that is several hundred KB across the roster and the UI shows the
// card fields only.
func (h *Handler) listGenerals(w http.ResponseWriter, r *http.Request) {
	type generalDTO struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Epithet string `json:"epithet,omitempty"`
		Era     string `json:"era,omitempty"`
		Family  string `json:"family"`
		Job     string `json:"job"`
		Bias    string `json:"bias,omitempty"`
	}

	lenses := h.generals.Generals()
	out := make([]generalDTO, 0, len(lenses))
	for _, l := range lenses {
		out = append(out, generalDTO{
			ID: l.ID, Name: l.Name, Epithet: l.Epithet, Era: l.Era,
			Family: string(l.Family), Job: l.Job, Bias: l.Bias,
		})
	}

	writeJSON(w, http.StatusOK, out)
}

// capture appends a line to today's daily note. It writes to the journal folder
// rather than the database so the markdown stays the source of truth.
func (h *Handler) capture(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	result, err := h.captureUC.Execute(r.Context(), req.Text)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusCreated, result)
}

func (h *Handler) consult(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Question string   `json:"question"`
		Tier     string   `json:"tier"`
		TopK     int      `json:"top_k"`
		Generals []string `json:"generals"`
		Mode     string   `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	result, err := h.consultUC.Execute(r.Context(), usecase.ConsultInput{
		Question:   req.Question,
		Tier:       req.Tier,
		TopK:       req.TopK,
		GeneralIDs: req.Generals,
		ModeID:     req.Mode,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) searchMemories(w http.ResponseWriter, r *http.Request) {
	kind, _ := domain.ParseMemoryKind(r.URL.Query().Get("kind"))
	var kindPtr *domain.MemoryKind
	if kind != "" {
		kindPtr = &kind
	}

	entries, err := h.searchUC.Execute(r.Context(), usecase.SearchInput{
		Query: r.URL.Query().Get("q"),
		Kind:  kindPtr,
		Tags:  service.ParseTags(r.URL.Query().Get("tags")),
		Limit: 50,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, entries)
}

func (h *Handler) listChatSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := h.chatUC.ListSessions(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (h *Handler) createChatSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	session, err := h.chatUC.CreateSession(r.Context(), req.Title)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, session)
}

func (h *Handler) getChatSession(w http.ResponseWriter, r *http.Request) {
	detail, err := h.chatUC.GetSession(r.Context(), r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (h *Handler) deleteChatSession(w http.ResponseWriter, r *http.Request) {
	err := h.chatUC.DeleteSession(r.Context(), r.PathValue("id"))
	if err != nil {
		if strings.Contains(err.Error(), "invalid session id") {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, port.ErrChatSessionNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) sendChatMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content  string    `json:"content"`
		Tier     string    `json:"tier"`
		Generals *[]string `json:"generals"`
		Mode     *string   `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	detail, err := h.chatUC.SendMessage(r.Context(), usecase.SendMessageInput{
		SessionID: r.PathValue("id"),
		Question:  req.Content,
		Tier:      req.Tier,
		Generals:  derefStrings(req.Generals),
		Mode:      req.Mode,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// derefStrings distinguishes "field absent" (nil, leave the session alone) from
// "field present but empty" (clear the pick back to auto-select).
func derefStrings(v *[]string) []string {
	if v == nil {
		return nil
	}
	if *v == nil {
		return []string{}
	}
	return *v
}
