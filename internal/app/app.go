// Package app wires the application together and implements the command line.
//
// It is the only package that knows about every other one; everything below it
// receives its dependencies explicitly, which keeps them testable in isolation.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/Reinaldo-Porfirio/avanet/internal/buildinfo"
	"github.com/Reinaldo-Porfirio/avanet/internal/config"
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

var (
	// errUsage marks errors caused by invalid command line input.
	errUsage = errors.New("invalid usage")
	// errReported marks failures the command already explained to the user
	// in Portuguese; Main only sets the exit code.
	errReported = errors.New("failure already reported")
)

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
	case "probe":
		err = runProbe(ctx, rest, env)
	case "speedtest":
		err = runSpeedTest(ctx, rest, env)
	case "status":
		err = runStatus(ctx, rest, env)
	case "profile":
		err = runProfile(ctx, rest, env)
	case "autostart":
		err = runAutostart(rest, env)
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
	case ctx.Err() != nil && errors.Is(err, ctx.Err()):
		// Stopped by Ctrl+C or Windows shutting down: a normal exit.
		return ExitOK
	case errors.Is(err, errUsage):
		fmt.Fprintf(env.Stderr, "erro: %v\n\n", err)
		printUsage(env.Stderr)
		return ExitUsage
	case errors.Is(err, errReported):
		return ExitFailure
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
  run        monitora continuamente (padrão quando nenhum comando é informado)
             -tray  mostra o ícone perto do relógio em vez de escrever no terminal
  status     resumo de hoje: cobertura, última medição, último teste, dados usados
  probe      diagnóstico rápido: conexão, Wi-Fi e latência agora
             -n N  pacotes por alvo (padrão 10)
  speedtest  mede download e upload agora (respeita o orçamento diário de dados)
             -movel  permite este teste em dados móveis/hotspot
  profile    mostra os perfis de uso; "avanet profile <id>" escolhe um
  autostart  "on" inicia o AvaNet com o Windows, "off" desliga; sem argumento, mostra
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

// prepareDataDir loads the configuration and makes sure the data directory exists.
func prepareDataDir(env Env) (config.Config, error) {
	cfg, err := config.Load(env.Getenv)
	if err != nil {
		return config.Config{}, fmt.Errorf("load config: %w", err)
	}
	if err := os.MkdirAll(cfg.DataDir, dataDirPerm); err != nil {
		return config.Config{}, fmt.Errorf("create data directory: %w", err)
	}
	return cfg, nil
}
