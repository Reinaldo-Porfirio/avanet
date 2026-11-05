// Package instance guarantees a single running agent per user session, so
// starting AvaNet twice (autostart plus a click) never measures twice.
package instance

import "errors"

// ErrAlreadyRunning means another process holds the instance.
var ErrAlreadyRunning = errors.New("another instance is already running")

// Lock is held for the lifetime of the process. The operating system also
// releases it if the process dies, so a crash never blocks the next start.
type Lock interface {
	Release() error
}
