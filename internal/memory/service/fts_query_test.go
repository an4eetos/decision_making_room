package service

import (
	"slices"
	"strings"
	"testing"
)

func TestBuildFTSQueryKeepsContentWordsOnly(t *testing.T) {
	t.Parallel()

	q, ok := BuildFTSQuery("What should I focus on today given the backlog?")
	if !ok {
		t.Fatal("expected a usable query")
	}

	for _, unwanted := range []string{"what", "should", "the", "on"} {
		if slices.Contains(q.Terms, unwanted) {
			t.Fatalf("stopword %q survived: %v", unwanted, q.Terms)
		}
	}
	for _, wanted := range []string{"focus", "today", "backlog"} {
		if !slices.Contains(q.Terms, wanted) {
			t.Fatalf("content word %q was dropped: %v", wanted, q.Terms)
		}
	}
}

// The whole point of the rewrite: terms are OR-joined. websearch_to_tsquery
// AND-joins them, which is why the full-text arm returned nothing for any
// conversational question.
func TestBuildFTSQueryOrJoinsTerms(t *testing.T) {
	t.Parallel()

	q, _ := BuildFTSQuery("postgres indexes and query plans")
	if !strings.Contains(q.English, " | ") {
		t.Fatalf("expected an OR-joined tsquery, got %q", q.English)
	}
	if strings.Contains(q.English, "&") {
		t.Fatalf("expected no AND in the tsquery, got %q", q.English)
	}
}

// to_tsquery raises an error on malformed input rather than returning no rows,
// and that error fails the whole retrieval. Nothing but [a-z0-9] may reach it.
func TestBuildFTSQueryStripsTsqueryMetacharacters(t *testing.T) {
	t.Parallel()

	q, ok := BuildFTSQuery("what about (postgres & 'indexes') | plans:*!")
	if !ok {
		t.Fatal("expected a usable query")
	}
	for _, term := range q.Terms {
		for _, r := range term {
			isSafe := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
			if !isSafe {
				t.Fatalf("unsafe rune %q survived in term %q", r, term)
			}
		}
	}
}

func TestBuildFTSQueryRejectsQuestionsWithNoContent(t *testing.T) {
	t.Parallel()

	for _, question := range []string{"", "   ", "?!", "hi", "ok", "what about it"} {
		if q, ok := BuildFTSQuery(question); ok {
			t.Fatalf("BuildFTSQuery(%q) returned %v; expected it to be skipped", question, q.Terms)
		}
	}
}

func TestBuildFTSQueryCapsTermCount(t *testing.T) {
	t.Parallel()

	q, ok := BuildFTSQuery(strings.Repeat("alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima mike november ", 2))
	if !ok {
		t.Fatal("expected a usable query")
	}
	if len(q.Terms) > maxFTSTerms {
		t.Fatalf("got %d terms, capped at %d", len(q.Terms), maxFTSTerms)
	}
}

func TestBuildFTSQueryDeduplicates(t *testing.T) {
	t.Parallel()

	q, _ := BuildFTSQuery("backlog backlog BACKLOG focus")
	if len(q.Terms) != 2 {
		t.Fatalf("expected 2 distinct terms, got %v", q.Terms)
	}
}
