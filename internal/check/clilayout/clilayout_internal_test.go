package clilayout

import (
	"context"
	"slices"
	"strings"
	"testing"

	goyze "github.com/gomatic/go-yze"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClilayoutConformantTreeIsSilent pins the whole exemption surface at
// once: a matched pair, a mount-only parent with a self-declaring child, a
// matched NESTED pair, a helper beneath a command, a helper beneath a domain
// verb, a grouping domain package, the shared vocabulary package, and a
// package outside both tiers — none binds a counterpart requirement.
func TestClilayoutConformantTreeIsSilent(t *testing.T) {
	t.Parallel()

	diags, err := Runner{}.Run(context.Background(), "testdata/conformant")

	require.NoError(t, err)
	assert.Empty(t, diags)
}

// TestClilayoutReportsTheKilroyShape pins the regression this check exists
// for: three verbs living in one command package while three domain verb
// packages sit beneath it. Each unmatched domain verb is named, and the flat
// command package — which has no self-declaring child and no domain verb of
// its own — is named too.
func TestClilayoutReportsTheKilroyShape(t *testing.T) {
	t.Parallel()

	diags, err := Runner{}.Run(context.Background(), "testdata/kilroy")

	require.NoError(t, err)
	messages := messagesOf(diags)
	assert.Len(t, messages, 4)
	for _, verb := range []string{"tenant/create", "tenant/list", "tenant/migrate"} {
		assert.True(t, slices.ContainsFunc(messages, func(m string) bool {
			return strings.Contains(m, "internal/domain/"+verb+" has no command package")
		}), "the unmatched domain verb %s must be named: %v", verb, messages)
	}
	assert.True(t, slices.ContainsFunc(messages, func(m string) bool {
		return strings.Contains(m, "internal/app/commands/tenant has no domain verb")
	}), "the flat command package must be named: %v", messages)
}

// TestClilayoutReportsTheBrokenShapes ports yze-go-layout's fixture claims: a
// command with no domain directory at all, one whose counterpart holds no Go
// source, one whose counterpart's only file does not parse, and a domain verb
// with no command package are each reported — including a verb that declares
// only Run, the contract's function element.
func TestClilayoutReportsTheBrokenShapes(t *testing.T) {
	t.Parallel()

	diags, err := Runner{}.Run(context.Background(), "testdata/broken")

	require.NoError(t, err)
	messages := messagesOf(diags)
	assert.Len(t, messages, 5)
	for _, want := range []string{
		"internal/app/commands/orphan has no domain verb",
		"internal/app/commands/stub has no domain verb",
		"internal/app/commands/hollow has no domain verb",
		"internal/domain/lonely has no command package",
		"internal/domain/runonly has no command package",
	} {
		assert.True(t, slices.ContainsFunc(messages, func(m string) bool { return strings.Contains(m, want) }),
			"missing %q in %v", want, messages)
	}
}

// TestClilayoutDiagnosticsAnchorAtTheDeclaringFile pins that a finding lands
// where the developer works: the file declaring the unmatched verb, with the
// stickler/clilayout rule id.
func TestClilayoutDiagnosticsAnchorAtTheDeclaringFile(t *testing.T) {
	t.Parallel()

	diags, err := Runner{}.Run(context.Background(), "testdata/broken")

	require.NoError(t, err)
	require.NotEmpty(t, diags)
	for _, diag := range diags {
		assert.Equal(t, Rule, diag.Rule)
		assert.Equal(t, Name, diag.Tool)
		assert.True(t, strings.HasSuffix(diag.Path, ".go"), "anchored at a source file: %s", diag.Path)
		assert.Positive(t, diag.Line)
	}
}

// TestClilayoutWalkErrorSurfaces pins that an unwalkable root is an error, not
// a silent pass — a check that cannot look must not report clean.
func TestClilayoutWalkErrorSurfaces(t *testing.T) {
	t.Parallel()

	_, err := Runner{}.Run(context.Background(), "testdata/does-not-exist")

	assert.Error(t, err)
}

// TestClilayoutIgnoresBuildExcludedFiles pins the constraint gate in both
// directions: a domain package whose ONLY contract declaration lives in a
// //go:build ignore file is not a verb (skipping it silences the false
// "domain verb has no command package"), while a build-excluded sibling must
// not hide a package's real, untagged declaration — solo/ is still reported,
// anchored at its untagged file.
func TestClilayoutIgnoresBuildExcludedFiles(t *testing.T) {
	t.Parallel()

	diags, err := Runner{}.Run(context.Background(), "testdata/tagged")

	require.NoError(t, err)
	require.Len(t, diags, 1, "greet/ declares only in a build-excluded file and binds nothing: %v", messagesOf(diags))
	assert.Contains(t, diags[0].Message, "internal/domain/solo has no command package")
	assert.True(t, strings.HasSuffix(diags[0].Path, "solo/solo.go"),
		"the diagnostic anchors at the untagged declaring file, never the excluded one: %s", diags[0].Path)
}

// messagesOf projects diagnostics onto their messages.
func messagesOf(diags []goyze.Diagnostic) []string {
	messages := make([]string, 0, len(diags))
	for _, diag := range diags {
		messages = append(messages, diag.Message)
	}
	return messages
}
