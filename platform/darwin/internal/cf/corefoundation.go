//go:build darwin

package cf

import (
	"sync"
	"unsafe"
)

// Ref is a CoreFoundation object reference (CFTypeRef, CFStringRef, ...),
// held as a plain word. Zero is NULL.
//
// Ownership follows CoreFoundation's Create Rule: a Ref returned by a
// function with Copy or Create in its name — or documented as retained, like
// Core Audio's kAudioProcessPropertyBundleID — belongs to the caller, who
// must Release it exactly once. A Ref obtained from a Get function (array
// elements, dictionary values, exported constants) is borrowed: valid only
// while its container lives, and never released.
type Ref uintptr

// CoreFoundation is the framework every other darwin adapter builds on.
var CoreFoundation = NewLibrary("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")

var (
	cfRelease               = CoreFoundation.Func("CFRelease", Void, Pointer)
	cfGetTypeID             = CoreFoundation.Func("CFGetTypeID", U64, Pointer)
	cfStringGetTypeID       = CoreFoundation.Func("CFStringGetTypeID", U64)
	cfNumberGetTypeID       = CoreFoundation.Func("CFNumberGetTypeID", U64)
	cfBooleanGetTypeID      = CoreFoundation.Func("CFBooleanGetTypeID", U64)
	cfArrayGetTypeID        = CoreFoundation.Func("CFArrayGetTypeID", U64)
	cfDictionaryGetTypeID   = CoreFoundation.Func("CFDictionaryGetTypeID", U64)
	cfArrayGetCount         = CoreFoundation.Func("CFArrayGetCount", S64, Pointer)
	cfArrayGetValueAtIndex  = CoreFoundation.Func("CFArrayGetValueAtIndex", Pointer, Pointer, S64)
	cfDictionaryGetValue    = CoreFoundation.Func("CFDictionaryGetValue", Pointer, Pointer, Pointer)
	cfNumberGetValue        = CoreFoundation.Func("CFNumberGetValue", U8, Pointer, S64, Pointer)
	cfBooleanGetValue       = CoreFoundation.Func("CFBooleanGetValue", U8, Pointer)
	cfStringGetLength       = CoreFoundation.Func("CFStringGetLength", S64, Pointer)
	cfStringGetMaximumSize  = CoreFoundation.Func("CFStringGetMaximumSizeForEncoding", S64, S64, U32)
	cfStringGetCString      = CoreFoundation.Func("CFStringGetCString", U8, Pointer, Pointer, S64, U32)
	allCoreFoundationFuncs  = []*Func{cfRelease, cfGetTypeID, cfStringGetTypeID, cfNumberGetTypeID, cfBooleanGetTypeID, cfArrayGetTypeID, cfDictionaryGetTypeID, cfArrayGetCount, cfArrayGetValueAtIndex, cfDictionaryGetValue, cfNumberGetValue, cfBooleanGetValue, cfStringGetLength, cfStringGetMaximumSize, cfStringGetCString}
	coreFoundationReadyOnce sync.Once
	coreFoundationReadyErr  error
)

const (
	encodingUTF8    = 0x08000100 // kCFStringEncodingUTF8
	numberSInt32    = 3          // kCFNumberSInt32Type
	maxStringLength = 1 << 20    // refuse absurd lengths rather than allocate them
)

// typeKind indexes typeIDs.
type typeKind int

const (
	kindString typeKind = iota
	kindNumber
	kindBoolean
	kindArray
	kindDictionary
	numKinds
)

// typeIDs caches CoreFoundation's runtime type identifiers, which are fixed
// for the life of the process. Written once inside Ready's sync.Once and
// only read after Ready returns, so concurrent readers need no lock.
var typeIDs [numKinds]uintptr

// Ready reports whether every CoreFoundation function this package wraps
// resolved. Adapters call it once before doing any work so a broken
// CoreFoundation turns into one log line and "no evidence", rather than a
// stream of per-call failures.
func Ready() error {
	coreFoundationReadyOnce.Do(func() {
		for _, f := range allCoreFoundationFuncs {
			if err := f.Load(); err != nil {
				coreFoundationReadyErr = err
				return
			}
		}
		for kind, f := range map[typeKind]*Func{
			kindString:     cfStringGetTypeID,
			kindNumber:     cfNumberGetTypeID,
			kindBoolean:    cfBooleanGetTypeID,
			kindArray:      cfArrayGetTypeID,
			kindDictionary: cfDictionaryGetTypeID,
		} {
			id, err := f.Call()
			if err != nil {
				coreFoundationReadyErr = err
				return
			}
			typeIDs[kind] = id
		}
	})
	return coreFoundationReadyErr
}

