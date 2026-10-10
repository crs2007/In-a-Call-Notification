//go:build darwin

package darwin

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/crs2007/callmqtt/platform/darwin/internal/cf"
)

// The frameworks the signal adapters call into. Each is opened lazily on
// first use (see cf.Library), so a framework that is missing or broken only
// disables the one signal that needs it.
var (
	coreGraphicsLib = cf.NewLibrary("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics")
	coreAudioLib    = cf.NewLibrary("/System/Library/Frameworks/CoreAudio.framework/CoreAudio")
	coreMediaIOLib  = cf.NewLibrary("/System/Library/Frameworks/CoreMediaIO.framework/CoreMediaIO")
	libSystem       = cf.NewLibrary("/usr/lib/libSystem.B.dylib")
)

// signalUnavailableLogged remembers which signals have already reported
// that they cannot work on this machine.
var signalUnavailableLogged sync.Map // signal name -> struct{}

// signalUnavailable logs, once per signal per process, that a framework or
// symbol needed for signal could not be loaded. A missing framework does
// not change between polls, so repeating the line every 2 seconds would only
// bury the log; the caller returns "no evidence" either way.
//
// It logs at Info rather than Debug because, unlike a single failed poll,
// this silently disables a whole class of detection — something a user
// reading the log while wondering why calls go unnoticed needs to see.
func signalUnavailable(signal string, err error) {
	if _, seen := signalUnavailableLogged.LoadOrStore(signal, struct{}{}); seen {
		return
	}
	slog.Info("macOS signal unavailable; reporting no evidence", "signal", signal, "err", err)
}

// errNilSymbol reports an exported framework constant that resolved but
// holds NULL — not expected on any shipping macOS, but reading through it
// would crash, so it is treated like a missing symbol.
func errNilSymbol(symbol string) error {
	return fmt.Errorf("%s is NULL", symbol)
}
