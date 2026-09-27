package usecase

import (
	"context"
	"log"
	"os/exec"
	"strings"
	"time"
)

// Notifier runs a local command when a check-in arrives — terminal-notifier on
// macOS, notify-send on Linux. Optional and off by default, because it runs a
// command on your machine.
//
// The command is split into arguments and executed directly, never through a
// shell. The body is model-written text, and with a shell in the way a check-in
// containing "; rm -rf ~" would be a command. Here it is only ever an argument.
type Notifier struct {
	argv []string
}

func NewNotifier(command string) *Notifier {
	return &Notifier{argv: strings.Fields(command)}
}

func (n *Notifier) Enabled() bool { return n != nil && len(n.argv) > 0 }

// Notify substitutes {title} and {body} into each argument and runs the command.
// Failures are logged and swallowed: a notification that did not show is not
// worth losing the check-in over.
func (n *Notifier) Notify(title, body string) {
	if !n.Enabled() {
		return
	}

	body = oneLine(body, 180)
	args := make([]string, len(n.argv))
	for i, a := range n.argv {
		a = strings.ReplaceAll(a, "{title}", title)
		args[i] = strings.ReplaceAll(a, "{body}", body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if out, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput(); err != nil {
		log.Printf("check-in: notify command failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}
