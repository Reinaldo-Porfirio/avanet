//go:build windows

// Command avanetw is AvaNet without a console window: the build Windows
// starts at logon, showing only the tray icon. Build it with
//
//	go build -ldflags -H=windowsgui -o avanetw.exe ./cmd/avanetw
//
// Same idea as python.exe/pythonw.exe: one code base, two entry points.
package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"

	"github.com/Reinaldo-Porfirio/avanet/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	// There is no console: capture what would go to stderr and show it in a
	// message box if the agent cannot start.
	var stderr bytes.Buffer
	code := app.Main(ctx, []string{"run", "-tray"}, app.Env{
		Stdout: io.Discard,
		Stderr: &stderr,
		Getenv: os.Getenv,
	})
	stop()

	if code != app.ExitOK {
		showError(strings.TrimSpace(stderr.String()))
	}
	os.Exit(code)
}

func showError(message string) {
	if message == "" {
		message = "O AvaNet não conseguiu iniciar. Veja o log em %LOCALAPPDATA%\\AvaNet\\logs."
	}
	text, err := windows.UTF16PtrFromString(message)
	if err != nil {
		return // the message itself is invalid; nothing safe left to show
	}
	title, _ := windows.UTF16PtrFromString("AvaNet") // constant ASCII: cannot fail
	_, _ = windows.MessageBox(0, text, title, windows.MB_OK|windows.MB_ICONINFORMATION)
}
