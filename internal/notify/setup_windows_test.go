package notify

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"
)

func TestShortcutStructLayouts(t *testing.T) {
	if got := unsafe.Sizeof(propVariant{}); got != 24 {
		t.Errorf("PROPVARIANT = %d bytes, want 24", got)
	}
	if got := unsafe.Sizeof(propertyKey{}); got != 20 {
		t.Errorf("PROPERTYKEY = %d bytes, want 20", got)
	}
}

// TestEnsureShortcut works in a temporary folder, never in the real Start Menu.
func TestEnsureShortcut(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AvaNet Test.lnk")
	first := filepath.Join(dir, "first.exe")
	second := filepath.Join(dir, "second.exe")
	for _, exe := range []string{first, second} {
		if err := os.WriteFile(exe, []byte("MZ"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	steps := []struct {
		name, target, appID string
		wantWritten         bool
	}{
		{"new shortcut", first, "AvaNet.Test", true},
		{"identical: left alone", first, "AvaNet.Test", false},
		{"executable moved", second, "AvaNet.Test", true},
		{"identity changed", second, "AvaNet.Other", true},
		{"identical again", second, "AvaNet.Other", false},
	}
	for _, s := range steps {
		written, err := ensureShortcut(path, s.target, s.appID)
		if err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		if written != s.wantWritten {
			t.Errorf("%s: written = %v, want %v", s.name, written, s.wantWritten)
		}
	}
}

func TestEnsureShortcutReplacesCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AvaNet Test.lnk")
	exe := filepath.Join(dir, "app.exe")
	for _, file := range []string{path, exe} {
		if err := os.WriteFile(file, []byte("not a shortcut"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	written, err := ensureShortcut(path, exe, "AvaNet.Test")
	if err != nil || !written {
		t.Errorf("written = %v, err = %v; a corrupt shortcut must be replaced", written, err)
	}
}
