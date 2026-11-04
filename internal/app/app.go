// Package app wires the application together and implements the command line.
//
// It is the only package that knows about every other one; everything below it
// receives its dependencies explicitly, which keeps them testable in isolation.
package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Reinaldo-Porfirio/avanet/internal/buildinfo"
	"github.com/Reinaldo-Porfirio/avanet/internal/config"
	"github.com/Reinaldo-Porfirio/avanet/internal/logging"
	"github.com/Reinaldo-Porfirio/avanet/internal/storage"
)

// Exit codes returned by Main.
const (
	ExitOK      = 0
	ExitFailure = 1
	ExitUsage   = 2
)

// dataDirPerm restricts the data directory to the current user where the
// operating system honors Unix permissions. On Windows, %LOCALAPPDATA% is
// already private to the user through its ACL.
const dataDirPerm = 0o700

// Env holds the process-level dependencies, injected so tests never touch the
// real environment or standard streams.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(key string) string
}

// errUsage marks errors caused by invalid command line input.
var errUsage = errors.New("invalid usage")

// Main runs the command named by args[0] and returns the process exit code.
// With no arguments it runs the agent, which is how Windows starts it at logon.
func Main(ctx context.Context, args []string, env Env) int {
	command, rest := "run", []string(nil)
	if len(args) > 0 {
		command, rest = args[0], args[1:]
	}

	var err error
	switch command {
	case "run":
		err = runAgent(ctx, rest, env)
	case "version":
		err = printVersion(rest, env)
	case "help", "-h", "-help", "--help":
		printUsage(env.Stdout)
		return ExitOK
	default:
		err = fmt.Errorf("%w: unknown command %q", errUsage, command)
	}

	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, errUsage):
		fmt.Fprintf(env.Stderr, "erro: %v\n\n", err)
		printUsage(env.Stderr)
		return ExitUsage
	default:
		fmt.Fprintf(env.Stderr, "erro: %v\n", err)
		return ExitFailure
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `AvaNet: monitora a qualidade da sua internet em segundo plano.

Uso:
  avanet [comando]

Comandos:
  run        inicia o monitoramento (padrão quando nenhum comando é informado)
  version    mostra a versão
  help       mostra esta ajuda

Variáveis de ambiente:
  AVANET_DATA_DIR   pasta dos dados (padrão: %LOCALAPPDATA%\AvaNet)
  AVANET_LOG_LEVEL  debug, info, warn ou error (padrão: info)
`)
}

func printVersion(args []string, env Env) error {
	if len(args) > 0 {
		return fmt.Errorf("%w: version takes no arguments", errUsage)
	}
	fmt.Fprintf(env.Stdout, "avanet %s\n", buildinfo.Version())
	return nil
}

func runAgent(ctx context.Context, args []string, env Env) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(io.Discard) // usage is printed by Main
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("%w: run takes no arguments", errUsage)
	}

	cfg, err := config.Load(env.Getenv)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := os.MkdirAll(cfg.DataDir, dataDirPerm); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}

	logger, logFile, err := logging.New(logging.Options{Dir: cfg.LogDir(), Level: cfg.LogLevel})
	if err != nil {
		return fmt.Errorf("start logging: %w", err)
	}
	defer func() {
		if closeErr := logFile.Close(); closeErr != nil {
			fmt.Fprintf(env.Stderr, "aviso: falha ao fechar o log: %v\n", closeErr)
		}
	}()

	db, err := storage.Open(ctx, cfg.DatabasePath())
	if err != nil {
		if ctx.Err() != nil {
			// Interrupted during startup (e.g. Ctrl+C): a normal shutdown, not a
			// failure. Migrations run in transactions, so nothing is left half-done.
			logger.Info("agent interrupted during startup", "reason", context.Cause(ctx))
			return nil
		}
		logger.Error("open database failed", "error", err)
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			logger.Error("close database failed", "error", closeErr)
		}
	}()

	logger.Info("agent started", "version", buildinfo.Version(), "data_dir", cfg.DataDir)
	fmt.Fprintf(env.Stdout, "AvaNet %s em execução. Dados em %s. Ctrl+C para sair.\n", buildinfo.Version(), cfg.DataDir)

	<-ctx.Done()

	logger.Info("agent stopping", "reason", context.Cause(ctx))
	return nil
}
