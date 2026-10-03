package mqtt

import (
	"sync"
	"time"
)

// connectLogInterval is how often a still-failing connection is logged after
// the first failure.
const connectLogInterval = 5 * time.Minute

// failureThrottle lets the first failure through, then at most one per
// connectLogInterval, counting every attempt. It mirrors the engine's
// publishFailing latch for the reconnect loop.
type failureThrottle struct {
	mu      sync.Mutex
	count   int
	lastLog time.Time
}

// failed records an attempt and reports the running count and whether this
// one should be logged.
func (t *failureThrottle) failed(now time.Time) (int, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.count++
	if t.count == 1 || now.Sub(t.lastLog) >= connectLogInterval {
		t.lastLog = now
		return t.count, true
	}
	return t.count, false
}

// recovered returns the number of failed attempts since the last success and
// resets the throttle.
func (t *failureThrottle) recovered() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := t.count
	t.count = 0
	return n
}
