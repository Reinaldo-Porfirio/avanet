package netinfo

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

// NLM_CONNECTION_COST flags (netlistmgr.h).
const (
	costFixed                = 0x2
	costVariable             = 0x4
	costOverDataLimit        = 0x10000
	costRoaming              = 0x40000
	costApproachingDataLimit = 0x80000

	meteredFlags = costFixed | costVariable | costOverDataLimit | costRoaming | costApproachingDataLimit
)

const (
	clsctxAll         = 0x17       // CLSCTX_INPROC_SERVER | INPROC_HANDLER | LOCAL_SERVER | REMOTE_SERVER
	sFalse            = 1          // COM already initialized on this thread (still needs CoUninitialize)
	rpcEChangedMode   = 0x80010106 // COM initialized by someone else in another mode
	hresultSuccessMax = 0x7FFFFFFF
)

var (
	// NewLazySystemDLL loads only from System32, preventing DLL hijacking.
	ole32            = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInst = ole32.NewProc("CoCreateInstance")

	clsidNetworkListManager = windows.GUID{Data1: 0xDCB00C01, Data2: 0x570F, Data3: 0x4A9B, Data4: [8]byte{0x8D, 0x69, 0x19, 0x9F, 0xDB, 0xA5, 0x72, 0x3B}}
	iidINetworkCostManager  = windows.GUID{Data1: 0xDCB00008, Data2: 0x570F, Data3: 0x4A9B, Data4: [8]byte{0x8D, 0x69, 0x19, 0x9F, 0xDB, 0xA5, 0x72, 0x3B}}
)

// networkCostManager mirrors a COM object: its first word points to the
// method table (vtable).
type networkCostManager struct{ vtbl *networkCostManagerVtbl }

// networkCostManagerVtbl lists INetworkCostManager's methods in declaration
// order, starting with those inherited from IUnknown. The order is the ABI.
type networkCostManagerVtbl struct {
	QueryInterface          uintptr
	AddRef                  uintptr
	Release                 uintptr
	GetCost                 uintptr
	GetDataPlanStatus       uintptr
	SetDestinationAddresses uintptr
}

// connectionCost asks the Network List Manager how Windows classifies the
// current internet connection. It is the same information Windows uses for
// "metered connection", which it sets automatically for phone hotspots.
func connectionCost() (cost domain.ConnectionCost, err error) {
	// COM state belongs to the OS thread, so the goroutine must not move
	// between threads until we are done.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	uninit, err := initCOM()
	if err != nil {
		return domain.CostUnknown, err
	}
	if uninit {
		defer windows.CoUninitialize()
	}

	var manager *networkCostManager
	hr, _, _ := procCoCreateInst.Call(
		uintptr(unsafe.Pointer(&clsidNetworkListManager)), 0, clsctxAll,
		uintptr(unsafe.Pointer(&iidINetworkCostManager)), uintptr(unsafe.Pointer(&manager)))
	if failed(hr) || manager == nil {
		return domain.CostUnknown, fmt.Errorf("create NetworkListManager: HRESULT 0x%08X", hresult(hr))
	}
	defer manager.release()

	var flags uint32
	// A nil destination asks for the machine-wide (internet) connection cost.
	hr, _, _ = syscall.SyscallN(manager.vtbl.GetCost, uintptr(unsafe.Pointer(manager)), uintptr(unsafe.Pointer(&flags)), 0)
	if failed(hr) {
		return domain.CostUnknown, fmt.Errorf("INetworkCostManager.GetCost: HRESULT 0x%08X", hresult(hr))
	}
	return costFromFlags(flags), nil
}

// release drops our reference to the COM object. IUnknown::Release returns
// the remaining reference count, not an error, so there is nothing to check.
func (m *networkCostManager) release() {
	_, _, _ = syscall.SyscallN(m.vtbl.Release, uintptr(unsafe.Pointer(m)))
}

// initCOM initializes COM for this thread and reports whether the caller must
// balance it with CoUninitialize.
func initCOM() (mustUninit bool, err error) {
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

// hresult extracts the HRESULT from a syscall return register. HRESULT is
// defined as 32 bits; the upper half of a 64-bit register is not part of it.
func hresult(r uintptr) uint32 {
	return uint32(r & 0xFFFFFFFF)
}

// failed reports whether an HRESULT signals failure (its high bit is set).
func failed(r uintptr) bool { return hresult(r) > hresultSuccessMax }

func costFromFlags(flags uint32) domain.ConnectionCost {
	switch {
	case flags == 0:
		return domain.CostUnknown // NLM_CONNECTION_COST_UNKNOWN
	case flags&meteredFlags != 0:
		return domain.CostMetered
	default:
		return domain.CostUnmetered
	}
}
