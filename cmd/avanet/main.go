// Command avanet monitors the quality of the computer's network connection in the
// background and produces a daily report.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/Reinaldo-Porfirio/avanet/internal/app"
)

func main() {
	// On Windows, Go maps console close, logoff and shutdown events to SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	code := app.Main(ctx, os.Args[1:], app.Env{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Getenv: os.Getenv,
	})

	stop()
	os.Exit(code)
}
