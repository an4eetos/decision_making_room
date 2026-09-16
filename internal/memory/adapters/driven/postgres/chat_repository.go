package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/an4eetos/decision-room/internal/memory/port"
)

type ChatRepository struct {
	pool *pgxpool.Pool
}

func NewChatRepository(pool *pgxpool.Pool) *ChatRepository {
	return &ChatRepository{pool: pool}
}

func (r *ChatRepository) CreateSession(ctx context.Context, session port.ChatSession) (port.ChatSession, error) {
	now := time.Now().UTC()
	if session.ID == uuid.Nil {
		session.ID = uuid.New()
	}
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	session.UpdatedAt = now

	_, err := r.pool.Exec(ctx, `
		INSERT INTO chat_sessions (id, title, summary, summary_updated_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, session.ID, session.Title, session.Summary, session.SummaryUpdatedAt, session.CreatedAt, session.UpdatedAt)
	if err != nil {
		return port.ChatSession{}, fmt.Errorf("insert chat session: %w", err)
	}

	return session, nil
}

func (r *ChatRepository) ListSessions(ctx context.Context, limit int) ([]port.ChatSession, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, title, summary, summary_updated_at, created_at, updated_at
		FROM chat_sessions
		ORDER BY updated_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list chat sessions: %w", err)
	}
	defer rows.Close()

	var sessions []port.ChatSession
	for rows.Next() {
		session, err := scanChatSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sessions, nil
}

func (r *ChatRepository) GetSession(ctx context.Context, id uuid.UUID) (port.ChatSession, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, title, summary, summary_updated_at, created_at, updated_at
		FROM chat_sessions
		WHERE id = $1
	`, id)

	session, err := scanChatSession(row)
	if err != nil {
		return port.ChatSession{}, fmt.Errorf("get chat session: %w", err)
	}
	return session, nil
}

func (r *ChatRepository) UpdateSession(ctx context.Context, session port.ChatSession) error {
	session.UpdatedAt = time.Now().UTC()

	_, err := r.pool.Exec(ctx, `
		UPDATE chat_sessions
		SET title = $2,
		    summary = $3,
		    summary_updated_at = $4,
		    updated_at = $5
		WHERE id = $1
	`, session.ID, session.Title, session.Summary, session.SummaryUpdatedAt, session.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update chat session: %w", err)
	}
	return nil
}

func (r *ChatRepository) CreateMessage(ctx context.Context, message port.ChatMessage) (port.ChatMessage, error) {
	now := time.Now().UTC()
	if message.ID == uuid.Nil {
		message.ID = uuid.New()
	}
	if message.CreatedAt.IsZero() {
		message.CreatedAt = now
	}

	sourcesJSON, err := json.Marshal(message.Sources)
	if err != nil {
		return port.ChatMessage{}, fmt.Errorf("marshal chat sources: %w", err)
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO chat_messages (id, session_id, role, content, sources, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, message.ID, message.SessionID, message.Role, message.Content, sourcesJSON, message.CreatedAt)
	if err != nil {
		return port.ChatMessage{}, fmt.Errorf("insert chat message: %w", err)
	}

	_, err = r.pool.Exec(ctx, `
		UPDATE chat_sessions
		SET updated_at = $2
		WHERE id = $1
	`, message.SessionID, now)
	if err != nil {
		return port.ChatMessage{}, fmt.Errorf("touch chat session: %w", err)
	}

	return message, nil
}

func (r *ChatRepository) ListMessages(ctx context.Context, sessionID uuid.UUID) ([]port.ChatMessage, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, session_id, role, content, sources, created_at
		FROM chat_messages
		WHERE session_id = $1
		ORDER BY created_at ASC, id ASC
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list chat messages: %w", err)
	}
	defer rows.Close()

	var messages []port.ChatMessage
	for rows.Next() {
		message, err := scanChatMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}

func (r *ChatRepository) DeleteSession(ctx context.Context, id uuid.UUID) error {
	var deletedID uuid.UUID
	err := r.pool.QueryRow(ctx, `
		DELETE FROM chat_sessions
		WHERE id = $1
		RETURNING id
	`, id).Scan(&deletedID)
	if err == pgx.ErrNoRows {
		return port.ErrChatSessionNotFound
	}
	if err != nil {
		return fmt.Errorf("delete chat session: %w", err)
	}
	return nil
}

type chatSessionScannable interface {
	Scan(dest ...any) error
}

func scanChatSession(row chatSessionScannable) (port.ChatSession, error) {
	var session port.ChatSession
	err := row.Scan(
		&session.ID,
		&session.Title,
		&session.Summary,
		&session.SummaryUpdatedAt,
		&session.CreatedAt,
		&session.UpdatedAt,
	)
	if err != nil {
		return port.ChatSession{}, err
	}
	return session, nil
}

func scanChatMessage(row chatSessionScannable) (port.ChatMessage, error) {
	var message port.ChatMessage
	var sourcesJSON []byte
	err := row.Scan(
		&message.ID,
		&message.SessionID,
		&message.Role,
		&message.Content,
		&sourcesJSON,
		&message.CreatedAt,
	)
	if err != nil {
		return port.ChatMessage{}, err
	}
	if len(sourcesJSON) > 0 {
		_ = json.Unmarshal(sourcesJSON, &message.Sources)
	}
	if message.Sources == nil {
		message.Sources = []port.ChatSource{}
	}
	return message, nil
}
