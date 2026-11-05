// Package autostart makes AvaNet start when the user logs in to Windows.
//
// It uses the per-user Run key (HKCU\...\CurrentVersion\Run): no
// administrator rights, and it can be turned off in Task Manager's
// "Startup apps" tab like any other app.
package autostart

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrUnsupported means this platform has no implementation yet.
var ErrUnsupported = errors.New("autostart is not supported on this platform")

// RunKey is the registry key, under HKEY_CURRENT_USER, that Windows reads at logon.
const RunKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// Entry is one autostart registration. Tests point it at a scratch key.
type Entry struct {
	KeyPath   string // under HKEY_CURRENT_USER
	ValueName string
}

// Default is AvaNet's real registration.
func Default() Entry { return Entry{KeyPath: RunKey, ValueName: "AvaNet"} }

// Command builds the command line stored in the registry for exe. The
// executable must exist at a stable, absolute path: never a temporary build
// from `go run`, which would vanish and leave a broken entry behind.
func Command(exe string) (string, error) {
	if !filepath.IsAbs(exe) {
		return "", fmt.Errorf("autostart needs an absolute path, got %q", exe)
	}
	info, err := os.Stat(exe)
	if err != nil {
		return "", fmt.Errorf("autostart executable: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("autostart executable is a directory: %q", exe)
	}
	if strings.Contains(strings.ToLower(filepath.ToSlash(exe)), "/go-build") {
		return "", fmt.Errorf("refusing to register a temporary `go run` build: %q (build the executable first)", exe)
	}
	if strings.ContainsRune(exe, '"') {
		return "", fmt.Errorf("invalid character in path: %q", exe)
	}
	return `"` + exe + `" run -tray`, nil
}
