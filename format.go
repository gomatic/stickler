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

// levelWarning is the "warning" severity string shared by the severity adapters and
// the github/sarif level mappers.
const levelWarning = "warning"

// levelNotice and levelError are the remaining GitHub annotation levels. Named
// alongside levelWarning so the mapping below can cover every declared Severity
// explicitly: an unnamed severity falling through a default is a finding
// silently downgraded or upgraded in CI output.
const (
	levelNotice = "notice"
	levelError  = "error"
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

// Format writes the result to w in the named format.
func Format(w io.Writer, format OutputFormat, result Result) error {
	switch format {
	case OutputHuman:
		return formatHuman(w, result)
	case OutputJSON:
		return formatJSON(w, result)
	case OutputGitHub:
		return formatGitHub(w, result)
	case OutputSARIF:
		return formatSARIF(w, result)
	default:
		return ErrUnknownOutput.With(nil, "format", string(format))
	}
}

// formatHuman writes one line per diagnostic, then one line per runner error.
func formatHuman(w io.Writer, result Result) error {
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

// ghLevel maps a severity to a GitHub annotation level.
func ghLevel(severity goyze.Severity) string {
	switch severity {
	case goyze.SeverityWarning:
		return levelWarning
	case goyze.SeverityInfo:
		return levelNotice
	case goyze.SeverityError:
		return levelError
	default:
		return levelError
	}
}
