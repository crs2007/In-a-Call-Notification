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
	"os"
	"time"

	"github.com/crs2007/callmqtt/internal/probe"
)

func main() {
	interval := flag.Duration("interval", 2*time.Second, "time between snapshots")
	count := flag.Int("count", 0, "number of snapshots to take (0 = run until interrupted)")
	flag.Parse()

	probe.Run(os.Stdout, *count, *interval, nil)
}
