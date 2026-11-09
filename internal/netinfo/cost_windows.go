package netinfo

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
	"github.com/Reinaldo-Porfirio/avanet/internal/wincom"
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

// methodGetCost is INetworkCostManager::GetCost in the vtable, after the
// three methods inherited from IUnknown.
const methodGetCost = 3

var (
	clsidNetworkListManager = windows.GUID{Data1: 0xDCB00C01, Data2: 0x570F, Data3: 0x4A9B, Data4: [8]byte{0x8D, 0x69, 0x19, 0x9F, 0xDB, 0xA5, 0x72, 0x3B}}
	iidINetworkCostManager  = windows.GUID{Data1: 0xDCB00008, Data2: 0x570F, Data3: 0x4A9B, Data4: [8]byte{0x8D, 0x69, 0x19, 0x9F, 0xDB, 0xA5, 0x72, 0x3B}}
)

// connectionCost asks the Network List Manager how Windows classifies the
// current internet connection. It is the same information Windows uses for
// "metered connection", which it sets automatically for phone hotspots.
func connectionCost() (cost domain.ConnectionCost, err error) {
	err = wincom.Run(func() error {
		manager, err := wincom.Create(&clsidNetworkListManager, &iidINetworkCostManager)
		if err != nil {
			return fmt.Errorf("create NetworkListManager: %w", err)
		}
		defer manager.Release()

		var flags uint32
		// A nil destination asks for the machine-wide (internet) connection cost.
		hr, _, _ := syscall.SyscallN(manager.Method(methodGetCost), manager.Ptr(), uintptr(unsafe.Pointer(&flags)), 0)
		if err := wincom.Check(hr, "INetworkCostManager.GetCost"); err != nil {
			return err
		}
		cost = costFromFlags(flags)
		return nil
	})
	return cost, err
}

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
