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
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/shirou/gopsutil/v4/process"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW     = user32.NewProc("GetWindowTextLengthW")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
)

type windowInfo struct {
	PID   uint32
	Title string
}

// enumBuffer accumulates results for the in-flight EnumWindows call.
//
// syscall.NewCallback allocates a callback slot that is never released, and
// the process is capped at a few thousand of them. A long-running poll loop
// must therefore create the callback exactly once, at package level, and
// share state through a package variable rather than a closure per call.
// EnumWindows is synchronous, so a single unsynchronised buffer is safe here.
var enumBuffer []windowInfo

var enumCallback = syscall.NewCallback(func(hwnd syscall.Handle, _ uintptr) uintptr {
	const continueEnumeration = 1

	if visible, _, _ := procIsWindowVisible.Call(uintptr(hwnd)); visible == 0 {
		return continueEnumeration
	}
	length, _, _ := procGetWindowTextLengthW.Call(uintptr(hwnd))
	if length == 0 {
		return continueEnumeration // untitled windows carry no signal
	}

	buf := make([]uint16, length+1)
	procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), length+1)

	var pid uint32
	procGetWindowThreadProcessId.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pid)))

	enumBuffer = append(enumBuffer, windowInfo{PID: pid, Title: windows.UTF16ToString(buf)})
	return continueEnumeration
})

func visibleWindows() []windowInfo {
	enumBuffer = enumBuffer[:0]
	procEnumWindows.Call(enumCallback, 0)
	out := make([]windowInfo, len(enumBuffer))
	copy(out, enumBuffer)
	return out
}

// consentStorePath is where Windows records which applications have used the
// microphone and camera, and — crucially — which are using them right now.
const consentStorePath = `SOFTWARE\Microsoft\Windows\CurrentVersion\CapabilityAccessManager\ConsentStore\`

// devicesInUse reports the applications currently holding the named capability
// device ("microphone" or "webcam").
//
// Each application has a subkey holding LastUsedTimeStart and LastUsedTimeStop
// as FILETIMEs. A LastUsedTimeStop of zero means the application has started
// using the device and has not stopped: it is live right now. Packaged (MSIX)
// applications such as New Teams sit directly under the device key, keyed by
// package family name; everything else sits under NonPackaged, keyed by its
// executable path with backslashes replaced by "#".
//
// A missing key means "no evidence", never an error — the agent must keep
// running with degraded detection rather than fail.
func devicesInUse(device string) []string {
	var inUse []string

	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		key, err := registry.OpenKey(root, consentStorePath+device, registry.READ)
		if err != nil {
			continue
		}

		names, err := key.ReadSubKeyNames(-1)
		if err != nil {
			key.Close()
			continue
		}

		for _, name := range names {
			if strings.EqualFold(name, "NonPackaged") {
				nonPackaged, err := registry.OpenKey(key, name, registry.READ)
				if err != nil {
					continue
				}
				appNames, err := nonPackaged.ReadSubKeyNames(-1)
				if err == nil {
					for _, app := range appNames {
						if isLive(nonPackaged, app) {
							inUse = append(inUse, strings.ReplaceAll(app, "#", `\`))
						}
					}
				}
				nonPackaged.Close()
				continue
			}
			if isLive(key, name) {
				inUse = append(inUse, name+"  (packaged)")
			}
		}
		key.Close()
	}

	sort.Strings(inUse)
	return inUse
}

func isLive(parent registry.Key, subkey string) bool {
	key, err := registry.OpenKey(parent, subkey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()

	stop, _, err := key.GetIntegerValue("LastUsedTimeStop")
	return err == nil && stop == 0
}

func processNames() map[uint32]string {
	names := make(map[uint32]string)
	procs, err := process.Processes()
	if err != nil {
		return names
	}
	for _, p := range procs {
		if name, err := p.Name(); err == nil {
			names[uint32(p.Pid)] = name
		}
	}
	return names
}

func snapshot(w *os.File) {
	fmt.Fprintf(w, "=== %s ===\n", time.Now().Format(time.RFC3339))

	names := processNames()
	wins := visibleWindows()
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

	for _, device := range []string{"microphone", "webcam"} {
		apps := devicesInUse(device)
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
