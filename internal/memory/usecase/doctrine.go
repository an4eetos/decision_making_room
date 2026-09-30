package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/sync/errgroup"

	gendomain "github.com/an4eetos/decision-room/internal/generals/domain"
	genport "github.com/an4eetos/decision-room/internal/generals/port"
	"github.com/an4eetos/decision-room/internal/memory/port"
)

// DoctrineIndex picks, for each lens on an answer, the doctrine passages that
// bear on the question.
//
// Cards alone made the doctrine decorative: it reached the model only on the
// deep tier, and only when the model thought to call read_doctrine. Injecting
// all of it is what the cards replaced. This is the middle — one or two
// passages per lens, ~225 tokens each, chosen by relevance.
//
// Selection costs no model call and no embedding call. Passage vectors are
// built once and cached in Postgres; the question vector is the one retrieval
// already computed, served from MemoEmbedder.
type DoctrineIndex struct {
	embedder port.Embedder
	store    port.DoctrineVectorStore

	// byLens is fixed at construction; the roster is immutable after startup.
	byLens map[string][]indexedPassage

	mu      sync.RWMutex
	vectors map[string][]float32 // by passage hash
}

type indexedPassage struct {
	gendomain.Passage
	hash  string
	terms map[string]struct{}
}

func NewDoctrineIndex(registry genport.Registry, embedder port.Embedder, store port.DoctrineVectorStore) *DoctrineIndex {
	x := &DoctrineIndex{
		embedder: embedder,
		store:    store,
		byLens:   make(map[string][]indexedPassage),
		vectors:  make(map[string][]float32),
	}
	if registry == nil {
		return x
	}
	for _, lens := range registry.Generals() {
		for _, p := range lens.Passages() {
			text := p.EmbedText()
			sum := sha256.Sum256([]byte(text))
			x.byLens[lens.ID] = append(x.byLens[lens.ID], indexedPassage{
				Passage: p,
				hash:    hex.EncodeToString(sum[:16]),
				terms:   termSet(text),
			})
		}
	}
	return x
}

// doctrineEmbedConcurrency bounds the first build. Twenty generals are about a
// hundred and fifty passages; unbounded, that is a burst a free quota rejects.
const doctrineEmbedConcurrency = 4

// Build loads cached vectors and embeds whatever is missing. Until it finishes,
// and for any lens it could not embed, selection falls back to word overlap, so
// a slow or failing embedder degrades the choice of passage, never the answer.
func (x *DoctrineIndex) Build(ctx context.Context) error {
	if x.embedder == nil {
		return nil
	}
	model := x.embedder.ModelID()

	cached := map[string][]float32{}
	if x.store != nil {
		loaded, err := x.store.Load(ctx, model)
		if err != nil {
			log.Printf("doctrine: could not load cached vectors, embedding all: %v", err)
		} else {
			cached = loaded
		}
	}

	var (
		keep    []string
		missing []indexedPassage
	)
	x.mu.Lock()
	for _, passages := range x.byLens {
		for _, p := range passages {
			keep = append(keep, p.hash)
			if vec, ok := cached[p.hash]; ok {
				x.vectors[p.hash] = vec
			} else {
				missing = append(missing, p)
			}
		}
	}
	x.mu.Unlock()

	var (
		failedMu sync.Mutex
		failed   int
	)
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(doctrineEmbedConcurrency)
	for _, p := range missing {
		group.Go(func() error {
			vec, err := x.embedder.Embed(groupCtx, p.EmbedText())
			if err != nil {
				failedMu.Lock()
				failed++
				failedMu.Unlock()
				// One passage failing should not stop the rest; the lens it
				// belongs to falls back to word overlap.
				return nil
			}
			x.mu.Lock()
			x.vectors[p.hash] = vec
			x.mu.Unlock()
			if x.store != nil {
				if err := x.store.Save(groupCtx, p.hash, model, vec); err != nil {
					log.Printf("doctrine: %v", err)
				}
			}
			return nil
		})
	}
	_ = group.Wait()

	if x.store != nil && failed == 0 {
		if err := x.store.Prune(ctx, model, keep); err != nil {
			log.Printf("doctrine: %v", err)
		}
	}

	log.Printf("doctrine: %d passages, %d from cache, %d embedded, %d failed",
		len(keep), len(keep)-len(missing), len(missing)-failed, failed)
	return nil
}

// modeSectionBoost is what a mode's preferred section adds to a cosine score.
// Small on purpose: it breaks near-ties toward the section the mode is about,
// and cannot drag in a passage that has nothing to do with the question.
const modeSectionBoost = 0.05

