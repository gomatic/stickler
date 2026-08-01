package stickler

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"

	errs "github.com/gomatic/go-error"
	goyze "github.com/gomatic/go-yze"
)

// Runner failures.
const (
	// ErrRunnerFailed reports that a runner could not be run, its config could not
	// be built, or its output could not be parsed — distinct from a clean pass that
	// merely reported findings.
	ErrRunnerFailed errs.Const = "runner failed"
	// ErrExec reports that a subprocess could not be started or exited non-zero; it
	// carries the captured stderr so the failure's real reason is reported.
	ErrExec errs.Const = "command execution failed"
)

// RunnerName identifies a runner's binary (the first word of its command).
type RunnerName string

// Arg is one command-line argument passed to a runner's binary.
type Arg string

// Command runs an external tool and returns its stdout. A non-nil error includes
// a non-zero exit; callers that can still parse the stdout (linters exit non-zero
// when they report findings) treat the output as authoritative.
type Command func(ctx context.Context, name RunnerName, args ...Arg) ([]byte, error)

// ExecCommand is the default Command, executing a real subprocess. On failure the
// returned error wraps ErrExec with the captured stderr so the underlying reason
// (config error, panic, load failure) reaches the caller's message.
func ExecCommand(ctx context.Context, name RunnerName, args ...Arg) ([]byte, error) {
	bin, list := string(name), stringArgs(args)
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, list...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, ErrExec.With(err, "stderr", strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// stringArgs converts typed arguments to the plain strings exec expects.
func stringArgs(args []Arg) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = string(a)
	}
	return out
}

// Argument placeholders substituted in a RunnerSpec's Args at run time.
const (
	placeholderRoot   = "{root}"
	placeholderConfig = "{config}"
)

// ParserName selects the output parser a runner's stdout is read with.
type ParserName string

// Built-in parser names.
const (
	ParserSticklerJSON ParserName = "stickler-json"
	ParserGolangciJSON ParserName = "golangci-json"
)

// Built-in tool literals (named so the spec table and adapters share one source).
const (
	toolYze          = "yze"
	toolGolangci     = "golangci-lint"
	golangciRunVerb  = "run"
	golangciJSONFlag = "--output.json.path=stdout"
	golangciCfgFlag  = "--config={path}"
	golangciBaseYAML = ".golangci.yaml"
	golangciBaseYML  = ".golangci.yml"
	yzeCfgFlag       = "--config={path}"
	yzeBaseYAML      = ".yze.yaml"
	yzeBaseYML       = ".yze.yml"
	// golangciParallelFlag disables golangci-lint's start-up file lock. That lock
	// is global (a fixed path, not under GOLANGCI_LINT_CACHE), so N concurrent
	// runs — a `git repo list | xargs -P<N> make check` fleet sweep — otherwise
	// have N-1 die with "parallel golangci-lint is running" (exit 3). The cache
	// is content-addressed and safe for concurrent runners, which is exactly why
	// golangci ships this opt-out.
	golangciParallelFlag = "--allow-parallel-runners"
)

// ConfigSpec declares, as data, how a tool takes its configuration file: the base
// config filename candidates (first found wins) the stickler overlays are merged
// onto, and the flag template (`{path}` substituted) that passes the effective
// config. It carries no tool-specific behavior — the merge is generic.
type ConfigSpec struct {
	Flag string
	Base []string
}

// RunnerSpec is the declarative definition of a tool stickler runs: its command,
// its argument template (with `{root}`/`{config}` placeholders), the parser its
// stdout is read with, and optionally how its config file is wired. A tool is data
// here, not code — adding one is configuration, not a recompile.
// Fields are ordered for struct-field alignment (slices last); the YAML schema is
// unaffected since decoding is by tag.
type RunnerSpec struct {
	Name    string      `yaml:"name"`
	Format  ParserName  `yaml:"format"`
	Config  *ConfigSpec `yaml:"config"`
	Command []string    `yaml:"command"`
	Args    []string    `yaml:"args"`
}

// Parser turns a tool's stdout into normalized diagnostics. A non-nil error means
// the tool self-reported a fatal problem (bad config, internal error). This is the
// only per-tool code in the runner layer.
type Parser func(out []byte) ([]goyze.Diagnostic, error)

// defaultParsers is the registry of output parsers selected by a spec's Format.
var defaultParsers = map[ParserName]Parser{
	ParserSticklerJSON: parseSticklerJSON,
	ParserGolangciJSON: parseGolangciJSON,
}

// DefaultRunnerSpecs is the built-in tool set as pure data: yze (native
// stickler-json, config merged from the repo's .yze.yaml plus the resolved
// `analyzers:` settings) and golangci-lint (adapted JSON, config merged from
// the repo's .golangci.yaml). A .stickler.yaml `define:` block overrides or
// extends this map without touching Go.
func DefaultRunnerSpecs() map[string]RunnerSpec {
	return map[string]RunnerSpec{
		toolYze: {
			Name:    toolYze,
			Command: []string{toolYze},
			Args: []string{
				"--format", string(ParserSticklerJSON), placeholderConfig, "--", placeholderRoot,
			},
			Format: ParserSticklerJSON,
			Config: &ConfigSpec{Base: []string{yzeBaseYAML, yzeBaseYML}, Flag: yzeCfgFlag},
		},
		toolGolangci: {
			Name:    toolGolangci,
			Command: []string{toolGolangci, golangciRunVerb},
			Args:    []string{golangciJSONFlag, golangciParallelFlag, placeholderConfig, "--", placeholderRoot},
			Format:  ParserGolangciJSON,
			Config:  &ConfigSpec{Base: []string{golangciBaseYAML, golangciBaseYML}, Flag: golangciCfgFlag},
		},
	}
}

