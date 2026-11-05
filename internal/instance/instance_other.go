//go:build !windows

package instance

type noLock struct{}

func (noLock) Release() error { return nil }

// Acquire is a no-op outside Windows, the only supported platform for now.
func Acquire(string) (Lock, error) { return noLock{}, nil }
