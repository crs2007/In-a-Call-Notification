//go:build darwin

package darwin

import (
	"os"
	"sort"
	"sync"
	"testing"
	"time"
)

// TestProcessNamesIncludesSelf is a basic sanity check on the sysctl walk
// and proc_pidpath naming: the test binary's own PID must show up with a
// non-empty executable name.
func TestProcessNamesIncludesSelf(t *testing.T) {
	names := ProcessNames()

	pid := uint32(os.Getpid())
	name, ok := names[pid]
	if !ok {
		t.Fatalf("ProcessNames() missing current PID %d (%d processes listed)", pid, len(names))
	}
	if name == "" {
		t.Fatalf("ProcessNames()[%d] is empty", pid)
	}
	if len(names) < 10 {
		t.Errorf("ProcessNames() listed only %d processes; expected the whole system", len(names))
	}
}

// TestSignalsReturnQuickly calls every adapter once on the real system. On
// a CI runner or a Mac without Screen Recording there may be no windows and
// nothing on the microphone, so only the contract is checked: no panic, a
// well-formed result, and well under one poll interval.
func TestSignalsReturnQuickly(t *testing.T) {
	const budget = time.Second
	procNames := ProcessNames()

	check := func(name string, f func()) {
		t.Helper()
		start := time.Now()
		f()
		if d := time.Since(start); d > budget {
			t.Errorf("%s took %v, want under %v", name, d, budget)
		}
	}

	check("VisibleWindows", func() {
		for _, w := range VisibleWindows() {
			if w.PID == 0 || w.Title == "" {
				t.Errorf("VisibleWindows() returned %+v; want a PID and a title", w)
			}
		}
	})
	check("AppsUsingMicrophone", func() {
		apps := AppsUsingMicrophone(procNames)
		if !sort.StringsAreSorted(apps) {
			t.Errorf("AppsUsingMicrophone() = %v, not sorted", apps)
		}
		for _, a := range apps {
			if a == "" {
				t.Errorf("AppsUsingMicrophone() = %q, contains an empty name", apps)
			}
		}
	})
	check("AppsUsingWebcam", func() {
		if apps := AppsUsingWebcam(procNames); apps != nil && (len(apps) != 1 || apps[0] != unknownApp) {
			t.Errorf("AppsUsingWebcam() = %v, want nil or [%s]", apps, unknownApp)
		}
	})
	check("ScreenRecordingGranted", func() { _ = ScreenRecordingGranted() })
}

// TestAudioProcessListReadable checks the Core Audio process-object path
// itself, not just that AppsUsingMicrophone returned: on macOS 14.2+ the
// list must be readable, and having just made Core Audio calls this test
// process is itself an audio client, so the list cannot be empty.
func TestAudioProcessListReadable(t *testing.T) {
	if err := audioReady(); err != nil {
		t.Skipf("Core Audio unavailable: %v", err)
	}
	if !audioHasProperty(systemObject, selProcessObjectList) {
		t.Skip("kAudioHardwarePropertyProcessObjectList needs macOS 14.2+")
	}
	objs, err := audioObjectList(systemObject, selProcessObjectList)
	if err != nil {
		t.Fatalf("audioObjectList(prs#): %v", err)
	}
	if len(objs) == 0 {
		t.Fatal("Core Audio process list is empty; expected at least this test process")
	}
	var sawPID bool
	for _, obj := range objs {
		if pid, err := audioUint32(obj, selProcessPID); err == nil && pid != 0 {
			sawPID = true
			break
		}
	}
	if !sawPID {
		t.Error("no Core Audio process object reported a PID")
	}
}

// TestCameraDeviceListReadable checks the seven-argument CoreMediaIO call
// shape: getting it wrong crashes the test binary rather than failing.
func TestCameraDeviceListReadable(t *testing.T) {
	if err := cmioObjectGetPropertyData.Load(); err != nil {
		t.Skipf("CoreMediaIO unavailable: %v", err)
	}
	devices, err := cmioObjectList(systemObject, selCMIODevices)
	if err != nil {
		t.Fatalf("cmioObjectList(dev#): %v", err)
	}
	for _, dev := range devices {
		if _, err := cmioUint32(dev, selDeviceIsRunningSomewhere); err != nil {
			t.Errorf("device %d: read running state: %v", dev, err)
		}
	}
}

// TestSignalsConcurrent exercises every adapter from many goroutines at
// once, mirroring the scenario where two engine generations' poll loops
// overlap during a reload (see platform/windows' enumMu). It exists to be
// run under `go test -race`: the adapters keep all per-call state local and
// initialise shared state under sync.Once, and this makes sure it stays
// that way.
func TestSignalsConcurrent(t *testing.T) {
	const goroutines = 8
	const callsPerGoroutine = 10

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				procNames := ProcessNames()
				_ = VisibleWindows()
				_ = AppsUsingMicrophone(procNames)
				_ = AppsUsingWebcam(procNames)
				_ = ScreenRecordingGranted()
			}
		}()
	}
	wg.Wait()
}

// BenchmarkPoll measures one full poll's worth of signal reads, the cost
// the agent pays every poll interval (2s by default). Keep it in the low
// milliseconds: the CPU budget is under 0.5% at a 2s poll.
func BenchmarkPoll(b *testing.B) {
	for b.Loop() {
		procNames := ProcessNames()
		_ = VisibleWindows()
		_ = AppsUsingMicrophone(procNames)
		_ = AppsUsingWebcam(procNames)
	}
}

func BenchmarkProcessNames(b *testing.B) {
	for b.Loop() {
		_ = ProcessNames()
	}
}

func BenchmarkVisibleWindows(b *testing.B) {
	for b.Loop() {
		_ = VisibleWindows()
	}
}

func BenchmarkAppsUsingMicrophone(b *testing.B) {
	procNames := ProcessNames()
	for b.Loop() {
		_ = AppsUsingMicrophone(procNames)
	}
}

func BenchmarkAppsUsingWebcam(b *testing.B) {
	for b.Loop() {
		_ = AppsUsingWebcam(nil)
	}
}
