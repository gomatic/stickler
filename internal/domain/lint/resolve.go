package lint

// What a pass is CONFIGURED from, kept apart from what a pass IS: which root
// it targets, which .stickler.yaml applies, which checks get assembled, and
// which format the report takes. Every one of these is a precedence decision,
// and precedence decisions are the ones worth reading in one place.

import (
	"io"
	"os"

	"github.com/gomatic/stickler/internal/checks"
	"github.com/gomatic/stickler/internal/config"
	"github.com/gomatic/stickler/internal/domain"
	"github.com/gomatic/stickler/internal/report"
	"github.com/gomatic/stickler/internal/suite"
)

// wholeModule is the package pattern that means "every package below here".
const wholeModule suite.Root = "./..."

// build assembles the runners this repository's configuration selects.
func build(resolved config.Resolved, repoRoot config.RepoRoot) ([]suite.Runner, error) {
	return checks.Build(execCommand, resolved, repoRoot)
}

// configure loads and resolves the global and repo configuration layers.
func configure(repoRoot config.RepoRoot) (config.Resolved, error) {
	home, _ := userHomeDir()
	return checks.Resolve(readFile, getenv, config.HomeDir(home), repoRoot)
}

// writer defaults an unset destination to standard output, so a caller that has
// one injects it and the CLI does not have to.
func writer(out io.Writer) io.Writer {
	if out == nil {
		return os.Stdout
	}
	return out
}

// configRoot is the directory whose .stickler.yaml applies: the explicit
// --root, else the current directory (a package pattern is not a config
// directory).
func configRoot(flag config.RepoRoot, target suite.Root) config.RepoRoot {
	if flag != "" {
		return flag
	}
	if target == wholeModule {
		return "."
	}
	return config.RepoRoot(target)
}

// format applies the precedence flag > config > human.
func format(flag, configured report.OutputFormat) report.OutputFormat {
	if flag != "" {
		return flag
	}
	if configured != "" {
		return configured
	}
	return report.OutputHuman
}

// rootOf defaults to the whole module when no root is named.
func rootOf(args []domain.Argument) suite.Root {
	if len(args) == 0 {
		return wholeModule
	}
	return suite.Root(args[0])
}
