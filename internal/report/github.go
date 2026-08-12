package report

// GitHub Actions workflow-command annotations, and the escaping they require.
// A diagnostic's path and message come from the code being linted, and GitHub
// Actions parses commands out of a job's stdout — so without escaping, a linted
// repository could emit workflow commands through the linter's own output, in a
// job that trusted the linter rather than the code.

import (
	"fmt"
	"io"
	"strings"

	goyze "github.com/gomatic/go-yze"

	"github.com/gomatic/stickler/internal/suite"
)

// The GitHub annotation levels. Every declared Severity is mapped explicitly:
// an unnamed severity falling through a default is a finding silently
// downgraded or upgraded in CI output.
const (
	levelNotice  = "notice"
	levelWarning = "warning"
	levelError   = "error"
)

// writeGitHub writes GitHub Actions workflow-command annotations, escaping data
// and property values per GitHub's workflow-command rules.
func writeGitHub(w io.Writer, result suite.Result) error {
	for _, d := range result.Diagnostics {
		if _, err := fmt.Fprintln(w, annotation(d)); err != nil {
			return err
		}
	}
	return nil
}

// annotation renders one diagnostic as a `::level props::message` workflow
// command with every component escaped.
func annotation(d goyze.Diagnostic) string {
	return fmt.Sprintf("::%s %s::%s", level(d.Severity), properties(d), escapeData(ghValue(d.Message)))
}

// properties builds the comma-separated property list, carrying the rule id as
// GitHub's `title` (omitted when absent) so the rule is not dropped.
func properties(d goyze.Diagnostic) string {
	props := make([]string, 0, 4)
	if d.Rule != "" {
		props = append(props, "title="+escapeProperty(ghValue(d.Rule)))
	}
	props = append(
		props,
		"file="+escapeProperty(ghValue(d.Path)),
		fmt.Sprintf("line=%d", d.Line),
		fmt.Sprintf("col=%d", d.Col),
	)
	return strings.Join(props, ",")
}

// level maps a severity to a GitHub annotation level.
func level(severity goyze.Severity) string {
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

// ghValue is a string destined for a GitHub workflow command, escaped before it is
// embedded so control characters and delimiters cannot break the annotation.
type ghValue string

// escapeData escapes a workflow-command message: %, CR, and LF. % is escaped
// first so the % introduced by later replacements is not double-escaped.
func escapeData(v ghValue) string {
	s := strings.ReplaceAll(string(v), "%", "%25")
	s = strings.ReplaceAll(s, "\r", "%0D")
	s = strings.ReplaceAll(s, "\n", "%0A")
	return s
}

// escapeProperty escapes a workflow-command property value: the data escapes
// plus the property delimiters comma and colon.
func escapeProperty(v ghValue) string {
	s := escapeData(v)
	s = strings.ReplaceAll(s, ",", "%2C")
	s = strings.ReplaceAll(s, ":", "%3A")
	return s
}
