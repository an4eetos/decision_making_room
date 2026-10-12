package usecase

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
)

const (
	chatSummaryTriggerMessages = 8
	chatSummaryKeepRecent      = 8
	summaryTimeout             = 45 * time.Second
)

type Chat struct {
	repo port.ChatRepository
	llm  port.LLM
	// consult keeps RAG behavior in one place.
	consult *Consult
	// observers hear about each finished turn. They must return immediately.
	observers []port.TurnObserver
	// board keeps beast mode's timed orders. Nil runs beast mode without clocks.
	board port.BeastBoard
}

type ChatSessionDTO struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Summary    string    `json:"summary,omitempty"`
	Tier       string    `json:"tier,omitempty"`
	Generals   []string  `json:"generals"`
	Mode       string    `json:"mode,omitempty"`
	ModeLocked bool      `json:"mode_locked"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type ChatMessageDTO struct {
	ID         string          `json:"id"`
	Role       string          `json:"role"`
	Content    string          `json:"content"`
	Sources    []ConsultSource `json:"sources,omitempty"`
	Tier       string          `json:"tier,omitempty"`
	Generals   []string        `json:"generals,omitempty"`
	Method     string          `json:"generals_method,omitempty"`
	Mode       string          `json:"mode,omitempty"`
	ModeMethod string          `json:"mode_method,omitempty"`
	// Suggestion is the room recommending an interrogation, signed by a general.
	Suggestion *domain.Suggestion `json:"suggestion,omitempty"`
	CreatedAt  time.Time          `json:"created_at"`
}

type ChatSessionDetail struct {
	Session  ChatSessionDTO   `json:"session"`
	Messages []ChatMessageDTO `json:"messages"`
}

func NewChat(repo port.ChatRepository, llm port.LLM, consult *Consult, observers ...port.TurnObserver) *Chat {
	return &Chat{repo: repo, llm: llm, consult: consult, observers: observers}
}

// WithBeastBoard gives beast mode its clocks.
func (c *Chat) WithBeastBoard(board port.BeastBoard) *Chat {
	c.board = board
	return c
}

// StartSession opens a conversation with an assistant message already in it.
// Check-ins use it, so that answering one is just replying — there is no second
// conversation mechanism to build or maintain.
func (c *Chat) StartSession(ctx context.Context, title, content, modeID string) (ChatSessionDTO, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Check-in"
	}

	session, err := c.repo.CreateSession(ctx, port.ChatSession{Title: title, ModeID: modeID})
	if err != nil {
		return ChatSessionDTO{}, err
	}

	if _, err := c.repo.CreateMessage(ctx, port.ChatMessage{
		SessionID: session.ID,
		Role:      "assistant",
		Content:   content,
		ModeID:    modeID,
	}); err != nil {
		return ChatSessionDTO{}, err
	}

	return toChatSessionDTO(session), nil
}

// SetMode pins the conversation to a mode, or with "" hands it back to
// detection, without sending anything. Leaving an interrogation uses it: the
// session must stop being locked the moment you leave, not on your next message,
// or a reload would put you straight back in.
func (c *Chat) SetMode(ctx context.Context, sessionID, modeID string) (ChatSessionDTO, error) {
	id, err := uuid.Parse(sessionID)
	if err != nil {
		return ChatSessionDTO{}, fmt.Errorf("invalid session id")
	}
	session, err := c.repo.GetSession(ctx, id)
	if err != nil {
		return ChatSessionDTO{}, err
	}
	session.ModeID = strings.TrimSpace(modeID)
	session.ModeLocked = session.ModeID != ""
	if err := c.repo.UpdateSession(ctx, session); err != nil {
		return ChatSessionDTO{}, err
	}
	return toChatSessionDTO(session), nil
}

func (c *Chat) CreateSession(ctx context.Context, title string) (ChatSessionDTO, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "New chat"
	}

	session, err := c.repo.CreateSession(ctx, port.ChatSession{Title: title})
	if err != nil {
		return ChatSessionDTO{}, err
	}
	return toChatSessionDTO(session), nil
}

func (c *Chat) ListSessions(ctx context.Context) ([]ChatSessionDTO, error) {
	sessions, err := c.repo.ListSessions(ctx, 100)
	if err != nil {
		return nil, err
	}
	out := make([]ChatSessionDTO, len(sessions))
	for i, session := range sessions {
		out[i] = toChatSessionDTO(session)
	}
	return out, nil
}

func (c *Chat) DeleteSession(ctx context.Context, sessionID string) error {
	id, err := uuid.Parse(sessionID)
	if err != nil {
		return fmt.Errorf("invalid session id")
	}
	return c.repo.DeleteSession(ctx, id)
}

func (c *Chat) GetSession(ctx context.Context, sessionID string) (ChatSessionDetail, error) {
	id, err := uuid.Parse(sessionID)
	if err != nil {
		return ChatSessionDetail{}, fmt.Errorf("invalid session id")
	}

	session, err := c.repo.GetSession(ctx, id)
	if err != nil {
		return ChatSessionDetail{}, err
	}
	messages, err := c.repo.ListMessages(ctx, id)
	if err != nil {
		return ChatSessionDetail{}, err
	}

	return ChatSessionDetail{
		Session:  toChatSessionDTO(session),
		Messages: toChatMessagesDTO(messages),
	}, nil
}

// SendMessageInput carries the per-turn choices. Tier is optional; empty falls
// back to the session's, then to the configured default.
type SendMessageInput struct {
	SessionID string
	Question  string
	Tier      string
	// Generals is the user's pick for this turn. Nil leaves the session's pick
	// alone; an explicitly empty slice clears it back to auto-select.
	Generals []string
	// Mode set by hand for this turn. It locks the session to that mode; an
	// empty string means "auto" and unlocks it.
	Mode *string
	// Conclude closes the session's mode with its position: "Take a position"
	// at the end of an interrogation.
	Conclude bool
	// Beast is what a beast-mode turn is about besides the text: a clock that
	// ran out, or a task reported done. Nil is an ordinary message.
	Beast *BeastEventInput
	// Progress, when set, receives the answer as it is written; see Progress.
	Progress *Progress
}

// BeastEventInput is a beast-mode event sent with a turn. Kind is "expired" or
// "report"; TaskIDs are the reported tasks. An expiry carries no ids: the
// board decides what actually ran out, so a stale tab cannot invent one.
type BeastEventInput struct {
	Kind    string
	TaskIDs []string
}

func (c *Chat) SendMessage(ctx context.Context, in SendMessageInput) (ChatSessionDetail, error) {
	sessionID, question := in.SessionID, in.Question
	id, err := uuid.Parse(sessionID)
	if err != nil {
		return ChatSessionDetail{}, fmt.Errorf("invalid session id")
	}
	question = strings.TrimSpace(question)

	session, err := c.repo.GetSession(ctx, id)
	if err != nil {
		return ChatSessionDetail{}, err
	}

	messages, err := c.repo.ListMessages(ctx, id)
	if err != nil {
		return ChatSessionDetail{}, err
	}

	modeID := session.ModeID
	if in.Mode != nil {
		modeID = *in.Mode
	}
	beast := modeID == beastModeID && c.board != nil
	var brief *port.BeastBrief
	if beast {
		// Before the user message is stored: an expiry that finds nothing
		// expired must leave no trace and cost no model call.
		brief, err = c.beastBrief(ctx, id, in.Beast, messages)
		if err != nil {
			return ChatSessionDetail{}, err
		}
		if brief.Event.Kind == "expired" {
			question = expiryQuestion(brief.Event.Tasks)
		}
	}
	if question == "" {
		return ChatSessionDetail{}, fmt.Errorf("question is required")
	}

	userMessage, err := c.repo.CreateMessage(ctx, port.ChatMessage{
		SessionID: id,
		Role:      "user",
		Content:   question,
	})
	if err != nil {
		return ChatSessionDetail{}, err
	}
	messages = append(messages, userMessage)

	if strings.TrimSpace(session.Title) == "" || session.Title == "New chat" {
		session.Title = deriveChatTitle(question)
		if err := c.repo.UpdateSession(ctx, session); err != nil {
			return ChatSessionDetail{}, err
		}
	}

	// Per-turn tier wins, then the session's, then the configured default. An
	// explicit pick also becomes the session default, so a conversation you took
	// deep stays deep without re-picking every turn.
	tier := strings.TrimSpace(in.Tier)
	if tier == "" {
		tier = session.Tier
	} else if tier != session.Tier {
		session.Tier = tier
		if err := c.repo.UpdateSession(ctx, session); err != nil {
			return ChatSessionDetail{}, err
		}
	}

	// A pick made this turn becomes the session's, the same way depth does.
	if in.Generals != nil && !sameStrings(in.Generals, session.Generals) {
		session.Generals = in.Generals
		if err := c.repo.UpdateSession(ctx, session); err != nil {
			return ChatSessionDetail{}, err
		}
	}

	// Choosing a mode by hand locks the session to it, so detection cannot pull
	// the conversation back out from under the user. Choosing "auto" unlocks it.
	if in.Mode != nil {
		session.ModeID = *in.Mode
		session.ModeLocked = *in.Mode != ""
		if err := c.repo.UpdateSession(ctx, session); err != nil {
			return ChatSessionDetail{}, err
		}
	}

	conversationHistory := buildChatHistory(session.Summary, messages[:len(messages)-1])
	consultResult, err := c.consult.Execute(ctx, ConsultInput{
		Question:       question,
		History:        conversationHistory,
		Tier:           tier,
		GeneralIDs:     session.Generals,
		RecentGenerals: recentGenerals(messages),
		SeatedGenerals: seatedGenerals(messages),
		SessionMode:    session.ModeID,
		ModeLocked:     session.ModeLocked,
		// The user turn was already appended, so a fresh conversation has one
		// message here and stickiness must not apply to it.
		TurnIndex: len(messages) - 1,
		Conclude:  in.Conclude,
		Beast:     brief,
		Progress:  in.Progress,
	})
	if err != nil {
		return ChatSessionDetail{}, err
	}

	suggestion := consultResult.Suggestion
	if suggestedRecently(messages) {
		suggestion = nil
	}

	assistantMessage, err := c.repo.CreateMessage(ctx, port.ChatMessage{
		SessionID:    id,
		Role:         "assistant",
		Content:      consultResult.Answer,
		Sources:      toChatSources(consultResult.Sources),
		Tier:         consultResult.Tier,
		Generals:     consultResult.Generals,
		ModeID:       consultResult.Mode,
		DetectMethod: consultResult.ModeMethod,
		Suggestion:   suggestion,
	})
	if err != nil {
		return ChatSessionDetail{}, err
	}
	messages = append(messages, assistantMessage)
	if in.Progress != nil {
		answer := toChatMessagesDTO([]port.ChatMessage{assistantMessage})[0]
		answer.Method = consultResult.GeneralsMethod
		in.Progress.answer(answer)
	}

	if beast {
		c.applyBeast(ctx, id, assistantMessage.ID, consultResult, brief, in.Conclude)
	}

	// Beast-mode orders live on their own board with their own clocks. Handing
	// the turn to commitments and the campaign would propose each order a
	// second time as a loop nobody asked for.
	observers := c.observers
	if beast {
		observers = nil
	}
	for _, observer := range observers {
		observer.ObserveTurn(port.Turn{
			SessionID:     id,
			MessageID:     assistantMessage.ID,
			UserText:      question,
			AssistantText: consultResult.Answer,
			ModeID:        consultResult.Mode,
		})
	}

	// Remember the detected mode so the conversation stays in it, unless the
	// user has locked one by hand.
	if !session.ModeLocked && consultResult.Mode != "" && consultResult.Mode != session.ModeID {
		session.ModeID = consultResult.Mode
		if err := c.repo.UpdateSession(ctx, session); err != nil {
			return ChatSessionDetail{}, err
		}
	}

	if err := c.maybeSummarize(ctx, &session, messages); err != nil {
		return ChatSessionDetail{}, err
	}

	updatedSession, err := c.repo.GetSession(ctx, id)
	if err != nil {
		return ChatSessionDetail{}, err
	}
	updatedMessages, err := c.repo.ListMessages(ctx, id)
	if err != nil {
		return ChatSessionDetail{}, err
	}

	return ChatSessionDetail{
		Session:  toChatSessionDTO(updatedSession),
		Messages: toChatMessagesDTO(updatedMessages),
	}, nil
}

func (c *Chat) maybeSummarize(ctx context.Context, session *port.ChatSession, messages []port.ChatMessage) error {
	if len(messages) < chatSummaryTriggerMessages {
		return nil
	}

	cutoff := len(messages) - chatSummaryKeepRecent
	if cutoff <= 0 {
		return nil
	}

	// The answer is already on screen; this is housekeeping the person still
	// waits on before they can send the next message. A busy model gets a
	// short leash and the old summary stands.
	ctx, cancel := context.WithTimeout(ctx, summaryTimeout)
	defer cancel()

	summaryPrompt := buildSummaryPrompt(session.Summary, messages[:cutoff])
	answer, err := c.llm.Chat(ctx, []port.Message{
		{Role: "system", Content: "Summarize the conversation so future answers can preserve user intent, commitments, preferences, and unresolved questions. Be concise and factual."},
		{Role: "user", Content: summaryPrompt},
	})
	if err != nil {
		return nil
	}
	answer = strings.TrimSpace(answer)
	if answer == "" || answer == session.Summary {
		return nil
	}

	now := time.Now().UTC()
	session.Summary = answer
	session.SummaryUpdatedAt = &now
	return c.repo.UpdateSession(ctx, *session)
}

func buildChatHistory(summary string, messages []port.ChatMessage) []port.Message {
	var history []port.Message

	if strings.TrimSpace(summary) != "" {
		history = append(history, port.Message{
			Role:    "user",
			Content: "Earlier conversation summary:\n" + strings.TrimSpace(summary),
		})
	}

	start := 0
	if len(messages) > chatSummaryKeepRecent {
		start = len(messages) - chatSummaryKeepRecent
	}

	for _, message := range messages[start:] {
		role := "assistant"
		if message.Role == "user" {
			role = "user"
		}
		history = append(history, port.Message{
			Role:    role,
			Content: strings.TrimSpace(message.Content),
		})
	}

	return history
}

func buildSummaryPrompt(existingSummary string, messages []port.ChatMessage) string {
	var b strings.Builder
	if strings.TrimSpace(existingSummary) != "" {
		b.WriteString("Existing summary:\n")
		b.WriteString(strings.TrimSpace(existingSummary))
		b.WriteString("\n\n")
	}
	b.WriteString("New conversation turns:\n")
	for _, message := range messages {
		role := "Assistant"
		if message.Role == "user" {
			role = "User"
		}
		fmt.Fprintf(&b, "%s: %s\n", role, strings.TrimSpace(message.Content))
	}
	return b.String()
}

func deriveChatTitle(question string) string {
	question = strings.TrimSpace(question)
	if len(question) <= 60 {
		return question
	}
	return question[:60] + "..."
}

func toChatSessionDTO(session port.ChatSession) ChatSessionDTO {
	return ChatSessionDTO{
		ID:         session.ID.String(),
		Title:      session.Title,
		Summary:    session.Summary,
		Tier:       session.Tier,
		Generals:   nonNilIDs(session.Generals),
		Mode:       session.ModeID,
		ModeLocked: session.ModeLocked,
		CreatedAt:  session.CreatedAt,
		UpdatedAt:  session.UpdatedAt,
	}
}

func toChatMessagesDTO(messages []port.ChatMessage) []ChatMessageDTO {
	out := make([]ChatMessageDTO, len(messages))
	for i, message := range messages {
		out[i] = ChatMessageDTO{
			ID:         message.ID.String(),
			Role:       message.Role,
			Content:    message.Content,
			Sources:    fromChatSources(message.Sources),
			Tier:       message.Tier,
			Generals:   message.Generals,
			Mode:       message.ModeID,
			ModeMethod: message.DetectMethod,
			Suggestion: message.Suggestion,
			CreatedAt:  message.CreatedAt,
		}
	}
	return out
}

func toChatSources(sources []ConsultSource) []port.ChatSource {
	out := make([]port.ChatSource, len(sources))
	for i, source := range sources {
		out[i] = port.ChatSource{
			ID:    source.ID,
			Kind:  string(source.Kind),
			Title: source.Title,
			Score: source.Score,
		}
	}
	return out
}

func fromChatSources(sources []port.ChatSource) []ConsultSource {
	out := make([]ConsultSource, len(sources))
	for i, source := range sources {
		out[i] = ConsultSource{
			ID:    source.ID,
			Kind:  domain.MemoryKind(source.Kind),
			Title: source.Title,
			Score: source.Score,
		}
	}
	return out
}

// recentGenerals collects the lenses used in the last two assistant turns, so
// selection can demote them and one lens does not answer everything.
func recentGenerals(messages []port.ChatMessage) []string {
	var out []string
	turns := 0
	for i := len(messages) - 1; i >= 0 && turns < 2; i-- {
		if messages[i].Role != "assistant" {
			continue
		}
		turns++
		out = append(out, messages[i].Generals...)
	}
	return out
}

// suggestionCooldown is how many recent answers are checked for an earlier
// suggestion. A room that recommends an interrogation every turn is a room
// whose recommendations get ignored.
const suggestionCooldown = 4

// suggestedRecently reports whether one of the last few answers already made a
// suggestion, or the conversation has been interrogated in that time — there
// is no point offering what just happened.
func suggestedRecently(messages []port.ChatMessage) bool {
	turns := 0
	for i := len(messages) - 1; i >= 0 && turns < suggestionCooldown; i-- {
		if messages[i].Role != "assistant" {
			continue
		}
		turns++
		if messages[i].Suggestion != nil || messages[i].ModeID == interrogationModeID {
			return true
		}
	}
	return false
}

// interrogationModeID is the shipped interrogation mode. The cooldown keys off
// it; everything else asks the mode itself.
const interrogationModeID = "interrogation"

// seatedGenerals is the roster of the latest answer that had one. Answers
// written without lenses, like check-ins, do not empty the bench.
func seatedGenerals(messages []port.ChatMessage) []string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" && len(messages[i].Generals) > 0 {
			return messages[i].Generals
		}
	}
	return nil
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// nonNilIDs keeps the JSON field an array rather than null, so the frontend can
// treat "no pick" and "empty pick" the same way.
func nonNilIDs(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// expiryPrefix opens the message the room writes for you when a clock runs
// out. The eruption cooldown finds earlier eruptions by it.
const expiryPrefix = "⏱ Time's up:"

// eruptionCooldown spaces the generals' unprompted eruptions. Clocks that run
// out inside it wait and are all shouted about at once.
const eruptionCooldown = 2 * time.Minute

// beastBrief applies this turn's event to the board and reads the board back
// for the shouter.
func (c *Chat) beastBrief(ctx context.Context, sessionID uuid.UUID, event *BeastEventInput, messages []port.ChatMessage) (*port.BeastBrief, error) {
	now := time.Now().UTC()
	brief := &port.BeastBrief{Now: now}

	if event != nil {
		switch event.Kind {
		case "expired":
			if eruptedRecently(messages, now) {
				return nil, port.ErrEruptionTooSoon
			}
			expired, err := c.board.Expire(ctx, sessionID, now)
			if err != nil {
				return nil, err
			}
			if len(expired) == 0 {
				return nil, port.ErrNothingExpired
			}
			brief.Event = port.BeastEvent{Kind: "expired", Tasks: expired}
		case "report":
			ids := make([]uuid.UUID, 0, len(event.TaskIDs))
			for _, raw := range event.TaskIDs {
				if taskID, err := uuid.Parse(raw); err == nil {
					ids = append(ids, taskID)
				}
			}
			reported, err := c.board.Report(ctx, sessionID, ids)
			if err != nil {
				return nil, err
			}
			if len(reported) > 0 {
				brief.Event = port.BeastEvent{Kind: "report", Tasks: reported}
			}
		}
	}

	tasks, err := c.board.Tasks(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	brief.Tasks = tasks
	if brief.Loops, err = c.board.DueLoops(ctx, now); err != nil {
		return nil, err
	}
	return brief, nil
}

// applyBeast records the turn on the board. The answer is already stored, so a
// board failure is logged rather than failing a turn the person has read.
func (c *Chat) applyBeast(ctx context.Context, sessionID, messageID uuid.UUID, result ConsultResult, brief *port.BeastBrief, conclude bool) {
	var err error
	if conclude {
		err = c.board.StandDown(ctx, sessionID)
	} else {
		var loops []port.BeastLoop
		if brief != nil {
			loops = brief.Loops
		}
		err = c.board.Apply(ctx, sessionID, messageID, result.Orders, result.Done, loops)
	}
	if err != nil {
		log.Printf("beast board: %v", err)
	}
}

func expiryQuestion(tasks []port.BeastTask) string {
	names := make([]string, 0, len(tasks))
	for _, t := range tasks {
		names = append(names, fmt.Sprintf("task %d, %s", t.Number, t.Text))
	}
	return expiryPrefix + " " + strings.Join(names, "; ") + ". Not reported."
}

func eruptedRecently(messages []port.ChatMessage, now time.Time) bool {
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.Role == "user" && strings.HasPrefix(m.Content, expiryPrefix) {
			return now.Sub(m.CreatedAt) < eruptionCooldown
		}
	}
	return false
}
