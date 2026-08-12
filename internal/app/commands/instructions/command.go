// Package instructions binds flags for "stickler instructions".
package instructions

import (
	app "github.com/gomatic/go-app"
	"github.com/urfave/cli/v3"

	domain "github.com/gomatic/stickler/internal/domain/instructions"
)

const (
	Name        = `instructions`
	usage       = `Print the rules every configured check enforces.`
	argUsage    = ``
	description = `Print, as one markdown document, what every check configured for this
repository enforces — in enough detail to write conforming code without running
the gate first.

Each section is the CHECK'S OWN statement of its rules, asked of the tool that
decides. Nothing here is a copy kept inside stickler, so nothing here can drift
from the analyzer that actually fails your build. A check that cannot state its
rules is named rather than omitted: a partial standard presented as the whole
one is worse than an admitted gap.

The document reflects the checks THIS repository runs, so it changes with
.stickler.yaml.

Examples:
  stickler instructions
  stickler instructions > AGENTS.md`
)

const rootFlag = "root"

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
		// Interactive, not Default: the document IS this command's output, and
		// a generic result encoding written to the same stream would sit in
		// the middle of it.
		Action: app.Interactive(&cfg, runAction),
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        rootFlag,
				Sources:     cli.EnvVars("STICKLER_ROOT"),
				Value:       "",
				Usage:       "Directory whose .stickler.yaml selects the checks (default: the working directory)",
				Destination: (*string)(&cfg.Root),
			},
		},
	}
}
