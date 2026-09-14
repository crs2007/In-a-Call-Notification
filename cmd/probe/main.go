// Command probe is a throwaway diagnostic that dumps the raw OS signals
// CallMQTT's detectors will eventually consume: every visible window title,
// and every application currently holding the microphone or camera.
//
// It deliberately does no filtering and no interpretation. Run it while
// joining and leaving real calls, capture the output into testdata/probe/,
// and use those captures to author detection rules:
//
//	go run ./cmd/probe > testdata/probe/teams-in-call.txt
//
// See docs/ for the scenario list.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	platformwindows "github.com/crs2007/callmqtt/platform/windows"
)

func snapshot(w *os.File) {
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

	devices := map[string][]string{
		"microphone": platformwindows.AppsUsingMicrophone(),
		"webcam":     platformwindows.AppsUsingWebcam(),
	}
	for _, device := range []string{"microphone", "webcam"} {
		apps := devices[device]
		fmt.Fprintf(w, "[%s] %d in use\n", device, len(apps))
		for _, app := range apps {
			fmt.Fprintf(w, "  %s\n", app)
		}
	}
	fmt.Fprintln(w)
}

func main() {
	interval := flag.Duration("interval", 2*time.Second, "time between snapshots")
	count := flag.Int("count", 0, "number of snapshots to take (0 = run until interrupted)")
	flag.Parse()

	for i := 0; *count == 0 || i < *count; i++ {
		if i > 0 {
			time.Sleep(*interval)
		}
		snapshot(os.Stdout)
	}
}
