package mqtt

import (
	"testing"
	"time"
)

func TestRecordConnectError(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("first failure always logs", func(t *testing.T) {
		next, shouldLog, count := recordConnectError(connectErrorState{}, t0)
		if !shouldLog || count != 1 {
			t.Fatalf("recordConnectError() = shouldLog=%v count=%d, want true, 1", shouldLog, count)
		}
		if !next.failing || next.count != 1 || !next.lastLog.Equal(t0) {
			t.Fatalf("unexpected next state: %+v", next)
		}
	})

	t.Run("failures inside the interval are suppressed", func(t *testing.T) {
		s, _, _ := recordConnectError(connectErrorState{}, t0)
		s, shouldLog, _ := recordConnectError(s, t0.Add(time.Minute))
		if shouldLog {
			t.Fatalf("recordConnectError() logged a failure inside connectErrorLogInterval")
		}
		if s.count != 2 {
			t.Fatalf("count = %d, want 2 (failures still accumulate while suppressed)", s.count)
		}
	})

	t.Run("a failure past the interval logs again with the accumulated count", func(t *testing.T) {
		s, _, _ := recordConnectError(connectErrorState{}, t0)
		s, _, _ = recordConnectError(s, t0.Add(time.Minute))
		s, shouldLog, count := recordConnectError(s, t0.Add(connectErrorLogInterval+time.Second))
		if !shouldLog {
			t.Fatalf("recordConnectError() did not log after connectErrorLogInterval elapsed")
		}
		if count != 3 {
			t.Fatalf("count = %d, want 3 (all failures since the last log)", count)
		}
		if s.count != 0 {
			t.Fatalf("state.count = %d, want reset to 0 after logging", s.count)
		}
	})
}

func TestRecordConnectRecovery(t *testing.T) {
	t.Run("recovering from no failure logs nothing", func(t *testing.T) {
		next, wasFailing, _ := recordConnectRecovery(connectErrorState{})
		if wasFailing {
			t.Fatalf("recordConnectRecovery() reported wasFailing on a clean state")
		}
		if next != (connectErrorState{}) {
			t.Fatalf("next = %+v, want zero value", next)
		}
	})

	t.Run("recovering from a failing state reports it and resets", func(t *testing.T) {
		s, _, _ := recordConnectError(connectErrorState{}, time.Now())
		next, wasFailing, count := recordConnectRecovery(s)
		if !wasFailing || count != 1 {
			t.Fatalf("recordConnectRecovery() = wasFailing=%v count=%d, want true, 1", wasFailing, count)
		}
		if next != (connectErrorState{}) {
			t.Fatalf("next = %+v, want zero value", next)
		}
	})
}
