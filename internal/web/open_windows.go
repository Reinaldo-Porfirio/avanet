package web

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// OpenBrowser opens url in the default browser. It asks Windows directly, so
// no command line or shell is involved.
func OpenBrowser(url string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	if err := windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	return nil
}
