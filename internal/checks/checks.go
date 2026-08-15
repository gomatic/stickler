// Package checks assembles every check stickler can run: the tools declared as
// data, plus the native checks stickler implements itself.
//
// It is the one place the native checks are named. Both domain verbs — the
// lint pass and the instructions document — must run over exactly the same set,
// or a repository could be checked against rules the instructions never
// mention. The natives are INJECTED into the runner registry here rather than
// registered inside internal/runner, which is what keeps that package from
// depending on the check packages that depend on it.
package checks

import (
	"github.com/gomatic/stickler/internal/check/binaries"
	"github.com/gomatic/stickler/internal/check/clicommands"
	"github.com/gomatic/stickler/internal/check/clilayout"
	"github.com/gomatic/stickler/internal/config"
	"github.com/gomatic/stickler/internal/runner"
	"github.com/gomatic/stickler/internal/suite"
)

// Native is every check stickler implements itself — the ones whose question is
// a whole-repo fact no single-package analyzer pass can see.
func Native() map[string]suite.Runner {
	return map[string]suite.Runner{
		binaries.Name:    binaries.Runner{},
		clilayout.Name:   clilayout.Runner{},
		clicommands.Name: clicommands.Runner{},
	}
}

// Build assembles the runners this repository's configuration selects.
func Build(command runner.Command, resolved config.Resolved, repoRoot config.RepoRoot) ([]suite.Runner, error) {
	registry := runner.Registry{
		Specs:  config.MergeSpecs(config.DefaultRunnerSpecs(), resolved.Define),
		Native: Native(),
	}
	ctx := runner.Context{BaseDir: string(repoRoot), Config: resolved.Config}
	return runner.Build(command, registry, resolved.Runners, ctx)
}

// Resolve loads and folds the global and repository configuration layers. The
// filesystem seams are parameters so a caller can exercise a pass without a
// real home directory or real config files.
func Resolve(
	read config.FileReader,
	getenv config.Getenv,
	home config.HomeDir,
	repoRoot config.RepoRoot,
) (config.Resolved, error) {
	layers, err := config.LoadLayers(read, config.Layers(getenv, home, repoRoot)...)
	if err != nil {
		return config.Resolved{}, err
	}
	return config.Resolve(layers...), nil
}
