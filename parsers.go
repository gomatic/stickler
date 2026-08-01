package stickler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"

	goyze "github.com/gomatic/go-yze"
)

// Turning each tool's own output into diagnostics. One parser per output shape,
// kept apart from the runner machinery because a parser changes when an
// upstream tool changes its JSON, and the runner machinery does not.

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
