package stickler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"

	errs "github.com/gomatic/go-error"
	goyze "github.com/gomatic/go-yze"
)

// Runner failures.
const (
	// ErrYzeFailed reports that the yze aggregator could not be run or parsed.
	ErrYzeFailed errs.Const = "yze runner failed"
	// ErrGolangciFailed reports that golangci-lint could not be run or parsed.
	ErrGolangciFailed errs.Const = "golangci-lint runner failed"
	// ErrExec reports that a subprocess could not be started or exited non-zero;
	// it carries the captured stderr so the failure's real reason is reported.
	ErrExec errs.Const = "command execution failed"
)

// RunnerName identifies a runner in configuration, selection, and as the binary
// name stickler executes.
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

// Runner names, used in configuration and selection.
const (
	RunnerYze      RunnerName = "yze"
	RunnerGolangci RunnerName = "golangci-lint"
)

// golangci-lint invocation literals.
const (
	golangciRunVerb    = "run"
	golangciConfigFlag = "--config="
	golangciConfigYAML = ".golangci.yaml"
	golangciConfigYML  = ".golangci.yml"
)

// RunnerContext carries what config-file runners need to build their effective
// configuration: the repo directory holding the base config files and the resolved
// per-tool overlays keyed by runner name.
type RunnerContext struct {
	Config  map[string][]Overlay
	BaseDir string
}

// BuildRunners constructs the runners named in the resolved configuration,
// defaulting to the full set when none are configured. A config-file runner is
// given its merger so it runs against the effective (base + overlay) config.
// Unknown runner names are ignored.
func BuildRunners(command Command, names []string, ctx RunnerContext) []Runner {
	if len(names) == 0 {
		names = []string{string(RunnerYze), string(RunnerGolangci)}
	}
	runners := make([]Runner, 0, len(names))
	for _, name := range names {
		if runner := newRunner(command, RunnerName(name), ctx); runner != nil {
			runners = append(runners, runner)
		}
	}
	return runners
}

// newRunner maps a runner name to its Runner, or nil when the name is unknown.
func newRunner(command Command, name RunnerName, ctx RunnerContext) Runner {
	switch name {
	case RunnerYze:
		return NewYzeRunner(command)
	case RunnerGolangci:
		return NewGolangciRunner(command, GolangciMerger(ctx.Config[string(RunnerGolangci)], ctx.BaseDir))
	default:
		return nil
	}
}

// GolangciMerger builds the generic config merger for golangci-lint — the only
// golangci-specific knowledge is its base config filenames and --config flag —
// wired to the production I/O seams.
func GolangciMerger(overlays []Overlay, baseDir string) ConfigMerger {
	return ConfigMerger{
		BaseNames: []string{golangciConfigYAML, golangciConfigYML},
		Flag:      golangciConfigFlag,
		Overlays:  overlays,
		BaseDir:   baseDir,
		Read:      os.ReadFile,
		Temp:      OSTempWriter,
	}
}

// yzeRunner runs the yze aggregator and reads its native stickler-json output —
// no adapter, since yze emits the Diagnostic schema directly.
type yzeRunner struct {
	command Command
}

// NewYzeRunner builds a Runner that invokes the yze aggregator.
func NewYzeRunner(command Command) Runner {
	return yzeRunner{command: command}
}

func (yzeRunner) Name() string { return string(RunnerYze) }

// Run executes yze. A non-zero exit with parsed findings is the expected "findings
// reported" path; a non-zero exit with zero findings means the tool itself failed
// (config/load/typecheck error or panic) and surfaces as ErrYzeFailed rather than
// masquerading as a clean pass.
func (y yzeRunner) Run(ctx context.Context, root Root) ([]goyze.Diagnostic, error) {
	out, execErr := y.command(ctx, RunnerYze, "--format", "stickler-json", "--", Arg(root))
	report, parseErr := goyze.UnmarshalReport(out)
	if parseErr != nil {
		return nil, ErrYzeFailed.With(firstError(execErr, parseErr))
	}
	if execErr != nil && len(report.Diagnostics) == 0 {
		return nil, ErrYzeFailed.With(execErr)
	}
	return report.Diagnostics, nil
}

// golangciRunner runs golangci-lint against the effective (base + overlay) config
// and adapts its JSON issues to diagnostics. The merge itself is generic; this
// runner only supplies golangci's adapter and (via the merger) its config spec.
type golangciRunner struct {
	command Command
	merger  ConfigMerger
}

// NewGolangciRunner builds a Runner that invokes golangci-lint, merging the repo's
// base .golangci.yaml with the configured overlays at run time via the merger.
func NewGolangciRunner(command Command, merger ConfigMerger) Runner {
	return golangciRunner{command: command, merger: merger}
}

func (golangciRunner) Name() string { return string(RunnerGolangci) }

// Run executes golangci-lint against the effective config. As with yze, a non-zero
// exit accompanied by parsed issues is the findings path; a non-zero exit (or a
// top-level Report.Error) with zero issues is a genuine tool failure surfaced as
// ErrGolangciFailed. A failure to build the effective config is also a tool
// failure, since the lint pass could not run.
func (g golangciRunner) Run(ctx context.Context, root Root) ([]goyze.Diagnostic, error) {
	configArgs, cleanup, err := g.merger.Args()
	if err != nil {
		return nil, ErrGolangciFailed.With(err)
	}
	defer cleanup()
	out, execErr := g.command(ctx, RunnerGolangci, golangciArgs(configArgs, root)...)
	parsed, parseErr := decodeGolangci(out)
	if parseErr != nil {
		return nil, ErrGolangciFailed.With(firstError(execErr, parseErr))
	}
	diags := adaptIssues(parsed.Issues)
	if parsed.Report.Error != "" {
		return nil, ErrGolangciFailed.With(execErr, "report", parsed.Report.Error)
	}
	if execErr != nil && len(diags) == 0 {
		return nil, ErrGolangciFailed.With(execErr)
	}
	return diags, nil
}

// golangciArgs assembles the golangci-lint argv: the run verb and JSON output,
// then any --config flag for the effective config, then the root after "--".
func golangciArgs(configArgs []Arg, root Root) []Arg {
	args := []Arg{golangciRunVerb, "--output.json.path=stdout"}
	args = append(args, configArgs...)
	return append(args, "--", Arg(root))
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
			Tool:     string(RunnerGolangci),
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
	case "warning":
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
