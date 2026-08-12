package checks_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gomatic/stickler/internal/checks"
	"github.com/gomatic/stickler/internal/config"
	"github.com/gomatic/stickler/internal/constants"
	"github.com/gomatic/stickler/internal/runner"
	"github.com/gomatic/stickler/internal/suite"
)

// TestNativeIsTheOneListOfChecksSticklerOwns pins the single registry. Both
// domain verbs run over this set, so a check named here but not there would
// mean a repository gated on a rule its own instructions never mention.
func TestNativeIsTheOneListOfChecksSticklerOwns(t *testing.T) {
	t.Parallel()

	native := checks.Native()

	require.Len(t, native, 3)
	for _, name := range []string{"binaries", "clilayout", "clitiers"} {
		runner, ok := native[name]
		require.True(t, ok, name)
		assert.Equal(t, name, runner.Name(), "a check's registry key is its own name")
	}
}

// TestEveryNativeCheckExplainsItself pins that stickler's own checks all state
// their rules. These are the rules stickler OWNS, so there is nobody else to
// ask; a native check that stayed silent would be a standard with no statement.
func TestEveryNativeCheckExplainsItself(t *testing.T) {
	t.Parallel()

	for name, check := range checks.Native() {
		explainer, ok := check.(suite.Explainer)
		require.True(t, ok, "%s states no instructions", name)

		text, err := explainer.Explain(t.Context())

		require.NoError(t, err, name)
		assert.Contains(t, string(text), "stickler/"+name, "a section names the rule it is about")
	}
}

// TestBuildSelectsEveryDeclaredToolAndNativeCheck pins the assembled set and
// its deterministic order.
func TestBuildSelectsEveryDeclaredToolAndNativeCheck(t *testing.T) {
	t.Parallel()

	built, err := checks.Build(stubCommand, config.Resolved{}, ".")

	require.NoError(t, err)
	require.Len(t, built, 5)
	for i, name := range []string{"binaries", "clilayout", "clitiers", "golangci-lint", "yze"} {
		assert.Equal(t, name, built[i].Name())
	}
}

// TestBuildFailsLoudOnAnUnknownName pins fail-closed selection: a repository
// asking for a check nothing defines must not pass greenly over zero checks.
func TestBuildFailsLoudOnAnUnknownName(t *testing.T) {
	t.Parallel()

	_, err := checks.Build(stubCommand, config.Resolved{Runners: []string{"nope"}}, ".")

	assert.ErrorIs(t, err, constants.ErrUnknownRunner)
}

// stubCommand stands in for a subprocess; Build never runs one.
func stubCommand(_ context.Context, _ runner.Name, _ []runner.EnvVar, _ ...runner.Arg) ([]byte, error) {
	return nil, nil
}

// TestResolveFoldsTheGlobalLayerThenTheRepository pins layer order and the
// merge semantics the layering model rests on: a plain sequence REPLACES the
// accumulated value, while an add/remove mapping mutates it. Getting that
// backwards would silently harden every centrally-softened rule in a repo.
func TestResolveFoldsTheGlobalLayerThenTheRepository(t *testing.T) {
	t.Parallel()
	read := func(path string) ([]byte, error) {
		if strings.HasSuffix(path, ".stickler.yaml") {
			return []byte("format: json\nsoft:\n  add: [yze/invariant]\n"), nil
		}
		return []byte("format: human\nsoft: [exhaustive]\n"), nil
	}

	resolved, err := checks.Resolve(read, func(string) string { return "" }, "/home", ".")

	require.NoError(t, err)
	assert.Equal(t, "json", resolved.Format, "the repository layer is folded last and wins")
	assert.Equal(t, []string{"exhaustive", "yze/invariant"}, resolved.Soft,
		"`add` merges with the global list rather than replacing it")
}

// TestResolveSurfacesAMalformedLayer pins that a broken config is an error, not
// an empty configuration that would silently select every default.
func TestResolveSurfacesAMalformedLayer(t *testing.T) {
	t.Parallel()
	read := func(string) ([]byte, error) { return []byte("runners: : :\n"), nil }

	_, err := checks.Resolve(read, func(string) string { return "" }, "/home", ".")

	assert.ErrorIs(t, err, constants.ErrConfig)
}
