//go:build !windows

package web

import "errors"

// OpenBrowser is only implemented on Windows.
func OpenBrowser(string) error { return errors.ErrUnsupported }
