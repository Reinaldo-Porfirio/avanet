package autostart

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

// scratchEntry uses a throwaway key, never the real Run key.
func scratchEntry(t *testing.T) Entry {
	t.Helper()
	key := fmt.Sprintf(`Software\AvaNet-test-%d-%d`, os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() {
		if err := registry.DeleteKey(registry.CURRENT_USER, key); err != nil && !errors.Is(err, registry.ErrNotExist) {
			t.Errorf("cleanup %s: %v", key, err)
		}
	})
	return Entry{KeyPath: key, ValueName: "AvaNet"}
}

func fakeExe(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "avanetw.exe")
	if err := os.WriteFile(exe, []byte("MZ"), 0o600); err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestEnableStatusDisable(t *testing.T) {
	entry := scratchEntry(t)
	exe := fakeExe(t)

	if _, enabled, err := entry.Status(); err != nil || enabled {
		t.Fatalf("before Enable: enabled=%v err=%v", enabled, err)
	}
	if err := entry.Enable(exe); err != nil {
		t.Fatal(err)
	}
	command, enabled, err := entry.Status()
	if err != nil || !enabled {
		t.Fatalf("after Enable: enabled=%v err=%v", enabled, err)
	}
	if want := `"` + exe + `" run -tray`; command != want {
		t.Errorf("command = %q, want %q", command, want)
	}
	for range 2 { // disabling twice is fine
		if err := entry.Disable(); err != nil {
			t.Fatal(err)
		}
	}
	if _, enabled, err := entry.Status(); err != nil || enabled {
		t.Errorf("after Disable: enabled=%v err=%v", enabled, err)
	}
}

func TestCommandRejectsUnsafePaths(t *testing.T) {
	goBuild := filepath.Join(t.TempDir(), "go-build123", "exe", "avanetw.exe")
	if err := os.MkdirAll(filepath.Dir(goBuild), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goBuild, []byte("MZ"), 0o600); err != nil {
		t.Fatal(err)
	}

	for name, exe := range map[string]string{
		"relative path":   `avanetw.exe`,
		"missing file":    filepath.Join(t.TempDir(), "nope.exe"),
		"directory":       t.TempDir(),
		"go run artifact": goBuild,
	} {
		if _, err := Command(exe); err == nil {
			t.Errorf("%s: %q accepted", name, exe)
		} else if strings.TrimSpace(err.Error()) == "" {
			t.Errorf("%s: empty error", name)
		}
	}
}