// Select returns, by lens id, the passages to put under each lens's card.
func (x *DoctrineIndex) Select(ctx context.Context, plan ConsultPlan) map[string][]gendomain.Passage {
	n := plan.Tier.DoctrinePassages
	if x == nil || n <= 0 || len(plan.Generals) == 0 {
		return nil
	}

	query := retrievalQuery(plan)
	var queryVec []float32
	if x.embedder != nil {
		// A cache hit in the normal flow: retrieval embedded this exact string.
		if vec, err := x.embedder.Embed(ctx, query); err == nil {
			queryVec = vec
		}
	}
	queryTerms := termSet(query)
	preferred := plan.Mode.Generals.Doctrine

	out := make(map[string][]gendomain.Passage, len(plan.Generals))
	for _, lens := range plan.Generals {
		passages := x.byLens[lens.ID]
		if len(passages) == 0 {
			continue
		}
		if picked := x.pick(passages, queryVec, queryTerms, preferred, n); len(picked) > 0 {
			out[lens.ID] = picked
		}
	}
	return out
}

type scoredPassage struct {
	indexedPassage
	score float64
}

func (x *DoctrineIndex) pick(passages []indexedPassage, queryVec []float32, queryTerms map[string]struct{}, preferred []string, n int) []gendomain.Passage {
	scored := x.score(passages, queryVec, queryTerms, preferred)
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].score > scored[j].score })

	// Two passages from one section say one thing twice. Take the best of each
	// section first, and only then fill from sections already used.
	var chosen, sameSection []gendomain.Passage
	used := map[string]bool{}
	for _, s := range scored {
		if used[s.Section] {
			sameSection = append(sameSection, s.Passage)
			continue
		}
		chosen = append(chosen, s.Passage)
		used[s.Section] = true
	}
	chosen = append(chosen, sameSection...)
	if len(chosen) > n {
		chosen = chosen[:n]
	}
	return chosen
}

// score uses cosine similarity when every passage of the lens has a vector, and
// word overlap otherwise. The two scales cannot be mixed within one lens.
//
// There is no fixed similarity floor in the vector path: cosine ranges differ
// enough between Gemini and nomic-embed-text that any constant is wrong for one
// of them. The lens was already chosen as relevant; its best passage is still
// the most useful thing it can say. The overlap path does have a floor, because
// zero shared words is an unambiguous "nothing here".
func (x *DoctrineIndex) score(passages []indexedPassage, queryVec []float32, queryTerms map[string]struct{}, preferred []string) []scoredPassage {
	x.mu.RLock()
	vectors := make([][]float32, len(passages))
	complete := queryVec != nil
	for i, p := range passages {
		vectors[i] = x.vectors[p.hash]
		if vectors[i] == nil {
			complete = false
		}
	}
	x.mu.RUnlock()

	out := make([]scoredPassage, 0, len(passages))
	for i, p := range passages {
		isPreferred := containsFold(preferred, p.Section)
		var s float64
		if complete {
			s = cosine(queryVec, vectors[i])
			if isPreferred {
				s += modeSectionBoost
			}
		} else {
			s = float64(overlap(queryTerms, p.terms))
			if isPreferred {
				s += 0.5
			}
			if s == 0 {
				continue
			}
		}
		out = append(out, scoredPassage{indexedPassage: p, score: s})
	}
	return out
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func overlap(a, b map[string]struct{}) int {
	n := 0
	for t := range a {
		if _, ok := b[t]; ok {
			n++
		}
	}
	return n
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(strings.TrimSpace(v), s) {
			return true
		}
	}
	return false
}

// termSet is a deliberately crude bag of words for the fallback path: short
// words and stopwords dropped, the rest cut to six letters so "encircled" and
// "encirclement" meet. It only has to beat picking a passage at random.
func termSet(text string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r)
	}) {
		r := []rune(w)
		if len(r) < 4 || doctrineStopwords[w] {
			continue
		}
		if len(r) > 6 {
			r = r[:6]
		}
		out[string(r)] = struct{}{}
	}
	return out
}

var doctrineStopwords = map[string]bool{
	"that": true, "this": true, "with": true, "have": true, "what": true, "when": true,
	"from": true, "they": true, "them": true, "their": true, "there": true, "then": true,
	"than": true, "were": true, "will": true, "would": true, "should": true, "could": true,
	"about": true, "into": true, "only": true, "just": true, "does": true, "your": true,
	"because": true, "which": true, "while": true, "where": true, "these": true, "those": true,
	"been": true, "being": true, "more": true, "most": true, "some": true, "such": true,
	"very": true, "also": true, "like": true, "want": true, "need": true, "know": true,
}
