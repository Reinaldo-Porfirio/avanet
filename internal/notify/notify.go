// Package notify shows notifications to the user.
package notify

import (
	"fmt"
	"sync"

	"git.sr.ht/~jackmordaunt/go-toast/v2"
)

// AppID is the name Windows shows on AvaNet's notifications. It is registered
// under HKCU\SOFTWARE\Classes\AppUserModelId\<AppID>, which needs no
// administrator rights.
const AppID = "AvaNet"

// Toast sends Windows toast notifications. Use a pointer: registering the
// application happens once. On other platforms the library does nothing.
type Toast struct {
	once sync.Once
	err  error
}

// Notify shows a notification.
func (t *Toast) Notify(title, body string) error {
	t.once.Do(func() { t.err = toast.SetAppData(toast.AppData{AppID: AppID}) })
	if t.err != nil {
		return fmt.Errorf("register notifications: %w", t.err)
	}
	n := toast.Notification{AppID: AppID, Title: title, Body: body}
	if err := n.Push(); err != nil {
		return fmt.Errorf("show notification: %w", err)
	}
	return nil
}
