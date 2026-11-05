//go:build !windows

package autostart

// Enable is not implemented outside Windows yet.
func (Entry) Enable(string) error { return ErrUnsupported }

// Disable is not implemented outside Windows yet.
func (Entry) Disable() error { return ErrUnsupported }

// Status is not implemented outside Windows yet.
func (Entry) Status() (string, bool, error) { return "", false, ErrUnsupported }
