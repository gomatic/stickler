package runner

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	goyze "github.com/gomatic/go-yze"

	"github.com/gomatic/stickler/internal/config"
	"github.com/gomatic/stickler/internal/constants"
)

// Turning each tool's own output into diagnostics. One parser per output shape,
// kept apart from the runner machinery because a parser changes when an
// upstream tool changes its JSON, and the runner machinery does not.

// Parser turns a tool's stdout into normalized diagnostics. A non-nil error means
// the tool self-reported a fatal problem (bad config, internal error). This is the
// only per-tool code in the runner layer.
type Parser func(out []byte) ([]goyze.Diagnostic, error)

// parsers is the registry of output parsers selected by a spec's Format.
var parsers = map[config.ParserName]Parser{
	config.ParserSticklerJSON: parseSticklerJSON,
	config.ParserGolangciJSON: parseGolangciJSON,
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
		return nil, constants.ErrRunnerFailed.With(nil, "report", parsed.Report.Error)
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
			Tool:     config.ToolGolangci,
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
// normalized goyze.Severity. These are golangci-lint's INPUT spellings; the
// report package's output levels are a separate vocabulary that happens to
// share two of the words.
type golangciSeverity string

// The golangci-lint severity spellings stickler recognizes.
const (
	golangciWarning golangciSeverity = "warning"
	golangciInfo    golangciSeverity = "info"
)

// severityOf maps a golangci-lint severity string to the normalized severity.
func severityOf(severity golangciSeverity) goyze.Severity {
	switch severity {
	case golangciWarning:
		return goyze.SeverityWarning
	case golangciInfo:
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
