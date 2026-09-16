package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
	"github.com/an4eetos/decision-room/internal/memory/service"
	"github.com/an4eetos/decision-room/internal/memory/usecase"
)

type Handler struct {
	ingestUC  *usecase.Ingest
	searchUC  *usecase.Search
	consultUC *usecase.Consult
	chatUC    *usecase.Chat
}

func NewHandler(ingest *usecase.Ingest, search *usecase.Search, consult *usecase.Consult, chat *usecase.Chat) *Handler {
	return &Handler{
		ingestUC:  ingest,
		searchUC:  search,
		consultUC: consult,
		chatUC:    chat,
	}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("POST /api/memories", h.createMemory)
	mux.HandleFunc("POST /api/consult", h.consult)
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

func (h *Handler) consult(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Question string `json:"question"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	result, err := h.consultUC.Execute(r.Context(), usecase.ConsultInput{Question: req.Question})
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
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	detail, err := h.chatUC.SendMessage(r.Context(), r.PathValue("id"), req.Content)
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
