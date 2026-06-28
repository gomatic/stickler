// Command stickler is the gomatic lint runner: it executes the yze suite and
// golangci-lint to completion, normalizes their findings, writes them to stderr
// in the chosen format, and exits non-zero if any finding or tool error occurred.
package main

import (
	"context"
	"fmt"
	"os"

	errs "github.com/gomatic/go-error"
	"github.com/gomatic/stickler"
	"github.com/urfave/cli/v3"
)

// errFailed is returned by the action to drive a non-zero exit when the lint pass
// failed (any finding or runner error).
const errFailed errs.Const = "lint failures found"

// runnersFor is indirected so tests can supply fake runners instead of spawning
// real subprocesses.
var runnersFor = defaultRunners

// osExit is indirected so tests can observe the process exit code.
var osExit = os.Exit

// defaultRunners is the zero-config tool set: the yze suite plus golangci-lint.
func defaultRunners() []stickler.Runner {
	return []stickler.Runner{
		stickler.NewYzeRunner(stickler.ExecCommand),
		stickler.NewGolangciRunner(stickler.ExecCommand),
	}
}

func main() { osExit(run(os.Args)) }

// run builds and executes the CLI, returning the process exit code.
func run(args []string) int {
	if err := createApp().Run(context.Background(), args); err != nil {
		fmt.Fprintln(os.Stderr, "stickler:", err)
		return 1
	}
	return 0
}

// createApp constructs the stickler CLI. The normalized report is written to
// stdout (so machine formats pipe cleanly); the pass/fail status line is written
// to stderr by run(). The ExitErrHandler is neutralized so Run returns errors.
func createApp() *cli.Command {
	return &cli.Command{
		Name:           "stickler",
		Usage:          "run the gomatic lint suite and report via exit code",
		ArgsUsage:      "[root]",
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "format", Value: string(stickler.OutputHuman), Usage: "output format (human, json, github)"},
		},
		Action: action,
	}
}

// action runs every tool, renders the result, and signals failure via errFailed.
func action(ctx context.Context, cmd *cli.Command) error {
	result := stickler.Orchestrate(ctx, rootOf(cmd.Args().Slice()), runnersFor())
	if err := stickler.Format(cmd.Writer, stickler.OutputFormat(cmd.String("format")), result); err != nil {
		return err
	}
	if result.Failed() {
		return errFailed
	}
	return nil
}

// rootOf defaults to the whole module when no root is named.
func rootOf(args []string) string {
	if len(args) == 0 {
		return "./..."
	}
	return args[0]
}
