package mqtt

import (
	"testing"
	"time"
)

func TestFailureThrottle(t *testing.T) {
	var th failureThrottle
	base := time.Unix(1000, 0)

	if n, ok := th.failed(base); !ok || n != 1 {
		t.Fatalf("first failure = (%d, %v), want (1, true)", n, ok)
	}
	if n, ok := th.failed(base.Add(10 * time.Second)); ok || n != 2 {
		t.Fatalf("repeat within window = (%d, %v), want (2, false)", n, ok)
	}
	if n, ok := th.failed(base.Add(connectLogInterval)); !ok || n != 3 {
		t.Fatalf("repeat after window = (%d, %v), want (3, true)", n, ok)
	}
	if n := th.recovered(); n != 3 {
		t.Fatalf("recovered() = %d, want 3", n)
	}
	if n := th.recovered(); n != 0 {
		t.Fatalf("second recovered() = %d, want 0", n)
	}
	if n, ok := th.failed(base.Add(time.Hour)); !ok || n != 1 {
		t.Fatalf("failure after recovery = (%d, %v), want (1, true)", n, ok)
	}
}
