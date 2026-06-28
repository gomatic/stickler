package stickler

import (
	"context"
	"encoding/json"
	"os/exec"

	errs "github.com/gomatic/go-error"
	goyze "github.com/gomatic/go-yze"
)

// Runner failures.
const (
	// ErrYzeFailed reports that the yze aggregator could not be run or parsed.
	ErrYzeFailed errs.Const = "yze runner failed"
	// ErrGolangciFailed reports that golangci-lint could not be run or parsed.
	ErrGolangciFailed errs.Const = "golangci-lint runner failed"
)

// Command runs an external tool and returns its stdout. A non-nil error includes
// a non-zero exit; callers that can still parse the stdout (linters exit non-zero
// when they report findings) treat the output as authoritative.
type Command func(ctx context.Context, name string, args ...string) ([]byte, error)

// ExecCommand is the default Command, executing a real subprocess.
func ExecCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
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

func (yzeRunner) Name() string { return "yze" }

func (y yzeRunner) Run(ctx context.Context, root string) ([]goyze.Diagnostic, error) {
	out, execErr := y.command(ctx, "yze", "--format", "stickler-json", root)
	report, parseErr := goyze.UnmarshalReport(out)
	if parseErr != nil {
		return nil, ErrYzeFailed.With(firstError(execErr, parseErr))
	}
	return report.Diagnostics, nil
}

// golangciRunner runs golangci-lint and adapts its JSON issues to diagnostics.
type golangciRunner struct {
	command Command
}

// NewGolangciRunner builds a Runner that invokes golangci-lint.
func NewGolangciRunner(command Command) Runner {
	return golangciRunner{command: command}
}

func (golangciRunner) Name() string { return "golangci-lint" }

func (g golangciRunner) Run(ctx context.Context, root string) ([]goyze.Diagnostic, error) {
	out, execErr := g.command(ctx, "golangci-lint", "run", "--output.json.path", "stdout", root)
	var parsed golangciOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, ErrGolangciFailed.With(firstError(execErr, err))
	}
	return adaptIssues(parsed.Issues), nil
}

// golangciOutput is the subset of golangci-lint's JSON report stickler consumes.
type golangciOutput struct {
	Issues []golangciIssue `json:"Issues"`
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
			Tool:     "golangci-lint",
			Rule:     issue.FromLinter,
			Path:     issue.Pos.Filename,
			Line:     issue.Pos.Line,
			Col:      issue.Pos.Column,
			Severity: severityOf(issue.Severity),
			Message:  issue.Text,
		})
	}
	return diags
}

// severityOf maps a golangci-lint severity string to the normalized severity.
func severityOf(severity string) goyze.Severity {
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
