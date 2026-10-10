//go:build darwin

package cf

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/go-webgpu/goffi/ffi"
	"github.com/go-webgpu/goffi/types"
)

// Type is a C argument or return type, as goffi describes it.
type Type = *types.TypeDescriptor

// The C types the darwin adapters need. Every argument is handed to goffi as
// a full machine word (see Func.Call); goffi reads only the declared width,
// which on little-endian arm64 and amd64 is the low bytes of the word.
var (
	Void    Type = types.VoidTypeDescriptor
	Pointer Type = types.PointerTypeDescriptor
	U8      Type = types.UInt8TypeDescriptor  // Boolean
	U32     Type = types.UInt32TypeDescriptor // UInt32, AudioObjectID, CFStringEncoding
	S32     Type = types.SInt32TypeDescriptor // OSStatus, pid_t, int
	S64     Type = types.SInt64TypeDescriptor // CFIndex
	U64     Type = types.UInt64TypeDescriptor // CFTypeID (unsigned long)
)

// maxArgs bounds Func.Call's argument count. goffi itself accepts up to 15;
// nothing here needs more than CMIOObjectGetPropertyData's 7, and a fixed
// bound keeps the per-call slot array off the heap.
const maxArgs = 8

// Library is a dynamic library (usually a framework binary) opened with
// dlopen on first use and kept open for the life of the process.
//
// Loading is lazy so that merely importing platform/darwin — e.g. from a
// test, or a build that never polls — costs nothing, and so a framework that
// is missing on some macOS version only disables the signal that needs it.
type Library struct {
	path string

	once   sync.Once
	handle unsafe.Pointer
	err    error
}

// NewLibrary declares a library by absolute path. Nothing is loaded until a
// Func from it is first called (or Global is read).
func NewLibrary(path string) *Library { return &Library{path: path} }

// load opens the library exactly once; every later call returns the cached
// handle or error. sync.Once makes this safe from concurrent poll loops.
func (l *Library) load() (unsafe.Pointer, error) {
	l.once.Do(func() {
		l.handle, l.err = ffi.LoadLibrary(l.path)
	})
	return l.handle, l.err
}

// Global reads a pointer-sized exported global, such as the CFStringRef
// constant kCGWindowOwnerPID. dlsym returns the variable's address; the
// value the caller wants is what is stored there. The value is not retained
// and must not be released.
func (l *Library) Global(name string) (Ref, error) {
	h, err := l.load()
	if err != nil {
		return 0, err
	}
	addr, err := ffi.GetSymbol(h, name)
	if err != nil {
		return 0, err
	}
	return Ref(*(*uintptr)(addr)), nil
}

// Func is one C function: resolved by name and given a prepared goffi call
// interface on first use, then callable any number of times from any
// goroutine. A Func's resolved state is immutable after preparation, so
// concurrent calls share it without locking.
type Func struct {
	lib  *Library
	name string
	ret  Type
	args []Type

	once sync.Once
	sym  unsafe.Pointer
	cif  types.CallInterface
	err  error
}

// Func declares a C function in l with the given return and argument types.
// Declaring is free; resolution happens on the first Call or Load.
func (l *Library) Func(name string, ret Type, args ...Type) *Func {
	return &Func{lib: l, name: name, ret: ret, args: args}
}

// Load resolves the symbol and prepares the call interface, reporting why
// the function is unusable if it is. Calling it up front lets an adapter
// decide once, cheaply, whether its signal is available at all.
func (f *Func) Load() error {
	f.once.Do(func() {
		if len(f.args) > maxArgs {
			f.err = fmt.Errorf("cf: %s: %d arguments exceeds %d", f.name, len(f.args), maxArgs)
			return
		}
		h, err := f.lib.load()
		if err != nil {
			f.err = err
			return
		}
		sym, err := ffi.GetSymbol(h, f.name)
		if err != nil {
			f.err = err
			return
		}
		if err := ffi.PrepareCallInterface(&f.cif, types.DefaultCall, f.ret, f.args); err != nil {
			f.err = fmt.Errorf("cf: prepare %s: %w", f.name, err)
			return
		}
		f.sym = sym
	})
	return f.err
}

// Name is the C symbol name, for log messages.
func (f *Func) Name() string { return f.name }

// Call invokes the function. Each argument is one machine word: integers
// are passed as-is, and a pointer into Go memory must be written inline at
// the call site as uintptr(unsafe.Pointer(&x)).
//
// The //go:uintptrescapes directive is what makes that inline conversion
// safe, exactly as it does for syscall.Proc.Call on Windows: the compiler
// moves the pointed-to value to the heap (where it cannot be relocated by a
// stack copy mid-call) and keeps it alive until Call returns. Converting to
// uintptr in a separate statement first would lose both guarantees.
//
// The result is the raw return register, zero-extended from the declared
// return width: compare Booleans with != 0, reinterpret OSStatus with
// int32(). A non-nil error means the function could not be called at all
// (library or symbol missing); callers treat that as "no evidence".
//
//go:uintptrescapes
func (f *Func) Call(args ...uintptr) (uintptr, error) {
	if err := f.Load(); err != nil {
		return 0, err
	}
	if len(args) != len(f.args) {
		return 0, fmt.Errorf("cf: %s: got %d arguments, want %d", f.name, len(args), len(f.args))
	}
	var slots [maxArgs]unsafe.Pointer
	avalue := slots[:len(args)]
	for i := range args {
		avalue[i] = unsafe.Pointer(&args[i])
	}
	var ret uintptr
	if _, err := ffi.CallFunction(&f.cif, f.sym, unsafe.Pointer(&ret), avalue); err != nil {
		return 0, fmt.Errorf("cf: call %s: %w", f.name, err)
	}
	return ret, nil
}
