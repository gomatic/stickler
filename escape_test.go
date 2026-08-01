package stickler

// White-box tests for the GitHub workflow-command escaping. A diagnostic's
// path and message are attacker-influenced in the only way that matters: they
// come from the code being linted. GitHub Actions parses `::error ...::message`
// out of a job's stdout, so an unescaped delimiter in either lets a linted
// repository EMIT WORKFLOW COMMANDS through the linter's output — setting an
// output variable, masking a value, or adding a path — in a job that trusted
// the linter, not the code.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGhValueEscapesEveryCharacterThatCanBreakAnAnnotation names ghValue's
// claim. The data escapes cover the characters that end or split a workflow
// command; % must go first, because escaping it after the others would
// double-escape the % those replacements introduce and corrupt every message
// containing a newline.
func TestGhValueEscapesEveryCharacterThatCanBreakAnAnnotation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in   ghValue
		want string
		why  string
	}{
		{in: "plain", want: "plain", why: "ordinary text is untouched"},
		{in: "100%", want: "100%25", why: "percent is escaped"},
		{in: "a\rb", want: "a%0Db", why: "carriage return would end the command"},
		{in: "a\nb", want: "a%0Ab", why: "newline would end the command"},
		{in: "%0A", want: "%250A", why: "a literal %0A must not survive as an encoded newline"},
		{in: "a\r\nb", want: "a%0D%0Ab", why: "both line terminators"},
	} {
		assert.Equal(t, tc.want, escapeGitHubData(tc.in), "escapeGitHubData(%q): %s", tc.in, tc.why)
	}
}

// TestGhValuePropertyEscapingAlsoCoversTheDelimiters names the property half.
// A property value sits inside the command's `file=...,line=...` list, so a
// comma splits it into another property and a colon ends the property section
// and begins the message — the two characters a path is most likely to contain.
func TestGhValuePropertyEscapingAlsoCoversTheDelimiters(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in   ghValue
		want string
		why  string
	}{
		{in: "a,b", want: "a%2Cb", why: "comma separates properties"},
		{in: "a:b", want: "a%3Ab", why: "colon ends the property section"},
		{in: "a%b", want: "a%25b", why: "the data escapes still apply"},
		{in: "a\nb", want: "a%0Ab", why: "and so do the line terminators"},
	} {
		assert.Equal(t, tc.want, escapeGitHubProperty(tc.in), "escapeGitHubProperty(%q): %s", tc.in, tc.why)
	}
}

// TestGhValueCannotEmitAWorkflowCommand is the property the two escapers exist
// for, stated as the attack rather than as a character list: no input may
// produce output that GitHub will parse as a new command. A path crafted to
// close the annotation and open another must come out inert.
func TestGhValueCannotEmitAWorkflowCommand(t *testing.T) {
	t.Parallel()

	for _, hostile := range []ghValue{
		"a\n::set-output name=x::pwned",
		"a\r::add-mask::secret",
		"a::error::pwned",
		"a,line=1,col=1::pwned",
	} {
		for _, got := range []string{escapeGitHubData(hostile), escapeGitHubProperty(hostile)} {
			assert.NotContains(t, got, "\n", "a newline survived: %q", got)
			assert.NotContains(t, got, "\r", "a carriage return survived: %q", got)
			assert.False(t, strings.Contains(got, "::") && strings.ContainsAny(got, "\r\n"),
				"output can still open a workflow command: %q", got)
		}
		assert.NotContains(t, escapeGitHubProperty(hostile), ",", "a property delimiter survived")
		assert.NotContains(t, escapeGitHubProperty(hostile), ":", "a property terminator survived")
	}
}
