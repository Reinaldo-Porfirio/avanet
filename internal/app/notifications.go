package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/autostart"
	"github.com/Reinaldo-Porfirio/avanet/internal/notify"
)

// shortcutSettle is how long Windows takes to notice a new Start Menu
// shortcut; a toast sent sooner would not be displayed.
const shortcutSettle = 3 * time.Second

// errTemporaryBuild means the program runs from a `go run` build, which is
// deleted afterwards and must not end up in the Start Menu.
var errTemporaryBuild = errors.New("running from a temporary build: notifications need a built executable")

// setupNotifications creates the Start Menu entry Windows requires before it
// displays AvaNet's notifications. It points to the windowless executable
// when there is one next to this program.
func setupNotifications(ctx context.Context) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}
	target := filepath.Join(filepath.Dir(self), trayExecutable)
	if _, err := os.Stat(target); err != nil {
		target = self
	}
	if autostart.IsTemporaryBuild(target) {
		return errTemporaryBuild
	}

	created, err := notify.Setup(target)
	if err != nil {
		return err
	}
	if created {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(shortcutSettle):
		}
	}
	return nil
}
