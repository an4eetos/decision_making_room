package gemini

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/an4eetos/decision-room/internal/memory/port"
)

const secretKey = "AQ.test-secret-key-0123456789"

// The key travels in a header. In the query string, Go's HTTP errors quoted
// it, and every timeout wrote it to the log.
func TestKeyIsSentInAHeaderNotTheURL(t *testing.T) {
	t.Parallel()

	var gotHeader, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("x-goog-api-key")
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, secretKey, []string{"test-model"}, "embed")
	if _, err := client.Chat(context.Background(), []port.Message{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatal(err)
	}
	if gotHeader != secretKey {
		t.Fatalf("x-goog-api-key = %q, want the key", gotHeader)
	}
	if strings.Contains(gotQuery, secretKey) || strings.Contains(gotQuery, "key=") {
		t.Fatalf("the key must not be in the URL: %q", gotQuery)
	}
}

// A request that fails before any response — the timeout that leaked the key
// in the first place — must produce an error without it.
func TestTransportErrorsDoNotCarryTheKey(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer server.Close()

	client := NewClient(server.URL, secretKey, []string{"test-model"}, "embed")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	for name, call := range map[string]func() error{
		"chat":  func() error { _, err := client.Chat(ctx, []port.Message{{Role: "user", Content: "hi"}}); return err },
		"embed": func() error { _, err := client.Embed(ctx, "text"); return err },
		"stream": func() error {
			_, err := client.ChatStream(ctx, []port.Message{{Role: "user", Content: "hi"}}, func(string) {})
			return err
		},
	} {
		err := call()
		if err == nil {
			t.Fatalf("%s: expected a timeout", name)
		}
		if strings.Contains(err.Error(), secretKey) {
			t.Fatalf("%s: the error leaks the key: %v", name, err)
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s: redaction must keep the cause visible to errors.Is: %v", name, err)
		}
	}
}

// The second line: a key that reaches an error by any other route is redacted,
// and the cause survives.
func TestRedactHidesTheKeyAndKeepsTheCause(t *testing.T) {
	t.Parallel()

	client := NewClient("http://example.invalid", secretKey, []string{"m"}, "embed")
	cause := &wrapped{msg: "dial https://host/?key=" + secretKey, err: context.Canceled}

	got := client.redact(cause)
	if strings.Contains(got.Error(), secretKey) || !strings.Contains(got.Error(), "[REDACTED]") {
		t.Fatalf("not redacted: %v", got)
	}
	if !errors.Is(got, context.Canceled) {
		t.Fatal("the cause must survive redaction")
	}
	if plain := errors.New("no key here"); client.redact(plain) != plain {
		t.Fatal("an error without the key should pass through untouched")
	}
}

type wrapped struct {
	msg string
	err error
}

func (w *wrapped) Error() string { return w.msg }
func (w *wrapped) Unwrap() error { return w.err }
