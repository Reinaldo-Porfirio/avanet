// Package notify shows notifications to the user.
//
// Windows keeps but does not display a toast from an application it cannot
// identify. For a program without an installer package, what identifies it is
// a Start Menu shortcut carrying its AppUserModelID: without one the call
// succeeds and nothing appears (validated on Windows 11 23H2 in Phase 5, also
// with the registry-only registration). Setup creates that shortcut.
package notify

import (
	"errors"
	"fmt"

	"git.sr.ht/~jackmordaunt/go-toast/v2"
)

const (
	// AppID is the identity Windows knows AvaNet by.
	AppID = "AvaNet.Monitor"
	// ShortcutName is the Start Menu entry, which also carries AppID.
	ShortcutName = "AvaNet.lnk"
)

// ErrUnsupported means the platform has no Start Menu to register in.
var ErrUnsupported = errors.New("notifications are not supported on this platform")

// Toast sends Windows toast notifications.
type Toast struct{}

// Notify shows a notification. Setup must have run for it to be displayed.
func (Toast) Notify(title, body string) error {
	n := toast.Notification{AppID: AppID, Title: title, Body: body}
	if err := n.Push(); err != nil {
		return fmt.Errorf("show notification: %w", err)
	}
	return nil
}
