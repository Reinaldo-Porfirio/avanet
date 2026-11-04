package logging

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestRotatingFileRotatesAndKeepsBackups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	const maxBytes, maxBackups = 10, 2

	file, err := openRotatingFile(path, maxBytes, maxBackups)
	if err != nil {
		t.Fatal(err)
	}

	// Each 6-byte line forces a rotation before the next one.
	for _, line := range []string{"line1\n", "line2\n", "line3\n", "line4\n"} {
		if _, err := file.Write([]byte(line)); err != nil {
			t.Fatalf("Write(%q): %v", line, err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	expected := []struct {
		path    string
		content string
	}{
		{path, "line4\n"},
		{backupPath(path, 1), "line3\n"},
		{backupPath(path, 2), "line2\n"}, // line1 was dropped: only 2 backups are kept
	}
	for _, e := range expected {
		got, err := os.ReadFile(e.path)
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Base(e.path), err)
		}
		if !bytes.Equal(got, []byte(e.content)) {
			t.Errorf("%s = %q, want %q", filepath.Base(e.path), got, e.content)
		}
	}

	if _, err := os.Stat(backupPath(path, maxBackups+1)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("backup beyond the limit exists (stat error: %v)", err)
	}
}

func TestRotatingFileWritesOversizedLineWhole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	file, err := openRotatingFile(path, 4, 1)
	if err != nil {
		t.Fatal(err)
	}
	line := []byte("a line longer than the limit\n")
	if _, err := file.Write(line); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, line) {
		t.Errorf("file = %q, want %q", got, line)
	}
}

func TestRotatingFileAppendsToExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("old\n"), filePerm); err != nil {
		t.Fatal(err)
	}

	file, err := openRotatingFile(path, 100, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("new\n")); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old\nnew\n" {
		t.Errorf("file = %q, want existing content preserved", got)
	}
}

func TestWriteAfterCloseFails(t *testing.T) {
	file, err := openRotatingFile(filepath.Join(t.TempDir(), "app.log"), 100, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("x")); err == nil {
		t.Error("Write after Close succeeded, want error")
	}
}
