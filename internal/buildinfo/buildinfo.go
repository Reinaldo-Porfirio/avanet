// Package buildinfo reports the version of the running binary.
package buildinfo

import "runtime/debug"

// version is set by release builds:
//
//	go build -ldflags "-X github.com/Reinaldo-Porfirio/avanet/internal/buildinfo.version=v1.2.3"
var version string

// Version returns the release version, the module version recorded by the Go
// toolchain (which includes the VCS revision), or "dev" when neither exists.
func Version() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
