//go:build !windows

package notify

// Setup is not implemented outside Windows yet.
func Setup(string) (bool, error) { return false, ErrUnsupported }
