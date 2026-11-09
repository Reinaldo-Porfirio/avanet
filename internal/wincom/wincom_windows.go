// Package wincom holds the small part of Windows COM that AvaNet needs.
//
// A COM object is a pointer to a table of function pointers (the vtable), and
// a method is called by its position in that table. Callers invoke methods
// with syscall.SyscallN directly, because Go only keeps pointer arguments
// valid when they are converted to uintptr in the call expression itself.
package wincom

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	clsctxAll       = 0x17       // CLSCTX_INPROC_SERVER | INPROC_HANDLER | LOCAL_SERVER | REMOTE_SERVER
	sFalse          = 1          // COM already initialized on this thread (still needs CoUninitialize)
	rpcEChangedMode = 0x80010106 // COM initialized by someone else in another mode

	hresultSuccessMax = 0x7FFFFFFF

	methodQueryInterface = 0
	methodRelease        = 2
)

var (
	// NewLazySystemDLL loads only from System32, preventing DLL hijacking.
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procPropVariantClear = ole32.NewProc("PropVariantClear")
)

// Object is a COM interface pointer.
type Object struct {
	vtable *[64]uintptr // real tables are shorter; only valid indexes are read
}

// Ptr is the object's own address: the first argument of every method.
func (o *Object) Ptr() uintptr { return uintptr(unsafe.Pointer(o)) }

// Method returns the address of the method at a vtable position. Positions
// follow the declaration order in the Windows headers, inherited methods first.
func (o *Object) Method(index int) uintptr { return o.vtable[index] }

// Release drops our reference. IUnknown::Release returns the remaining
// reference count, not an error, so there is nothing to check.
func (o *Object) Release() {
	_, _, _ = syscall.SyscallN(o.Method(methodRelease), o.Ptr())
}

// Query asks the object for another of its interfaces.
func (o *Object) Query(iid *windows.GUID) (*Object, error) {
	var out *Object
	hr, _, _ := syscall.SyscallN(o.Method(methodQueryInterface), o.Ptr(), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	if err := Check(hr, "QueryInterface"); err != nil {
		return nil, err
	}
	return out, nil
}

// Create instantiates a COM class.
func Create(clsid, iid *windows.GUID) (*Object, error) {
	var out *Object
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(clsid)), 0, clsctxAll, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	if err := Check(hr, "CoCreateInstance"); err != nil {
		return nil, err
	}
	if out == nil {
		return nil, errors.New("CoCreateInstance returned no object")
	}
	return out, nil
}

// Run calls fn on a COM-initialized thread. COM state belongs to the OS
// thread, so the goroutine is pinned to it until fn returns.
func Run(fn func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	mustUninit, err := initialize()
	if err != nil {
		return err
	}
	if mustUninit {
		defer windows.CoUninitialize()
	}
	return fn()
}

func initialize() (mustUninit bool, err error) {
	err = windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED)
	var errno syscall.Errno
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &errno) && errno == sFalse:
		return true, nil
	case errors.As(err, &errno) && uint32(errno) == rpcEChangedMode:
		return false, nil // usable as is; not ours to uninitialize
	default:
		return false, fmt.Errorf("CoInitializeEx: %w", err)
	}
}

// Check turns a failed HRESULT into an error.
func Check(r uintptr, op string) error {
	if !Failed(r) {
		return nil
	}
	return fmt.Errorf("%s: HRESULT 0x%08X", op, hresult(r))
}

// hresult extracts the HRESULT from a syscall return register. HRESULT is
// defined as 32 bits; the upper half of a 64-bit register is not part of it.
func hresult(r uintptr) uint32 { return uint32(r & 0xFFFFFFFF) }

// Failed reports whether an HRESULT signals failure (its high bit is set).
func Failed(r uintptr) bool { return hresult(r) > hresultSuccessMax }

// ClearPropVariant frees what a COM call allocated inside a PROPVARIANT.
func ClearPropVariant(v unsafe.Pointer) error {
	hr, _, _ := procPropVariantClear.Call(uintptr(v))
	return Check(hr, "PropVariantClear")
}
