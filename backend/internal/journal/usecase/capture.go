package usecase

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Capture appends a line to today's daily note.
//
// It writes to the journal folder rather than straight to the database so the
// file stays the source of truth: the watcher picks the change up and re-ingests,
// and what you captured is still there in plain markdown if you stop using this
// tool entirely.
type Capture struct {
	root string
	now  func() time.Time
}

func NewCapture(root string) *Capture {
	return &Capture{root: root, now: func() time.Time { return time.Now() }}
}

type CaptureResult struct {
	Path string `json:"path"`
	Date string `json:"date"`
}

// Execute appends one timestamped line. Local time deliberately: "today" means
// your today, and a note written at 1am should land on the day you think it did.
func (u *Capture) Execute(ctx context.Context, text string) (CaptureResult, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return CaptureResult{}, fmt.Errorf("nothing to capture")
	}
	if u.root == "" {
		return CaptureResult{}, fmt.Errorf("journal directory is not configured")
	}

	now := u.now()
	date := now.Format("2006-01-02")
	dir := filepath.Join(u.root, "daily")
	path := filepath.Join(dir, date+".md")

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return CaptureResult{}, fmt.Errorf("create daily directory: %w", err)
	}

	var entry strings.Builder
	// A new file gets a heading so the chunker has something to anchor on; it
	// splits on markdown headers, and a file without one is chunked blind.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Fprintf(&entry, "# %s\n\n", date)
	} else if err != nil {
		return CaptureResult{}, fmt.Errorf("stat daily note: %w", err)
	}

	fmt.Fprintf(&entry, "- %s — %s\n", now.Format("15:04"), text)

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return CaptureResult{}, fmt.Errorf("open daily note: %w", err)
	}
	defer file.Close()

	if _, err := file.WriteString(entry.String()); err != nil {
		return CaptureResult{}, fmt.Errorf("append to daily note: %w", err)
	}

	return CaptureResult{Path: filepath.Join("daily", date+".md"), Date: date}, nil
}
