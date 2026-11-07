package httpserver

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// pruneAbove is how many tracked addresses trigger a sweep of expired ones,
// so a scan from many addresses cannot grow the map without bound.
const pruneAbove = 1024

// Limiter counts attempts in fixed windows, per key and in total. The total
// is the backstop for an attacker who rotates addresses.
type Limiter struct {
	mu     sync.Mutex
	window time.Duration
	perKey int
	total  int
	keys   map[string]*window
	all    window
	now    func() time.Time
}

type window struct {
	start time.Time
	count int
}

func NewLimiter(perKey, total int, every time.Duration) *Limiter {
	return &Limiter{
		window: every,
		perKey: perKey,
		total:  total,
		keys:   make(map[string]*window),
		now:    time.Now,
	}
}

// Allow records an attempt for key. When either budget is spent it records
// nothing and says how long until the spent one resets.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if len(l.keys) > pruneAbove {
		for k, w := range l.keys {
			if now.Sub(w.start) >= l.window {
				delete(l.keys, k)
			}
		}
	}

	w, ok := l.keys[key]
	if !ok {
		w = &window{start: now}
		l.keys[key] = w
	}
	l.roll(w, now)
	l.roll(&l.all, now)

	if w.count >= l.perKey {
		return false, w.start.Add(l.window).Sub(now)
	}
	if l.all.count >= l.total {
		return false, l.all.start.Add(l.window).Sub(now)
	}
	w.count++
	l.all.count++
	return true, 0
}

// Reset forgets a key's attempts, after it succeeded.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.keys, key)
}

func (l *Limiter) roll(w *window, now time.Time) {
	if now.Sub(w.start) >= l.window {
		w.start = now
		w.count = 0
	}
}

// ClientIP is the address to rate limit by. Behind a proxy that is the last
// X-Forwarded-For entry, the one the proxy itself appended; earlier entries
// are whatever the client chose to send.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
