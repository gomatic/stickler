package stickler

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

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

// formatGitHub writes GitHub Actions workflow-command annotations, escaping data
// and property values per GitHub's workflow-command rules.
func formatGitHub(w io.Writer, result Result) error {
	for _, d := range result.Diagnostics {
		if _, err := fmt.Fprintln(w, githubAnnotation(d)); err != nil {
			return err
		}
	}
	return nil
}

// githubAnnotation renders one diagnostic as a `::level props::message` workflow
// command with every component escaped.
func githubAnnotation(d goyze.Diagnostic) string {
	return fmt.Sprintf("::%s %s::%s", ghLevel(d.Severity), githubProps(d), escapeGitHubData(ghValue(d.Message)))
}

// githubProps builds the comma-separated property list, carrying the rule id as
// GitHub's `title` (omitted when absent) so the rule is not dropped.
func githubProps(d goyze.Diagnostic) string {
	props := make([]string, 0, 4)
	if d.Rule != "" {
		props = append(props, "title="+escapeGitHubProperty(ghValue(d.Rule)))
	}
	props = append(
		props,
		"file="+escapeGitHubProperty(ghValue(d.Path)),
		fmt.Sprintf("line=%d", d.Line),
		fmt.Sprintf("col=%d", d.Col),
	)
	return strings.Join(props, ",")
}

// ghValue is a string destined for a GitHub workflow command, escaped before it is
// embedded so control characters and delimiters cannot break the annotation.
type ghValue string

// escapeGitHubData escapes a workflow-command message: %, CR, and LF. % is escaped
// first so the % introduced by later replacements is not double-escaped.
func escapeGitHubData(v ghValue) string {
	s := strings.ReplaceAll(string(v), "%", "%25")
	s = strings.ReplaceAll(s, "\r", "%0D")
	s = strings.ReplaceAll(s, "\n", "%0A")
	return s
}

// escapeGitHubProperty escapes a workflow-command property value: the data escapes
// plus the property delimiters comma and colon.
func escapeGitHubProperty(v ghValue) string {
	s := escapeGitHubData(v)
	s = strings.ReplaceAll(s, ",", "%2C")
	s = strings.ReplaceAll(s, ":", "%3A")
	return s
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
