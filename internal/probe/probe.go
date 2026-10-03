// Package probe dumps the raw OS signals the detectors consume: every visible
// window title, and every application holding the microphone or camera.
//
// The output deliberately does no filtering and contains window titles, which
// carry meeting names. It is opt-in diagnostics only and must never be wired
// into the regular log.
package probe

import (
	"fmt"
	"io"
	"sort"
	"time"

	platformwindows "github.com/crs2007/callmqtt/platform/windows"
)

// Banner warns the reader before the output is shared.
const Banner = "# In a Call Notification probe output.\n" +
	"# Contains window titles (meeting names). Review and redact before pasting.\n\n"

// Snapshot writes one timestamped dump of the current signals to w.
func Snapshot(w io.Writer) {
	fmt.Fprintf(w, "=== %s ===\n", time.Now().Format(time.RFC3339))

	names := platformwindows.ProcessNames()
	wins := platformwindows.VisibleWindows()
	sort.Slice(wins, func(i, j int) bool {
		if names[wins[i].PID] != names[wins[j].PID] {
			return names[wins[i].PID] < names[wins[j].PID]
		}
		return wins[i].Title < wins[j].Title
	})

	fmt.Fprintf(w, "[windows] %d visible\n", len(wins))
	for _, win := range wins {
		name := names[win.PID]
		if name == "" {
			name = "?"
		}
		fmt.Fprintf(w, "  pid=%-6d proc=%-28s title=%q\n", win.PID, name, win.Title)
	}

	devices := []struct {
		name string
		apps []string
	}{
		{"microphone", platformwindows.AppsUsingMicrophone(names)},
		{"webcam", platformwindows.AppsUsingWebcam(names)},
	}
	for _, d := range devices {
		fmt.Fprintf(w, "[%s] %d in use\n", d.name, len(d.apps))
		for _, app := range d.apps {
			fmt.Fprintf(w, "  %s\n", app)
		}
	}
	fmt.Fprintln(w)
}

// Run writes Banner and then count snapshots (0 = until ctxDone closes),
// interval apart.
func Run(w io.Writer, count int, interval time.Duration, done <-chan struct{}) {
	io.WriteString(w, Banner)
	for i := 0; count == 0 || i < count; i++ {
		if i > 0 {
			select {
			case <-done:
				return
			case <-time.After(interval):
			}
		}
		Snapshot(w)
	}
}
