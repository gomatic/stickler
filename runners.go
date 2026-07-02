package stickler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"slices"
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
// stickler-json, no config file) and golangci-lint (adapted JSON, config merged
// from the repo's .golangci.yaml). A .stickler.yaml `define:` block overrides or
// extends this map without touching Go.
func DefaultRunnerSpecs() map[string]RunnerSpec {
	return map[string]RunnerSpec{
		toolYze: {
			Name:    toolYze,
			Command: []string{toolYze},
			Args:    []string{"--format", string(ParserSticklerJSON), "--", placeholderRoot},
			Format:  ParserSticklerJSON,
		},
		toolGolangci: {
			Name:    toolGolangci,
			Command: []string{toolGolangci, golangciRunVerb},
			Args:    []string{golangciJSONFlag, placeholderConfig, "--", placeholderRoot},
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
		if runner, ok := newSpecRunner(command, specs[name], nameParam(name), ctx); ok {
			runners = append(runners, runner)
		}
	}
	return runners
}

// nameParam names the name parameter of newSpecRunner; rename it to the real domain concept.
type nameParam string

// newSpecRunner builds a generic runner for one spec, returning false when the spec
// is undefined (empty name) or names a parser with no registered handler.
func newSpecRunner(command Command, spec RunnerSpec, name nameParam, ctx RunnerContext) (Runner, bool) {
	parser, ok := defaultParsers[spec.Format]
	if spec.Name == "" || !ok {
		return nil, false
	}
	return specRunner{spec: spec, command: command, parser: parser, merger: specMerger(spec, ctx, name)}, true
}

// specMerger builds the generic config merger for a spec that takes a config file,
// or the zero merger (no-op Args) when the spec carries no ConfigSpec.
func specMerger(spec RunnerSpec, ctx RunnerContext, name nameParam) ConfigMerger {
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
		args = append(args, substituteArg(rawParam(raw), root, configArgs)...)
	}
	return RunnerName(r.spec.Command[0]), args
}

// rawParam names the raw parameter of substituteArg; rename it to the real domain concept.
type rawParam string

// substituteArg expands one templated argument: `{config}` becomes the merged
// config argument(s) (dropped when there are none), and `{root}` is replaced inline.
func substituteArg(raw rawParam, root Root, configArgs []Arg) []Arg {
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
// set (and thus output) does not depend on map iteration order.
func sortedKeys(specs map[string]RunnerSpec) []string {
	names := make([]string, 0, len(specs))
	for name := range specs {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// parseSticklerJSON reads yze's native stickler-json Diagnostic report.
func parseSticklerJSON(out []byte) ([]goyze.Diagnostic, error) {
	report, err := goyze.UnmarshalReport(out)
	if err != nil {
		return nil, err
	}
	return report.Diagnostics, nil
}

// parseGolangciJSON decodes golangci-lint's JSON, adapting its issues to
// diagnostics and surfacing a top-level run error (e.g. invalid config) as a
// fatal parser error.
func parseGolangciJSON(out []byte) ([]goyze.Diagnostic, error) {
	parsed, err := decodeGolangci(out)
	if err != nil {
		return nil, err
	}
	if parsed.Report.Error != "" {
		return nil, ErrRunnerFailed.With(nil, "report", parsed.Report.Error)
	}
	return adaptIssues(parsed.Issues), nil
}

// decodeGolangci reads the first JSON value from golangci-lint's stdout, tolerating
// the human-readable summary footer v2 appends after it. Empty stdout (io.EOF) is a
// valid zero-issue result, not a parse error — whether it is clean or a tool
// failure is decided by the exit status the caller already holds.
func decodeGolangci(out []byte) (golangciOutput, error) {
	var parsed golangciOutput
	err := json.NewDecoder(bytes.NewReader(out)).Decode(&parsed)
	if errors.Is(err, io.EOF) {
		return golangciOutput{}, nil
	}
	if err != nil {
		return golangciOutput{}, err
	}
	return parsed, nil
}

// golangciOutput is the subset of golangci-lint's JSON report stickler consumes.
type golangciOutput struct {
	Report golangciReport  `json:"Report"`
	Issues []golangciIssue `json:"Issues"`
}

// golangciReport carries golangci-lint's top-level run status; a non-empty Error
// means the run itself failed (e.g. invalid configuration), distinct from findings.
type golangciReport struct {
	Error string `json:"Error"`
}

type golangciIssue struct {
	FromLinter string      `json:"FromLinter"`
	Text       string      `json:"Text"`
	Severity   string      `json:"Severity"`
	Pos        golangciPos `json:"Pos"`
}

type golangciPos struct {
	Filename string `json:"Filename"`
	Line     int    `json:"Line"`
	Column   int    `json:"Column"`
}

// adaptIssues maps golangci-lint issues into normalized diagnostics.
func adaptIssues(issues []golangciIssue) []goyze.Diagnostic {
	diags := make([]goyze.Diagnostic, 0, len(issues))
	for _, issue := range issues {
		diags = append(diags, goyze.Diagnostic{
			Tool:     toolGolangci,
			Rule:     issue.FromLinter,
			Path:     issue.Pos.Filename,
			Line:     issue.Pos.Line,
			Col:      issue.Pos.Column,
			Severity: severityOf(golangciSeverity(issue.Severity)),
			Message:  issue.Text,
		})
	}
	return diags
}

// golangciSeverity is golangci-lint's per-issue severity string, mapped onto the
// normalized goyze.Severity.
type golangciSeverity string

// severityOf maps a golangci-lint severity string to the normalized severity.
func severityOf(severity golangciSeverity) goyze.Severity {
	switch severity {
	case levelWarning:
		return goyze.SeverityWarning
	case "info":
		return goyze.SeverityInfo
	default:
		return goyze.SeverityError
	}
}

// firstError returns primary when set, otherwise secondary.
func firstError(primary, secondary error) error {
	if primary != nil {
		return primary
	}
	return secondary
}
