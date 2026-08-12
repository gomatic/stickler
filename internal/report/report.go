// Package report renders a finished lint pass. Each format is a WIRE format
// with a consumer that parses it — a terminal, a GitHub Actions runner, a code
// scanning upload — which is why rendering lives here rather than in a generic
// result encoder.
package report

import (
	"encoding/json"
	"fmt"
	"io"

	goyze "github.com/gomatic/go-yze"

	"github.com/gomatic/stickler/internal/constants"
	"github.com/gomatic/stickler/internal/suite"
)

// OutputFormat names how a Result is rendered.
type OutputFormat string

// The output formats stickler supports.
const (
	OutputHuman  OutputFormat = "human"
	OutputJSON   OutputFormat = "json"
	OutputGitHub OutputFormat = "github"
	OutputSARIF  OutputFormat = "sarif"
)

// Write renders the result to w in the named format.
func Write(w io.Writer, format OutputFormat, result suite.Result) error {
	switch format {
	case OutputHuman:
		return writeHuman(w, result)
	case OutputJSON:
		return writeJSON(w, result)
	case OutputGitHub:
		return writeGitHub(w, result)
	case OutputSARIF:
		return writeSARIF(w, result)
	default:
		return constants.ErrUnknownOutput.With(nil, "format", string(format))
	}
}

// writeHuman writes one line per diagnostic, then one line per runner error.
func writeHuman(w io.Writer, result suite.Result) error {
	for _, d := range result.Diagnostics {
		_, err := fmt.Fprintf(w, "%s:%d:%d: %s [%s] (%s)\n",
			d.Path, d.Line, d.Col, d.Message, d.Severity, d.Rule)
		if err != nil {
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

// writeJSON encodes the result as one JSON document.
func writeJSON(w io.Writer, result suite.Result) error {
	return json.NewEncoder(w).Encode(jsonResult{
		Diagnostics: result.Diagnostics,
		Errors:      errorStrings(result.Errors),
	})
}

// errorStrings renders each runner error as its message.
func errorStrings(errors []error) []string {
	out := make([]string, 0, len(errors))
	for _, e := range errors {
		out = append(out, e.Error())
	}
	return out
}
