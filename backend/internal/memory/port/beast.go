package port

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// BeastTask is one timed order on a beast-mode board. Number is its place in
// the session, starting at one, so the generals and the person can both say
// "task 2" and mean the same row.
type BeastTask struct {
	ID         uuid.UUID
	Number     int
	Text       string
	Minutes    int
	DeadlineAt time.Time
	// Status is "open", "done" or "abandoned".
	Status   string
	Expiries int
}

// Overdue reports an open task whose clock has run out.
func (t BeastTask) Overdue(now time.Time) bool {
	return t.Status == "open" && !t.DeadlineAt.After(now)
}

// BeastLoop is an open loop due today or overdue: the crisis that made the
// person reach for the button. Number is its place in the brief, so an order
// can say which loop it serves.
type BeastLoop struct {
	ID     uuid.UUID
	Number int
	Text   string
	DueAt  *time.Time
}

// BeastOrder is an order the generals issued, parsed out of their answer.
// Loop is the number of the open loop it serves, or zero.
type BeastOrder struct {
	Minutes int
	Text    string
	Loop    int
}

// BeastEvent is what this turn is about, besides the person's message.
type BeastEvent struct {
	// Kind is "", "expired" or "report".
	Kind  string
	Tasks []BeastTask
}

// BeastBrief is everything the shouter reads about the fight in progress.
type BeastBrief struct {
	Tasks []BeastTask
	Loops []BeastLoop
	Event BeastEvent
	Now   time.Time
}

// ErrNothingExpired means an expiry turn arrived but no clock had actually run
// out: a second tab, a skewed clock, or a task reported a moment before. No
// model call is made for it.
var ErrNothingExpired = errors.New("no beast task has expired")

// ErrEruptionTooSoon means the generals erupted on their own too recently. The
// tasks stay overdue and the next attempt picks them all up at once.
var ErrEruptionTooSoon = errors.New("last beast eruption was too recent")

// BeastBoard keeps the timed orders. It is implemented outside the memory
// module and is optional: without it beast mode still shouts, it just has no
// clocks.
type BeastBoard interface {
	Tasks(ctx context.Context, sessionID uuid.UUID) ([]BeastTask, error)
	DueLoops(ctx context.Context, now time.Time) ([]BeastLoop, error)
	// Expire re-arms every open task whose deadline has passed and returns
	// them re-armed, so the new deadline is what the generals hold them to.
	Expire(ctx context.Context, sessionID uuid.UUID, now time.Time) ([]BeastTask, error)
	// Report marks the given tasks done and returns them. Tasks already done
	// come back as they are; abandoned ones and other sessions' are skipped.
	Report(ctx context.Context, sessionID uuid.UUID, taskIDs []uuid.UUID) ([]BeastTask, error)
	// Apply records what the generals did this turn: new orders, and tasks the
	// person said were finished, by number. loops is the brief the orders saw,
	// so a loop number resolves to the row it meant.
	Apply(ctx context.Context, sessionID, messageID uuid.UUID, orders []BeastOrder, done []int, loops []BeastLoop) error
	// StandDown abandons whatever is still open.
	StandDown(ctx context.Context, sessionID uuid.UUID) error
}

type relaxedSafetyKey struct{}

// WithRelaxedSafety marks a model call as one where harsh language aimed at the
// person is the point. Providers with harassment filters loosen them for this
// call only; hate-speech filters stay where they were.
func WithRelaxedSafety(ctx context.Context) context.Context {
	return context.WithValue(ctx, relaxedSafetyKey{}, true)
}

// RelaxedSafety reports whether WithRelaxedSafety was applied.
func RelaxedSafety(ctx context.Context) bool {
	v, _ := ctx.Value(relaxedSafetyKey{}).(bool)
	return v
}
