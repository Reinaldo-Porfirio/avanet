package notify

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/Reinaldo-Porfirio/avanet/internal/wincom"
)

// Positions in the vtables, after the three methods of IUnknown.
const (
	shellLinkGetPath = 3
	shellLinkSetPath = 20

	persistFileLoad = 5
	persistFileSave = 6

	propertyStoreGetValue = 5
	propertyStoreSetValue = 6
	propertyStoreCommit   = 7
)

const (
	vtLPWSTR    = 31 // PROPVARIANT holding a wide string
	stgmRead    = 0
	slgpRawPath = 4 // SLGP_RAWPATH: the path as stored
	maxPath     = 32768
)

var (
	clsidShellLink   = windows.GUID{Data1: 0x00021401, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidShellLink     = windows.GUID{Data1: 0x000214F9, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidPersistFile   = windows.GUID{Data1: 0x0000010B, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidPropertyStore = windows.GUID{Data1: 0x886D8EEB, Data2: 0x8CF2, Data3: 0x4446, Data4: [8]byte{0x8D, 0x02, 0xCD, 0xBA, 0x1D, 0xBD, 0xCF, 0x99}}

	// keyAppUserModelID is PKEY_AppUserModel_ID.
	keyAppUserModelID = propertyKey{
		fmtid: windows.GUID{Data1: 0x9F4C2855, Data2: 0x9F79, Data3: 0x4B39, Data4: [8]byte{0xA8, 0xD0, 0xE1, 0xD4, 0x2D, 0xE1, 0xD5, 0xF3}},
		pid:   5,
	}
)

type propertyKey struct {
	fmtid windows.GUID
	pid   uint32
}

// propVariant is a PROPVARIANT holding a string, its only use here.
type propVariant struct {
	vt       uint16
	reserved [3]uint16
	value    *uint16
	extra    uintptr
}

// Setup makes sure the Start Menu entry that lets Windows display AvaNet's
// notifications exists and starts target. It reports whether it had to write
// the shortcut: Windows takes a moment to notice a new one.
func Setup(target string) (created bool, err error) {
	programs, err := windows.KnownFolderPath(windows.FOLDERID_Programs, 0)
	if err != nil {
		return false, fmt.Errorf("locate Start Menu: %w", err)
	}
	return ensureShortcut(filepath.Join(programs, ShortcutName), target, AppID)
}

// ensureShortcut writes a shortcut unless an identical one already exists, so
// starting the agent every day does not rewrite the file.
func ensureShortcut(path, target, appID string) (written bool, err error) {
	err = wincom.Run(func() error {
		var upToDate bool
		err := withShellLink(func(link, persist, store *wincom.Object) (err error) {
			upToDate, err = shortcutMatches(link, persist, store, path, target, appID)
			return err
		})
		if err != nil || upToDate {
			return err
		}
		// A fresh object: one loaded for reading refuses property changes.
		err = withShellLink(func(link, persist, store *wincom.Object) error {
			return writeShortcut(link, persist, store, path, target, appID)
		})
		written = err == nil
		return err
	})
	return written, err
}

// withShellLink creates a shell link object and gives fn the interfaces used
// here. It must run inside wincom.Run.
func withShellLink(fn func(link, persist, store *wincom.Object) error) error {
	link, err := wincom.Create(&clsidShellLink, &iidShellLink)
	if err != nil {
		return fmt.Errorf("create shell link: %w", err)
	}
	defer link.Release()
	persist, err := link.Query(&iidPersistFile)
	if err != nil {
		return err
	}
	defer persist.Release()
	store, err := link.Query(&iidPropertyStore)
	if err != nil {
		return err
	}
	defer store.Release()
	return fn(link, persist, store)
}

func shortcutMatches(link, persist, store *wincom.Object, path, target, appID string) (bool, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}
	hr, _, _ := syscall.SyscallN(persist.Method(persistFileLoad), persist.Ptr(), uintptr(unsafe.Pointer(file)), stgmRead)
	if wincom.Failed(hr) {
		return false, nil // unreadable: replace it
	}

	buffer := make([]uint16, maxPath)
	hr, _, _ = syscall.SyscallN(link.Method(shellLinkGetPath), link.Ptr(), uintptr(unsafe.Pointer(&buffer[0])), maxPath, 0, slgpRawPath)
	if err := wincom.Check(hr, "read shortcut target"); err != nil {
		return false, err
	}
	if !sameFile(windows.UTF16ToString(buffer), target) {
		return false, nil
	}

	var value propVariant
	hr, _, _ = syscall.SyscallN(store.Method(propertyStoreGetValue), store.Ptr(), uintptr(unsafe.Pointer(&keyAppUserModelID)), uintptr(unsafe.Pointer(&value)))
	if err := wincom.Check(hr, "read shortcut identity"); err != nil {
		return false, err
	}
	current := ""
	if value.vt == vtLPWSTR && value.value != nil {
		current = windows.UTF16PtrToString(value.value)
	}
	if err := wincom.ClearPropVariant(unsafe.Pointer(&value)); err != nil {
		return false, err
	}
	return current == appID, nil
}

// sameFile compares files, not spellings: Windows stores a shortcut's target in
// its long form even when it was given an 8.3 path such as C:\Users\RUNNER~1.
// A path that cannot be read is no match, so the shortcut gets rewritten.
func sameFile(a, b string) bool {
	infoA, err := os.Stat(a)
	if err != nil {
		return false
	}
	infoB, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(infoA, infoB)
}

func writeShortcut(link, persist, store *wincom.Object, path, target, appID string) error {
	targetPtr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	identity, err := windows.UTF16PtrFromString(appID)
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}

	hr, _, _ := syscall.SyscallN(link.Method(shellLinkSetPath), link.Ptr(), uintptr(unsafe.Pointer(targetPtr)))
	if err := wincom.Check(hr, "set shortcut target"); err != nil {
		return err
	}
	value := propVariant{vt: vtLPWSTR, value: identity}
	hr, _, _ = syscall.SyscallN(store.Method(propertyStoreSetValue), store.Ptr(), uintptr(unsafe.Pointer(&keyAppUserModelID)), uintptr(unsafe.Pointer(&value)))
	if err := wincom.Check(hr, "set shortcut identity"); err != nil {
		return err
	}
	hr, _, _ = syscall.SyscallN(store.Method(propertyStoreCommit), store.Ptr())
	if err := wincom.Check(hr, "commit shortcut identity"); err != nil {
		return err
	}
	hr, _, _ = syscall.SyscallN(persist.Method(persistFileSave), persist.Ptr(), uintptr(unsafe.Pointer(file)), 1)
	return wincom.Check(hr, "save shortcut")
}
