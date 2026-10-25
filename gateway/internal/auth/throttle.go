package auth

import (
	"sync"
	"time"
)

// Throttle limits failed sign-in attempts per key (an email address or an IP address):
// after Max failures within Window, the key is refused until the window has passed.
// It lives in memory, which is enough for a single gateway instance.
type Throttle struct {
	Max    int
	Window time.Duration

	mu   sync.Mutex
	keys map[string]*attempts
}

type attempts struct {
	first time.Time
	count int
}

func NewThrottle(max int, window time.Duration) *Throttle {
	return &Throttle{Max: max, Window: window, keys: map[string]*attempts{}}
}

// Allowed reports whether key may try again now.
func (t *Throttle) Allowed(key string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	a := t.keys[key]
	return a == nil || a.count < t.Max || now.Sub(a.first) >= t.Window
}

// Fail records a failed attempt.
func (t *Throttle) Fail(key string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.keys) > 50_000 {
		for k, a := range t.keys {
			if now.Sub(a.first) >= t.Window {
				delete(t.keys, k)
			}
		}
	}
	a := t.keys[key]
	if a == nil || now.Sub(a.first) >= t.Window {
		a = &attempts{first: now}
		t.keys[key] = a
	}
	a.count++
}

// Reset forgets key, after a successful sign-in.
func (t *Throttle) Reset(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.keys, key)
}
