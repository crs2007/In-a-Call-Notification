package mqtt

import "time"

// connectErrorLogInterval bounds how often a persisting connect failure is
// re-logged after the first one. autopaho's default ConnectRetryDelay is
// 10s, so without this a broker outage logs ~360 lines/hour; this caps it to
// one line per interval plus the initial failure.
const connectErrorLogInterval = 10 * time.Minute

// connectErrorState is the rate-limiter's state between OnConnectError
// calls: whether a run of failures is currently in progress, how many have
// happened since the last log line, and when that line was last written.
type connectErrorState struct {
	failing bool
	count   int
	lastLog time.Time
}

// recordConnectError is the pure decision behind OnConnectError's rate
// limiting, split out so it can be table-tested without a real broker or a
// live clock. Given the state before this failure and the time it occurred,
// it returns the updated state, whether this failure should be logged now,
// and (when it should) the number of failures to report — mirroring the
// engine's publishFailing latch (internal/engine/engine.go) so a broker
// outage logs its first failure, then one summary line per
// connectErrorLogInterval instead of one line per retry.
func recordConnectError(s connectErrorState, now time.Time) (next connectErrorState, shouldLog bool, count int) {
	s.count++
	if !s.failing {
		s.failing = true
		s.lastLog = now
		return s, true, s.count
	}
	if now.Sub(s.lastLog) >= connectErrorLogInterval {
		count = s.count
		s.lastLog = now
		s.count = 0
		return s, true, count
	}
	return s, false, 0
}

// recordConnectRecovery is recordConnectError's counterpart for a successful
// (re)connect: it reports whether the prior state was mid-failure, so the
// caller only logs a recovery line when there was something to recover
// from, and how many failures had accumulated since the last log line, then
// returns the tracker reset to its zero state.
func recordConnectRecovery(s connectErrorState) (next connectErrorState, wasFailing bool, count int) {
	return connectErrorState{}, s.failing, s.count
}
