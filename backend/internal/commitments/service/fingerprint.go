// Package service holds the pure logic: deduplication, the cheap prefilter that
// decides whether extraction is worth a model call, and parsing what comes back.
package service

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"unicode"
)

// Fingerprint normalises a commitment so the same promise made twice, in
// slightly different words, is one row: "Ship the MVP by Friday" and "ship MVP
// by friday!" collapse together.
//
// Sorted tokens rather than the raw string, so word order does not matter, and
// filler words are dropped so they cannot make two identical promises distinct.
func Fingerprint(text string) string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	seen := make(map[string]struct{}, len(words))
	kept := make([]string, 0, len(words))
	for _, w := range words {
		if _, filler := fillerWords[w]; filler {
			continue
		}
		if _, dup := seen[w]; dup {
			continue
		}
		seen[w] = struct{}{}
		kept = append(kept, w)
	}
	sort.Strings(kept)

	sum := sha256.Sum256([]byte(strings.Join(kept, " ")))
	return hex.EncodeToString(sum[:])
}

var fillerWords = toSet(`
a an the to i ll will i'll im i'm going gonna need must should have got
my me by on at for of and or this that it be
`)

func toSet(list string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, w := range strings.Fields(list) {
		set[w] = struct{}{}
	}
	return set
}
