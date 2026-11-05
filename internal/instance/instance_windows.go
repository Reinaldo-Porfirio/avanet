package instance

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

type mutexLock struct{ handle windows.Handle }

// Acquire takes a named mutex in the session-local namespace ("Local\"):
// each logged-in user has their own agent.
func Acquire(name string) (Lock, error) {
	ptr, err := windows.UTF16PtrFromString(`Local\` + name)
	if err != nil {
		return nil, fmt.Errorf("instance name: %w", err)
	}
	handle, err := windows.CreateMutex(nil, false, ptr)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		// CreateMutex still returned a handle to the existing mutex.
		return nil, errors.Join(ErrAlreadyRunning, windows.CloseHandle(handle))
	}
	if err != nil {
		return nil, fmt.Errorf("create instance mutex: %w", err)
	}
	return &mutexLock{handle: handle}, nil
}

func (l *mutexLock) Release() error {
	if err := windows.CloseHandle(l.handle); err != nil {
		return fmt.Errorf("release instance mutex: %w", err)
	}
	return nil
}
