//go:build windows

package winnet

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Connection cost flags (NLM_CONNECTION_COST in netlistmgr.h).
const (
	CostUnrestricted         = 0x1
	CostFixed                = 0x2
	CostVariable             = 0x4
	CostOverDataLimit        = 0x10000
	CostCongested            = 0x20000
	CostRoaming              = 0x40000
	CostApproachingDataLimit = 0x80000
)

var (
	ole32            = windows.NewLazySystemDLL("ole32.dll") // System32 only: no DLL hijacking
	coCreateInstance = ole32.NewProc("CoCreateInstance")

	clsidNetworkListManager = windows.GUID{Data1: 0xDCB00C01, Data2: 0x570F, Data3: 0x4A9B, Data4: [8]byte{0x8D, 0x69, 0x19, 0x9F, 0xDB, 0xA5, 0x72, 0x3B}}
	iidINetworkCostManager  = windows.GUID{Data1: 0xDCB00008, Data2: 0x570F, Data3: 0x4A9B, Data4: [8]byte{0x8D, 0x69, 0x19, 0x9F, 0xDB, 0xA5, 0x72, 0x3B}}
)

const clsctxAll = 0x17

// costManager mirrors the COM object layout: a pointer to its method table.
type costManager struct{ vtbl *costManagerVtbl }

// INetworkCostManager derives from IUnknown; method order matters.
type costManagerVtbl struct {
	QueryInterface, AddRef, Release                uintptr
	GetCost, GetDataPlanStatus, SetDestinationAddr uintptr
}

// MachineCost returns Windows' cost classification for the current internet
// connection. "Metered" (Variable or any data-limit flag) is what Windows shows
// as "conexão limitada" and what it sets automatically for phone hotspots.
func MachineCost() (uint32, error) {
	runtime.LockOSThread() // COM apartments are per OS thread
	defer runtime.UnlockOSThread()

	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil {
		return 0, fmt.Errorf("CoInitializeEx: %w", err)
	}
	defer windows.CoUninitialize()

	var mgr *costManager
	hr, _, _ := coCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidNetworkListManager)), 0, clsctxAll,
		uintptr(unsafe.Pointer(&iidINetworkCostManager)), uintptr(unsafe.Pointer(&mgr)))
	if hr != 0 {
		return 0, fmt.Errorf("CoCreateInstance(NetworkListManager): HRESULT 0x%08X", uint32(hr))
	}
	defer syscall.SyscallN(mgr.vtbl.Release, uintptr(unsafe.Pointer(mgr)))

	var cost uint32
	hr, _, _ = syscall.SyscallN(mgr.vtbl.GetCost, uintptr(unsafe.Pointer(mgr)), uintptr(unsafe.Pointer(&cost)), 0)
	if hr != 0 {
		return 0, fmt.Errorf("INetworkCostManager.GetCost: HRESULT 0x%08X", uint32(hr))
	}
	return cost, nil
}

// IsMetered reports whether a cost value means the user pays per byte or has a cap.
func IsMetered(cost uint32) bool {
	return cost&(CostFixed|CostVariable|CostOverDataLimit|CostApproachingDataLimit|CostRoaming) != 0
}
