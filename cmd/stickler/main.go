package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sort"

	app "github.com/gomatic/go-app"
	"github.com/gomatic/go-log"
	"github.com/urfave/cli/v3"

	"github.com/gomatic/stickler/internal/app/commands/instructions"
	"github.com/gomatic/stickler/internal/app/commands/lint"
)

const (
	argUsage    = ``
	description = `stickler is the gomatic lint runner: it executes the configured checks to
completion, normalizes their findings into one diagnostic schema, and reports
pass or fail through the process exit code.

A tool is DATA. Which checkers run, how each is invoked, how its configuration
is merged, and how its output is parsed are all declared in .stickler.yaml
layered over the global configuration — never in Go. The only per-tool code
stickler carries is an output parser.

Available Commands:
  instructions - Print the rules every configured check enforces
  lint         - Run the suite over a root (the default; "stickler ./..." is "stickler lint ./...")

Findings that are neither softened nor probed fail the build. A softened rule is
still reported and still counted, against a committed per-rule baseline that may
only fall — so a soft rule is non-blocking, never invisible. A PROBE is a rule
the global configuration declares judgment-bound: it reports at any count and
never gates, because the remedy is to adjudicate the finding rather than to
record a number.`
	envName   = "STICKLER"
	envPrefix = envName + "_"
	name      = `stickler`
	usage     = `Run the gomatic lint suite and report via exit code.`
)

var (
	appCreator    = createApp
	loggerConfig  log.LoggerConfig
	loggerCreator = productionLogger
)

// productionLogger builds the application logger from the parsed logging flags.
// It is invoked from the root Before hook, after flag parsing has populated
// loggerConfig, so --log-level and --log-format take effect.
func productionLogger(_ *cli.Command) *slog.Logger {
	return loggerConfig.NewLogger(os.Stderr)
}

// version is the application version.
// Set via ldflags: -X main.version=1.0.0
var version = "dev"

// osExit is indirected so tests can observe the process exit code.
var osExit = os.Exit

func main() { osExit(run(os.Args)) }

// run builds and executes the CLI, returning the process exit code. Keeping the
// exit code as a return value (rather than calling os.Exit here) makes the whole
// run path testable.
func run(args []string) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	if err := appCreator(loggerCreator).Run(ctx, args); err != nil {
		slog.Error("Application error", "error", err)
		return 1
	}
	return 0
}

// createApp constructs the definition of the CLI.
func createApp(getLogger app.GetLoggerFunc) *cli.Command {
	cliApp := &cli.Command{
		Name:                  name,
		Usage:                 usage,
		ArgsUsage:             argUsage,
		Description:           description,
		Version:               version,
		EnableShellCompletion: true,
		// Every CI job invokes the bare `stickler [root]`. urfave prepends the
		// default command to the positional args, so that invocation keeps
		// meaning `stickler lint [root]` now that the tool has a command tree.
		DefaultCommand: lint.Name,
		Commands: []*cli.Command{
			instructions.Command(),
			lint.Command(),
		},
		Before: func(ctx context.Context, c *cli.Command) (context.Context, error) {
			c.Root().Metadata[app.LoggerMetadataKey] = getLogger(c)
			return ctx, nil
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "log-level",
				Sources:     cli.EnvVars(envPrefix + "LOG_LEVEL"),
				Value:       "info",
				Usage:       "Set the logging level (debug, info, warn, error)",
				Destination: (*string)(&loggerConfig.Level),
			},
			&cli.StringFlag{
				Name:        "log-format",
				Sources:     cli.EnvVars(envPrefix + "LOG_FORMAT"),
				Value:       "text",
				Usage:       "Set the log output format (text, json)",
				Destination: (*string)(&loggerConfig.Format),
			},
		},
	}

	sort.Sort(cli.FlagsByName(cliApp.Flags))

	return cliApp
}