// isType reports whether r is a non-NULL object of the given CF type. Checking before every typed accessor costs one call and
// means an unexpected value type — Apple changing a dictionary entry from a
// number to a string, say — reads as "absent" instead of crashing inside
// CoreFoundation.
func isType(r Ref, kind typeKind) bool {
	if r == 0 || Ready() != nil {
		return false
	}
	id, err := cfGetTypeID.Call(uintptr(r))
	return err == nil && id != 0 && id == typeIDs[kind]
}

// Release drops one reference to r. NULL is ignored (CFRelease itself would
// crash on it).
func Release(r Ref) {
	if r == 0 {
		return
	}
	_, _ = cfRelease.Call(uintptr(r))
}

// String converts a CFStringRef to a Go string. NULL, a non-string object,
// or a string that cannot be encoded as UTF-8 all read as "".
func String(r Ref) string {
	if !isType(r, kindString) {
		return ""
	}
	length, err := cfStringGetLength.Call(uintptr(r))
	if err != nil || int64(length) <= 0 {
		return ""
	}
	if int64(length) > maxStringLength {
		return ""
	}
	size, err := cfStringGetMaximumSize.Call(length, encodingUTF8)
	if err != nil || int64(size) <= 0 {
		return ""
	}
	buf := make([]byte, int64(size)+1) // +1 for the NUL terminator
	ok, err := cfStringGetCString.Call(uintptr(r), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), encodingUTF8)
	if err != nil || ok&0xff == 0 {
		return ""
	}
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}

// Int32 reads a CFNumberRef as a signed 32-bit integer. ok is false for
// NULL, a non-number, or a value CoreFoundation reports as lossy.
func Int32(r Ref) (v int32, ok bool) {
	if !isType(r, kindNumber) {
		return 0, false
	}
	res, err := cfNumberGetValue.Call(uintptr(r), numberSInt32, uintptr(unsafe.Pointer(&v)))
	if err != nil || res&0xff == 0 {
		return 0, false
	}
	return v, true
}

// Bool reads a CFBooleanRef. A CFNumber is also accepted (non-zero is
// true), since several CoreGraphics dictionary entries documented as
// booleans are in practice stored as numbers.
func Bool(r Ref) (v, ok bool) {
	if isType(r, kindBoolean) {
		res, err := cfBooleanGetValue.Call(uintptr(r))
		return err == nil && res&0xff != 0, err == nil
	}
	if n, ok := Int32(r); ok {
		return n != 0, true
	}
	return false, false
}

// ArrayLen is CFArrayGetCount, or 0 for NULL or a non-array.
func ArrayLen(r Ref) int {
	if !isType(r, kindArray) {
		return 0
	}
	n, err := cfArrayGetCount.Call(uintptr(r))
	if err != nil || int64(n) < 0 {
		return 0
	}
	return int(int64(n))
}

// ArrayAt returns the borrowed element at index i. The caller must bound i
// by ArrayLen; an out-of-range index is reported as NULL rather than handed
// to CoreFoundation, which would abort the process.
func ArrayAt(r Ref, i int) Ref {
	if i < 0 || i >= ArrayLen(r) {
		return 0
	}
	v, err := cfArrayGetValueAtIndex.Call(uintptr(r), uintptr(i))
	if err != nil {
		return 0
	}
	return Ref(v)
}

// IsDictionary reports whether r is a CFDictionaryRef.
func IsDictionary(r Ref) bool {
	return isType(r, kindDictionary)
}

// DictGet returns the borrowed value for key in dictionary d, or NULL if d
// is not a dictionary or has no such key.
func DictGet(d, key Ref) Ref {
	if key == 0 || !IsDictionary(d) {
		return 0
	}
	v, err := cfDictionaryGetValue.Call(uintptr(d), uintptr(key))
	if err != nil {
		return 0
	}
	return Ref(v)
}
