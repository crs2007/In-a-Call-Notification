//go:build windows

package windows

import (
	"sync"
	"testing"
)

// TestVisibleWindowsConcurrent exercises VisibleWindows from many goroutines
// at once, mirroring the scenario where two engine generations' poll loops
// overlap during a reload. It exists to be run under `go test -race`: without
// enumMu guarding enumBuffer, this reliably trips the race detector.
func TestVisibleWindowsConcurrent(t *testing.T) {
	const goroutines = 10
	const callsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				_ = VisibleWindows()
			}
		}()
	}
	wg.Wait()
}
