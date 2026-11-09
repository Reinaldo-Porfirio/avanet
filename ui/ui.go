// Package ui holds the panel's files, built into the executable so the panel
// needs nothing from the internet.
package ui

import (
	"embed"
	"io/fs"
)

//go:embed static
var embedded embed.FS

// Assets returns the panel's files, with index.html at the root.
func Assets() (fs.FS, error) { return fs.Sub(embedded, "static") }
