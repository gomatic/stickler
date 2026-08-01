package stickler

// Escaping values for GitHub workflow commands. A diagnostic's path and message
// come from the code being linted, and GitHub Actions parses commands out of a
// job's stdout — so without this, a linted repository could emit workflow
// commands through the linter's own output, in a job that trusted the linter
// rather than the code.

import "strings"

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
