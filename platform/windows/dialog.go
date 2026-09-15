//go:build windows

// Package windows holds every Win32 syscall CallMQTT makes. Nothing outside
// this package talks to the OS directly, and nothing in internal/ imports
// it: platform code returns raw data (or, here, raw user input) and leaves
// every decision to the caller.
package windows

import (
	"fmt"
	"runtime"
	"strconv"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// BrokerFields is the dialog's four typed inputs — the only settings in the
// product that genuinely need typing rather than a toggle or a pick.
type BrokerFields struct {
	Host     string
	Port     int
	Username string
	Password string
}

// ShowBrokerDialog opens a modal window with four fields, pre-filled from
// current, and blocks until the user confirms or cancels. ok is false on
// Cancel, close, or the port field failing to parse; fields is only
// meaningful when ok is true.
//
// No CGO, no GUI toolkit: CreateWindowEx with EDIT/STATIC/BUTTON controls and
// a GetMessage loop, the same shape as the tray icon's own window. Win32
// windows are affine to the OS thread that creates them, so this locks that
// thread for the call's duration.
func ShowBrokerDialog(current BrokerFields) (BrokerFields, bool, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	d := &dialog{fields: current}
	if err := d.show(); err != nil {
		return BrokerFields{}, false, err
	}
	return d.result, d.confirmed, nil
}

// ShowError pops a blocking MessageBox with an OK button and an error icon.
// It exists because the shipped binary is linked with -H=windowsgui: it has
// no console, so anything written to stderr — a startup error, most
// visibly "no config, run `callmqtt init`" on first launch — is otherwise
// invisible and the process just exits with nothing on screen.
func ShowError(title, message string) {
	msg, err := windows.UTF16PtrFromString(message)
	if err != nil {
		return
	}
	titleP, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(msg)), uintptr(unsafe.Pointer(titleP)), mbOK|mbIconError)
}

// --- Win32 plumbing ----------------------------------------------------

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procShowWindow          = user32.NewProc("ShowWindow")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procIsDialogMessageW    = user32.NewProc("IsDialogMessageW")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procSetWindowTextW      = user32.NewProc("SetWindowTextW")
	procGetWindowTextW      = user32.NewProc("GetWindowTextW")
	procGetWindowTextLenW   = user32.NewProc("GetWindowTextLengthW")
	procGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	procMessageBoxW         = user32.NewProc("MessageBoxW")
	procSetFocus            = user32.NewProc("SetFocus")
	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
)

const (
	wsOverlapped   = 0x00000000
	wsCaption      = 0x00C00000
	wsSysMenu      = 0x00080000
	wsVisible      = 0x10000000
	wsChild        = 0x40000000
	wsTabStop      = 0x00010000
	wsBorder       = 0x00800000
	wsExClientEdge = 0x00000200

	esAutoHScroll   = 0x0080
	esPassword      = 0x0020
	bsPushButton    = 0x00000000
	bsDefPushButton = 0x00000001

	wmDestroy = 0x0002
	wmClose   = 0x0010
	wmCommand = 0x0111

	smCXScreen = 0
	smCYScreen = 1

	mbOK          = 0x00000000
	mbIconWarning = 0x00000030
	mbIconError   = 0x00000010

	idOK     = 1 // IDOK, so Enter (via IsDialogMessage) triggers this button
	idCancel = 2 // IDCANCEL, so Escape triggers this button
	idHost   = 101
	idPort   = 102
	idUser   = 103
	idPass   = 104

	dialogWidth  = 340
	dialogHeight = 240
)

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type point struct{ x, y int32 }

type msgT struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

// dialog owns one modal window's state: the hwnd it's tied to, the child
// controls it needs to read back, and the outcome the caller gets once the
// message loop returns.
type dialog struct {
	fields    BrokerFields
	result    BrokerFields
	confirmed bool

	hwnd               uintptr
	hostEdit, portEdit uintptr
	userEdit, passEdit uintptr
}

// dialogRegistry maps an hwnd to the dialog instance driving it. A global map
// rather than GWLP_USERDATA keeps this file's unsafe-pointer surface to the
// window class registration only; ShowBrokerDialog is not expected to be
// called concurrently, but nothing here would break if it were.
var dialogRegistry = map[uintptr]*dialog{}

