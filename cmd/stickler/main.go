// Command stickler is the gomatic lint runner: it executes the yze suite and
// golangci-lint to completion, normalizes their findings, writes them to stderr
// in the chosen format, and exits non-zero if any finding or tool error occurred.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	errs "github.com/gomatic/go-error"
	"github.com/urfave/cli/v3"

	"github.com/gomatic/stickler"
)

// errFailed is returned by the action to drive a non-zero exit when the lint pass
// failed (any finding or runner error).
const errFailed errs.Const = "lint failures found"

// appName is the CLI name.
const appName = "stickler"

// version is the application version, exposed via --version. It defaults to "dev"
// and is overwritten at build time via ldflags: -X main.version={{.Version}}
// (see .goreleaser.yml).
var version = "dev"

// defaultTimeout bounds an entire lint pass so a wedged linter cannot hang the run
// forever; it is overridable with --timeout.
const defaultTimeout = 5 * time.Minute

// Indirected dependencies, so tests supply fake runners and config instead of
// spawning real subprocesses or reading real files.
var (
	osExit       = os.Exit
	getenv       = os.Getenv
	userHomeDir  = os.UserHomeDir
	readFile     = os.ReadFile
	buildRunners = defaultBuildRunners
)

// defaultBuildRunners builds the configured runners over real subprocesses, giving
// each config-file runner the context it needs to merge its effective config.
func defaultBuildRunners(
	specs map[string]stickler.RunnerSpec, names []string, ctx stickler.RunnerContext,
) []stickler.Runner {
	return stickler.BuildRunners(stickler.ExecCommand, specs, names, ctx)
}

func main() { osExit(run(os.Args)) }

// run builds and executes the CLI, returning the process exit code.
func run(args []string) int {
	if err := createApp().Run(context.Background(), args); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, appName+":", err)
		return 1
	}
	return 0
}

// createApp constructs the stickler CLI. The normalized report is written to
// stdout (so machine formats pipe cleanly); the pass/fail status line is written
// to stderr by run(). The ExitErrHandler is neutralized so Run returns errors.
func createApp() *cli.Command {
	return &cli.Command{
		Name:           appName,
		Version:        version,
		Usage:          "run the gomatic lint suite and report via exit code",
		ArgsUsage:      "[root]",
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "format", Usage: "output format (human, json, github, sarif); overrides config"},
			&cli.StringFlag{Name: "root", Usage: "directory whose .stickler.yaml is loaded (default: the target)"},
			&cli.DurationFlag{
				Name:  "timeout",
				Value: defaultTimeout,
				Usage: "maximum duration for the whole lint pass",
			},
		},
		Action: action,
	}
}

// action loads configuration, runs the configured tools under an overall timeout,
// renders the result, and signals failure via errFailed.
func action(ctx context.Context, cmd *cli.Command) error {
	ctx, cancel := context.WithTimeout(ctx, cmd.Duration("timeout"))
	defer cancel()
	root := rootOf(cmd.Args().Slice())
	repoRoot := configRoot(stickler.RepoRoot(cmd.String("root")), root)
	resolved, err := configure(repoRoot)
	if err != nil {
		return err
	}
	runnerCtx := stickler.RunnerContext{BaseDir: string(repoRoot), Config: resolved.Config}
	specs := stickler.MergeSpecs(stickler.DefaultRunnerSpecs(), resolved.Define)
	result := stickler.Orchestrate(ctx, root, buildRunners(specs, resolved.Runners, runnerCtx))
	format := chooseFormat(stickler.OutputFormat(cmd.String("format")), stickler.OutputFormat(resolved.Format))
	if err := stickler.Format(cmd.Writer, format, result); err != nil {
		return err
	}
	if result.Failed(stickler.Soft(resolved.Soft)) {
		return errFailed
	}
	return nil
}

// configure loads and resolves the global and repo configuration layers.
func configure(repoRoot stickler.RepoRoot) (stickler.Resolved, error) {
	home, _ := userHomeDir()
	layers, err := stickler.LoadLayers(readFile, stickler.ConfigPaths(getenv, stickler.HomeDir(home), repoRoot)...)
	if err != nil {
		return stickler.Resolved{}, err
	}
	return stickler.Resolve(layers...), nil
}

// configRoot is the directory whose .stickler.yaml applies: the explicit --root,
// else the current directory (a package pattern is not a config directory).
func configRoot(flag stickler.RepoRoot, target stickler.Root) stickler.RepoRoot {
	if flag != "" {
		return flag
	}
	if target == "./..." {
		return "."
	}
	return stickler.RepoRoot(target)
}

// chooseFormat applies the precedence flag > config > human.
func chooseFormat(flag, configured stickler.OutputFormat) stickler.OutputFormat {
	if flag != "" {
		return flag
	}
	if configured != "" {
		return configured
	}
	return stickler.OutputHuman
}

// rootOf defaults to the whole module when no root is named.
func rootOf(args []string) stickler.Root {
	if len(args) == 0 {
		return "./..."
	}
	return stickler.Root(args[0])
}
