package stickler

import (
	"encoding/json"
	"fmt"
	"io"

	errs "github.com/gomatic/go-error"
	goyze "github.com/gomatic/go-yze"
)

// ErrUnknownOutput reports an output format stickler does not support.
const ErrUnknownOutput errs.Const = "unknown output format"

// OutputFormat names how a Result is rendered.
type OutputFormat string

// The output formats stickler supports.
const (
	OutputHuman  OutputFormat = "human"
	OutputJSON   OutputFormat = "json"
	OutputGitHub OutputFormat = "github"
)

// Format writes the result to w in the named format.
func Format(w io.Writer, format OutputFormat, result Result) error {
	switch format {
	case OutputHuman:
		return formatHuman(w, result)
	case OutputJSON:
		return formatJSON(w, result)
	case OutputGitHub:
		return formatGitHub(w, result)
	default:
		return ErrUnknownOutput.With(nil, "format", string(format))
	}
}

// formatHuman writes one line per diagnostic, then one line per runner error.
func formatHuman(w io.Writer, result Result) error {
	for _, d := range result.Diagnostics {
		if _, err := fmt.Fprintf(w, "%s:%d:%d: %s [%s] (%s)\n", d.Path, d.Line, d.Col, d.Message, d.Severity, d.Rule); err != nil {
			return err
		}
	}
	for _, e := range result.Errors {
		if _, err := fmt.Fprintf(w, "runner error: %s\n", e); err != nil {
			return err
		}
	}
	return nil
}

// jsonResult is the machine-readable rendering of a Result.
type jsonResult struct {
	Diagnostics []goyze.Diagnostic `json:"diagnostics"`
	Errors      []string           `json:"errors,omitempty"`
}

func formatJSON(w io.Writer, result Result) error {
	return json.NewEncoder(w).Encode(jsonResult{
		Diagnostics: result.Diagnostics,
		Errors:      errorStrings(result.Errors),
	})
}

func errorStrings(errors []error) []string {
	out := make([]string, 0, len(errors))
	for _, e := range errors {
		out = append(out, e.Error())
	}
	return out
}

// formatGitHub writes GitHub Actions workflow-command annotations.
func formatGitHub(w io.Writer, result Result) error {
	for _, d := range result.Diagnostics {
		if _, err := fmt.Fprintf(w, "::%s file=%s,line=%d,col=%d::%s\n", ghLevel(d.Severity), d.Path, d.Line, d.Col, d.Message); err != nil {
			return err
		}
	}
	return nil
}

// ghLevel maps a severity to a GitHub annotation level.
func ghLevel(severity goyze.Severity) string {
	switch severity {
	case goyze.SeverityWarning:
		return "warning"
	case goyze.SeverityInfo:
		return "notice"
	default:
		return "error"
	}
}
