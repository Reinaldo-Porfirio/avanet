//go:build !windows

package config

import (
	"fmt"
	"path/filepath"
)

const appDirName = "avanet"

// defaultBaseDir follows the XDG Base Directory specification. Windows is the
// only supported platform for now; this keeps the code buildable and testable
// elsewhere (CI runs the race detector on Linux).
func defaultBaseDir(getenv func(string) string) (string, error) {
	if dir := getenv("XDG_DATA_HOME"); dir != "" {
		return absoluteDir("XDG_DATA_HOME", dir)
	}

	home := getenv("HOME")
	if home == "" {
		return "", fmt.Errorf("%w: neither XDG_DATA_HOME nor HOME is set; set %s instead", ErrInvalid, EnvDataDir)
	}
	home, err := absoluteDir("HOME", home)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share"), nil
}