func (d *dialog) show() error {
	className, err := windows.UTF16PtrFromString("CallMQTTBrokerDialog")
	if err != nil {
		return fmt.Errorf("encode window class name: %w", err)
	}
	hinstance, _, _ := procGetModuleHandleW.Call(0)

	wc := wndClassExW{
		cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		lpfnWndProc:   syscall.NewCallback(dialogWndProc),
		hInstance:     hinstance,
		lpszClassName: className,
	}
	if ret, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); ret == 0 {
		// ERROR_CLASS_ALREADY_EXISTS is expected on a second dialog in the
		// same process and is not a failure.
		if err := windows.GetLastError(); err != nil && err != windows.ERROR_CLASS_ALREADY_EXISTS {
			return fmt.Errorf("register window class: %w", err)
		}
	}

	title, err := windows.UTF16PtrFromString("In a Call Notification — MQTT Broker Settings")
	if err != nil {
		return fmt.Errorf("encode window title: %w", err)
	}

	x, y := centered(dialogWidth, dialogHeight)
	style := uintptr(wsOverlapped | wsCaption | wsSysMenu)

	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		style,
		uintptr(x), uintptr(y), uintptr(dialogWidth), uintptr(dialogHeight),
		0, 0, hinstance, 0,
	)
	if hwnd == 0 {
		return fmt.Errorf("create dialog window: %w", windows.GetLastError())
	}
	d.hwnd = hwnd
	dialogRegistry[hwnd] = d
	defer delete(dialogRegistry, hwnd)

	d.buildControls(hinstance)
	d.setFields(d.fields)

	procShowWindow.Call(hwnd, 5) // SW_SHOW
	procSetForegroundWindow.Call(hwnd)
	procSetFocus.Call(d.hostEdit)

	var m msgT
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			return nil
		}
		if handled, _, _ := procIsDialogMessageW.Call(hwnd, uintptr(unsafe.Pointer(&m))); handled == 0 {
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
	}
}

// buildControls lays out four label+edit rows and OK/Cancel buttons. Layout
// is fixed and hand-tuned rather than computed: this is the one dialog in
// the product, not a form builder.
func (d *dialog) buildControls(hinstance uintptr) {
	label := func(text string, y int32) {
		p, _ := windows.UTF16PtrFromString(text)
		className, _ := windows.UTF16PtrFromString("STATIC")
		procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(p)),
			uintptr(wsChild|wsVisible),
			20, uintptr(y), 90, 20,
			d.hwnd, 0, hinstance, 0)
	}
	edit := func(id int, y int32, extraStyle uintptr) uintptr {
		className, _ := windows.UTF16PtrFromString("EDIT")
		hwnd, _, _ := procCreateWindowExW.Call(wsExClientEdge,
			uintptr(unsafe.Pointer(className)), 0,
			uintptr(wsChild|wsVisible|wsTabStop|wsBorder)|extraStyle,
			120, uintptr(y), 190, 22,
			d.hwnd, uintptr(id), hinstance, 0)
		return hwnd
	}
	button := func(text string, id int, x int32, style uintptr) {
		p, _ := windows.UTF16PtrFromString(text)
		className, _ := windows.UTF16PtrFromString("BUTTON")
		procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(p)),
			uintptr(wsChild|wsVisible|wsTabStop)|style,
			uintptr(x), 170, 90, 26,
			d.hwnd, uintptr(id), hinstance, 0)
	}

	label("Broker host", 20)
	d.hostEdit = edit(idHost, 18, esAutoHScroll)
	label("Port", 55)
	d.portEdit = edit(idPort, 53, esAutoHScroll)
	label("Username", 90)
	d.userEdit = edit(idUser, 88, esAutoHScroll)
	label("Password", 125)
	d.passEdit = edit(idPass, 123, esAutoHScroll|esPassword)

	button("OK", idOK, 130, bsDefPushButton)
	button("Cancel", idCancel, 230, bsPushButton)
}

func (d *dialog) setFields(f BrokerFields) {
	setText(d.hostEdit, f.Host)
	setText(d.portEdit, strconv.Itoa(f.Port))
	setText(d.userEdit, f.Username)
	setText(d.passEdit, f.Password)
}

// confirm reads the controls back, validates the port, and — if valid — ends
// the dialog with confirmed=true. An invalid port keeps the dialog open
// rather than handing the caller a broken config to save.
func (d *dialog) confirm() {
	portText := getText(d.portEdit)
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		msg, _ := windows.UTF16PtrFromString("Port must be a number between 1 and 65535.")
		title, _ := windows.UTF16PtrFromString("In a Call Notification")
		procMessageBoxW.Call(d.hwnd, uintptr(unsafe.Pointer(msg)), uintptr(unsafe.Pointer(title)), mbOK|mbIconWarning)
		return
	}

	d.result = BrokerFields{
		Host:     getText(d.hostEdit),
		Port:     port,
		Username: getText(d.userEdit),
		Password: getText(d.passEdit),
	}
	d.confirmed = true
	procDestroyWindow.Call(d.hwnd)
}

func (d *dialog) cancel() {
	d.confirmed = false
	procDestroyWindow.Call(d.hwnd)
}

func dialogWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmCommand:
		id := int(wParam & 0xffff)
		if d, ok := dialogRegistry[hwnd]; ok {
			switch id {
			case idOK:
				d.confirm()
				return 0
			case idCancel:
				d.cancel()
				return 0
			}
		}
	case wmClose:
		if d, ok := dialogRegistry[hwnd]; ok {
			d.cancel()
		}
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

func centered(w, h int32) (int32, int32) {
	sw, _, _ := procGetSystemMetrics.Call(smCXScreen)
	sh, _, _ := procGetSystemMetrics.Call(smCYScreen)
	return (int32(sw) - w) / 2, (int32(sh) - h) / 2
}

func setText(hwnd uintptr, s string) {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		return
	}
	procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(p)))
}

func getText(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLenW.Call(hwnd)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), n+1)
	return windows.UTF16ToString(buf)
}
