// Package lint binds flags for "stickler lint".
package lint

import (
	"time"

	app "github.com/gomatic/go-app"
	"github.com/urfave/cli/v3"

	domain "github.com/gomatic/stickler/internal/domain/lint"
)

const (
	Name        = `lint`
	usage       = `Run the gomatic lint suite and report via exit code.`
	argUsage    = `[root]`
	description = `Run every configured check over a root and report the findings.

The checks are DATA: the tools stickler runs, how each is invoked, and how its
output is parsed are declared in .stickler.yaml, layered over the global
configuration. The native checks stickler implements itself — the ones whose
question is a whole-repo fact no single-package analyzer can see — run
alongside them.

The report goes to stdout so machine formats pipe cleanly. A finding that is
neither softened nor from a declared probe, a soft rule grown past its committed
baseline, or any tool failure exits non-zero. A probe reports at any count and
never gates.

Examples:
  stickler
  stickler ./internal/...
  stickler --format=github`
)

const (
	formatFlag  = "format"
	rootFlag    = "root"
	timeoutFlag = "timeout"
)

var (
	cfg       domain.Config
	runAction = domain.Run
)

// Command returns the CLI command definition.
func Command() *cli.Command {
	return &cli.Command{
		Name:        Name,
		Usage:       usage,
		ArgsUsage:   argUsage,
		Description: description,
		// Interactive, not Default: this command OWNS stdout. Its formats are
		// wire formats a CI runner or a code scanner parses, so a generic
		// result encoding written to the same stream would corrupt them. The
		// report is written by the runner; what remains to say is logged to
		// stderr.
		Action: app.Interactive(&cfg, runAction),
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        formatFlag,
				Sources:     cli.EnvVars("STICKLER_FORMAT"),
				Value:       "",
				Usage:       "Output format (human, json, github, sarif); overrides config",
				Destination: (*string)(&cfg.Format),
			},
			&cli.StringFlag{
				Name:        rootFlag,
				Sources:     cli.EnvVars("STICKLER_ROOT"),
				Value:       "",
				Usage:       "Directory whose .stickler.yaml is loaded (default: the target)",
				Destination: (*string)(&cfg.Root),
			},
			&cli.DurationFlag{
				Name:        timeoutFlag,
				Sources:     cli.EnvVars("STICKLER_TIMEOUT"),
				Value:       time.Duration(domain.DefaultTimeout),
				Usage:       "Maximum duration for the whole lint pass",
				Destination: (*time.Duration)(&cfg.Timeout),
			},
		},
	}
}
