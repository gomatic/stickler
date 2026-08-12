package runner

import (
	"context"
	"slices"
	"strings"

	goyze "github.com/gomatic/go-yze"

	"github.com/gomatic/stickler/internal/config"
	"github.com/gomatic/stickler/internal/constants"
	"github.com/gomatic/stickler/internal/suite"
)

// Registry is everything a selected name may resolve to: the declared tool
// specs, and the native checks stickler implements itself.
//
// Native is INJECTED rather than looked up here. A registry of native checks
// inside this package would make it depend on the check packages, which depend
// on it — the composition root supplies them instead, and the dependency stays
// one-way.
type Registry struct {
	Specs  map[string]config.RunnerSpec
	Native map[string]suite.Runner
}

// Context carries what config-file runners need to build their effective
// configuration: the repo directory holding the base config files and the resolved
// per-tool overlays keyed by runner name.
type Context struct {
	Config  map[string][]config.Overlay
	BaseDir string
}

// Build resolves the named runners against the registry into generic runners.
// Names default to every defined spec plus every native check. A config-defined
// spec with a native check's name overrides it, so a repository can rewire even a
// native check without a recompile. An unknown name, or a spec naming an unknown
// parser, fails with ErrUnknownRunner.
func Build(command Command, registry Registry, names []string, ctx Context) ([]suite.Runner, error) {
	if len(names) == 0 {
		names = registry.defaultNames()
	}
	runners := make([]suite.Runner, 0, len(names))
	for _, name := range names {
		runner, ok := registry.resolve(command, specName(name), ctx)
		if !ok {
			return nil, constants.ErrUnknownRunner.With(nil, "name", name)
		}
		runners = append(runners, runner)
	}
	return runners, nil
}

// specName is a runner's key in the registry — the name a .stickler.yaml
// selects it by and its config overlays are keyed by (distinct from Name, the
// binary it executes).
type specName string

// defaultNames is every defined spec plus every native check, in one
// deterministic order.
func (r Registry) defaultNames() []string {
	names := make([]string, 0, len(r.Specs)+len(r.Native))
	for name := range r.Specs {
		names = append(names, name)
	}
	for name := range r.Native {
		if _, defined := r.Specs[name]; !defined {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// resolve resolves one name: a defined spec wins — so a repository can rewire
// even a native check from configuration — then a native check.
func (r Registry) resolve(command Command, name specName, ctx Context) (suite.Runner, bool) {
	if runner, ok := newSpecRunner(command, r.Specs[string(name)], name, ctx); ok {
		return runner, true
	}
	runner, ok := r.Native[string(name)]
	return runner, ok
}

// newSpecRunner builds a generic runner for one spec, returning false when the spec
// is undefined (empty name) or names a parser with no registered handler.
func newSpecRunner(command Command, spec config.RunnerSpec, name specName, ctx Context) (suite.Runner, bool) {
	parser, ok := parsers[spec.Format]
	if spec.Name == "" || !ok {
		return nil, false
	}
	return specRunner{spec: spec, command: command, parser: parser, merger: specMerger(spec, ctx, name)}, true
}

// specMerger builds the generic config merger for a spec that takes a config file,
// or the zero merger (no-op Args) when the spec carries no Spec. The
// merger's filesystem seams are left unset, so it reads and writes for real.
func specMerger(spec config.RunnerSpec, ctx Context, name specName) config.Merger {
	if spec.Config == nil {
		return config.Merger{}
	}
	return config.Merger{
		BaseNames: spec.Config.Base,
		Flag:      spec.Config.Flag,
		Overlays:  ctx.Config[string(name)],
		BaseDir:   ctx.BaseDir,
	}
}

// specRunner is the single, tool-agnostic Runner: it merges the effective config,
// substitutes the argument placeholders, executes the command, and reads the
// output through the spec's parser, applying the uniform findings-vs-failure rule.
type specRunner struct {
	command Command
	parser  Parser
	merger  config.Merger
	spec    config.RunnerSpec
}

// Name names the runner in the registry and in error reports.
func (r specRunner) Name() string { return r.spec.Name }

// Run executes the spec. A non-zero exit accompanied by parsed findings is the
// expected "findings reported" path; a parser error, a config-build failure, or a
// non-zero exit with zero findings is a genuine tool failure surfaced as
// ErrRunnerFailed rather than masquerading as a clean pass.
func (r specRunner) Run(ctx context.Context, root suite.Root) ([]goyze.Diagnostic, error) {
	configArgs, cleanup, err := r.merger.Args()
	if err != nil {
		return nil, constants.ErrRunnerFailed.With(err, "runner", r.spec.Name)
	}
	defer cleanup()
	env, envCleanup, err := resolveEnv(r.spec.Env)
	if err != nil {
		return nil, constants.ErrRunnerFailed.With(err, "runner", r.spec.Name)
	}
	defer envCleanup()
	name, args := r.argv(root, configArgs)
	out, execErr := r.command(ctx, name, env, args...)
	diags, parseErr := r.parser(out)
	if parseErr != nil {
		return nil, constants.ErrRunnerFailed.With(firstError(execErr, parseErr), "runner", r.spec.Name)
	}
	if execErr != nil && len(diags) == 0 {
		return nil, constants.ErrRunnerFailed.With(execErr, "runner", r.spec.Name)
	}
	return diags, nil
}

// argv builds the executed command: the binary (Command[0]), the fixed command
// verbs (Command[1:]), then the Args with `{config}` expanded to the merged config
// argument(s) and `{root}` substituted with the target.
func (r specRunner) argv(root suite.Root, configArgs []string) (Name, []Arg) {
	args := toArgs(r.spec.Command[1:])
	for _, raw := range r.spec.Args {
		args = append(args, substituteArg(argTemplate(raw), root, configArgs)...)
	}
	return Name(r.spec.Command[0]), args
}

// argTemplate is one entry of a RunnerSpec's Args: an argument that may carry
// the `{root}`/`{config}` placeholders, expanded to Args at run time.
type argTemplate string

// substituteArg expands one templated argument: `{config}` becomes the merged
// config argument(s) (dropped when there are none), and `{root}` is replaced inline.
func substituteArg(raw argTemplate, root suite.Root, configArgs []string) []Arg {
	if string(raw) == config.PlaceholderConfig {
		return toArgs(configArgs)
	}
	return []Arg{Arg(strings.ReplaceAll(string(raw), config.PlaceholderRoot, string(root)))}
}

// Explain runs the tool's declared instructions invocation and returns what it
// printed. A spec that declares none cannot explain itself; that is reported
// as a refusal rather than an empty section, so a tool silently contributing
// nothing to the standards an agent reads is impossible to miss.
func (r specRunner) Explain(ctx context.Context) (suite.Instructions, error) {
	if len(r.spec.Instructions) == 0 {
		return "", constants.ErrNoInstructions.With(nil, "runner", r.spec.Name)
	}
	env, cleanup, err := resolveEnv(r.spec.Env)
	if err != nil {
		return "", constants.ErrRunnerFailed.With(err, "runner", r.spec.Name)
	}
	defer cleanup()
	name := Name(r.spec.Command[0])
	args := append(toArgs(r.spec.Command[1:]), toArgs(r.spec.Instructions)...)
	out, err := r.command(ctx, name, env, args...)
	if err != nil {
		return "", constants.ErrRunnerFailed.With(err, "runner", r.spec.Name)
	}
	return suite.Instructions(out), nil
}
