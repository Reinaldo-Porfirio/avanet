package config

import "fmt"

const appDirName = "AvaNet"

// defaultBaseDir returns %LOCALAPPDATA%: per-user, not synced between machines
// (unlike %APPDATA%), which suits a local measurement history.
func defaultBaseDir(getenv func(string) string) (string, error) {
	dir := getenv("LOCALAPPDATA")
	if dir == "" {
		return "", fmt.Errorf("%w: LOCALAPPDATA is not set; set %s instead", ErrInvalid, EnvDataDir)
	}
	return absoluteDir("LOCALAPPDATA", dir)
}