// RunnerContext carries what config-file runners need to build their effective
// configuration: the repo directory holding the base config files and the resolved
// per-tool overlays keyed by runner name.
type RunnerContext struct {
	Config  map[string][]Overlay
	BaseDir string
}

// BuildRunners resolves the named runners against the spec registry (built-in
// defaults overlaid with any config-defined specs) into generic runners. Names
// default to every defined spec. An unknown name, or a spec naming an unknown
// parser, is skipped.
func BuildRunners(command Command, specs map[string]RunnerSpec, names []string, ctx RunnerContext) []Runner {
	if len(names) == 0 {
		names = sortedKeys(specs)
	}
	runners := make([]Runner, 0, len(names))
	for _, name := range names {
		if runner, ok := newSpecRunner(command, specs[name], specName(name), ctx); ok {
			runners = append(runners, runner)
		}
	}
	return runners
}

// specName is a runner's key in the spec registry — the name a .stickler.yaml
// selects it by and its config overlays are keyed by (distinct from RunnerName,
// the binary it executes).
type specName string

// newSpecRunner builds a generic runner for one spec, returning false when the spec
// is undefined (empty name) or names a parser with no registered handler.
func newSpecRunner(command Command, spec RunnerSpec, name specName, ctx RunnerContext) (Runner, bool) {
	parser, ok := defaultParsers[spec.Format]
	if spec.Name == "" || !ok {
		return nil, false
	}
	return specRunner{spec: spec, command: command, parser: parser, merger: specMerger(spec, ctx, name)}, true
}

// specMerger builds the generic config merger for a spec that takes a config file,
// or the zero merger (no-op Args) when the spec carries no ConfigSpec.
func specMerger(spec RunnerSpec, ctx RunnerContext, name specName) ConfigMerger {
	if spec.Config == nil {
		return ConfigMerger{}
	}
	return ConfigMerger{
		BaseNames: spec.Config.Base,
		Flag:      spec.Config.Flag,
		Overlays:  ctx.Config[string(name)],
		BaseDir:   ctx.BaseDir,
		Read:      os.ReadFile,
		Temp:      OSTempWriter,
	}
}

// specRunner is the single, tool-agnostic Runner: it merges the effective config,
// substitutes the argument placeholders, executes the command, and reads the
// output through the spec's parser, applying the uniform findings-vs-failure rule.
type specRunner struct {
	command Command
	parser  Parser
	merger  ConfigMerger
	spec    RunnerSpec
}

func (r specRunner) Name() string { return r.spec.Name }

// Run executes the spec. A non-zero exit accompanied by parsed findings is the
// expected "findings reported" path; a parser error, a config-build failure, or a
// non-zero exit with zero findings is a genuine tool failure surfaced as
// ErrRunnerFailed rather than masquerading as a clean pass.
func (r specRunner) Run(ctx context.Context, root Root) ([]goyze.Diagnostic, error) {
	configArgs, cleanup, err := r.merger.Args()
	if err != nil {
		return nil, ErrRunnerFailed.With(err, "runner", r.spec.Name)
	}
	defer cleanup()
	name, args := r.argv(root, configArgs)
	out, execErr := r.command(ctx, name, args...)
	diags, parseErr := r.parser(out)
	if parseErr != nil {
		return nil, ErrRunnerFailed.With(firstError(execErr, parseErr), "runner", r.spec.Name)
	}
	if execErr != nil && len(diags) == 0 {
		return nil, ErrRunnerFailed.With(execErr, "runner", r.spec.Name)
	}
	return diags, nil
}

// argv builds the executed command: the binary (Command[0]), the fixed command
// verbs (Command[1:]), then the Args with `{config}` expanded to the merged config
// argument(s) and `{root}` substituted with the target.
func (r specRunner) argv(root Root, configArgs []Arg) (RunnerName, []Arg) {
	args := toArgs(r.spec.Command[1:])
	for _, raw := range r.spec.Args {
		args = append(args, substituteArg(argTemplate(raw), root, configArgs)...)
	}
	return RunnerName(r.spec.Command[0]), args
}

// argTemplate is one entry of a RunnerSpec's Args: an argument that may carry
// the `{root}`/`{config}` placeholders, expanded to Args at run time.
type argTemplate string

// substituteArg expands one templated argument: `{config}` becomes the merged
// config argument(s) (dropped when there are none), and `{root}` is replaced inline.
func substituteArg(raw argTemplate, root Root, configArgs []Arg) []Arg {
	if string(raw) == placeholderConfig {
		return configArgs
	}
	return []Arg{Arg(strings.ReplaceAll(string(raw), placeholderRoot, string(root)))}
}

// toArgs converts a string slice to typed Args.
func toArgs(in []string) []Arg {
	out := make([]Arg, len(in))
	for i, s := range in {
		out[i] = Arg(s)
	}
	return out
}

// sortedKeys returns the spec names in deterministic order, so the default runner
