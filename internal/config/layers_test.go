package config_test

// The layer tests: where a configuration layer comes from, and what its scope
// permits. They live beside layers.go rather than in the settings tests,
// because a scope rule is answered by the loader and never by the merge.

import (
	"errors"
	"testing"

	errs "github.com/gomatic/go-error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gomatic/stickler/internal/config"
	"github.com/gomatic/stickler/internal/constants"
)

func TestLoadLayersSkipsMissingAndParsesPresent(t *testing.T) {
	read := func(path string) ([]byte, error) {
		if path == "global" {
			return nil, errs.Const("absent")
		}
		return []byte("format: github\n"), nil
	}

	layers, err := config.LoadLayers(read,
		config.Layer{Path: "global", Scope: config.ScopeGlobal},
		config.Layer{Path: "repo", Scope: config.ScopeRepo},
	)

	require.NoError(t, err)
	require.Len(t, layers, 1)
	assert.Equal(t, "github", config.Resolve(layers...).Format)
}

func TestLoadLayersReportsParseError(t *testing.T) {
	read := func(string) ([]byte, error) { return []byte("runners: : :\n"), nil }

	_, err := config.LoadLayers(read, config.Layer{Path: "bad.yaml", Scope: config.ScopeGlobal})

	require.Error(t, err)
	assert.True(t, errors.Is(err, constants.ErrConfig))
}

func TestConfigLayers(t *testing.T) {
	withXDG := config.Layers(func(string) string { return "/xdg" }, "/home/u", "/repo")
	assert.Equal(t, config.Path("/xdg/stickler/config.yaml"), withXDG[0].Path)
	assert.Equal(t, config.Path("/repo/.stickler.yaml"), withXDG[1].Path)

	// The scope travels WITH the path rather than being inferred from position:
	// an absent global config is skipped, so "the first layer loaded" is not the
	// global one, and a probe declaration must never be honoured by accident.
	assert.Equal(t, config.ScopeGlobal, withXDG[0].Scope)
	assert.Equal(t, config.ScopeRepo, withXDG[1].Scope)

	noXDG := config.Layers(func(string) string { return "" }, "/home/u", "/repo")
	assert.Equal(t, config.Path("/home/u/.config/stickler/config.yaml"), noXDG[0].Path)

	// XDG spec: a relative $XDG_CONFIG_HOME is invalid and must be ignored.
	relXDG := config.Layers(func(string) string { return "relative/dir" }, "/home/u", "/repo")
	assert.Equal(t, config.Path("/home/u/.config/stickler/config.yaml"), relXDG[0].Path)
}

// TestUnresolvableHomeYieldsNoGlobalLayerAtAll pins that a home which did not
// resolve produces NO global layer rather than a RELATIVE one. Joining an empty
// home gives `.config/stickler/config.yaml`, which resolves against the working
// directory — inside the tree being linted. A repository that committed that
// path would then supply its own global layer, and the settings only the global
// layer may declare would be the repository's to declare, which is exactly the
// disablement the scope exists to prevent. A container with no HOME needs no
// attacker to arrive in that state.
func TestUnresolvableHomeYieldsNoGlobalLayerAtAll(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		xdg  string
		home config.HomeDir
	}{
		{name: "no home and no XDG", xdg: "", home: ""},
		{name: "a relative XDG and no home", xdg: "relative/dir", home: ""},
		{name: "a relative home", xdg: "", home: "also/relative"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			layers := config.Layers(func(string) string { return tc.xdg }, tc.home, "/repo")

			require.Len(t, layers, 1, "the repository is the only layer there is")
			assert.Equal(t, config.Path("/repo/.stickler.yaml"), layers[0].Path)
			assert.Equal(t, config.ScopeRepo, layers[0].Scope)
		})
	}
}

func TestGlobalLayerDeclaresTheProbeRules(t *testing.T) {
	read := func(path string) ([]byte, error) {
		if path == "global" {
			return []byte("soft: [yze/invariant, yze/errtest]\nprobe: [yze/invariant]\n"), nil
		}
		return []byte("soft-baseline:\n  yze/errtest: 3\n"), nil
	}

	layers, err := config.LoadLayers(read,
		config.Layer{Path: "global", Scope: config.ScopeGlobal},
		config.Layer{Path: "repo", Scope: config.ScopeRepo},
	)
	require.NoError(t, err)

	resolved := config.Resolve(layers...)
	assert.Equal(t, []string{"yze/invariant"}, resolved.Probe)
	assert.Equal(t, []string{"yze/invariant", "yze/errtest"}, resolved.Soft)
	assert.Equal(t, map[string]int{"yze/errtest": 3}, resolved.SoftBaseline)
}

// TestRepositoryMayNotDeclareAProbe pins the one thing this mechanism must not
// become: a way for a repository to stop a rule gating forever. Raising a
// baseline is at least a NUMBER a reviewer can read and a ratchet that may only
// fall; a repo-declared probe would be unbounded and uncounted, which is the
// disablement the ratchet exists to prevent. Which analyzers are judgment-bound
// is a property of the analyzer, so only the global layer says so.
func TestRepositoryMayNotDeclareAProbe(t *testing.T) {
	read := func(path string) ([]byte, error) {
		if path == "global" {
			return []byte("probe: [yze/invariant]\n"), nil
		}
		return []byte("probe:\n  add: [yze/ptrrecv]\n"), nil
	}

	_, err := config.LoadLayers(read,
		config.Layer{Path: "global", Scope: config.ScopeGlobal},
		config.Layer{Path: "/repo/.stickler.yaml", Scope: config.ScopeRepo},
	)

	require.Error(t, err)
	assert.True(t, errors.Is(err, constants.ErrProbeNotGlobal))
	assert.Contains(t, err.Error(), "/repo/.stickler.yaml", "the refusal names the file to edit")
}

// TestRepositoryMayNotWriteTheProbeKeyInAnySpelling enumerates the spellings of
// the key, not the spellings that would CHANGE the folded list. Several of these
// are no-ops today — an empty mapping, a null, an empty directive — and refusing
// only the effective ones would mean a repository writing one of the others is
// silently ignored, which is how a refusal decays into a shrug the moment the
// merge rules change.
func TestRepositoryMayNotWriteTheProbeKeyInAnySpelling(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		yaml string
	}{
		{name: "a populated sequence", yaml: "probe: [yze/invariant]\n"},
		{name: "an empty sequence, which replaces with nothing", yaml: "probe: []\n"},
		{name: "an add directive", yaml: "probe:\n  add: [yze/invariant]\n"},
		{name: "an empty add directive", yaml: "probe:\n  add: []\n"},
		{name: "a remove directive", yaml: "probe:\n  remove: [yze/invariant]\n"},
		{name: "an empty mapping", yaml: "probe: {}\n"},
		{name: "a null value", yaml: "probe:\n"},
		{name: "a null value beside another setting", yaml: "probe:\nformat: json\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			read := func(string) ([]byte, error) { return []byte(tc.yaml), nil }

			_, err := config.LoadLayers(read, config.Layer{Path: "repo", Scope: config.ScopeRepo})

			assert.ErrorIs(t, err, constants.ErrProbeNotGlobal, "%s is a declaration", tc.name)
		})
	}
}

// TestRepositoryMaySettleEverySettingThatIsNotAProbe pins the refusal's edge: it
// is the probe key that a repository may not write, not configuration in
// general, and not a key that merely contains the word.
func TestRepositoryMaySettleEverySettingThatIsNotAProbe(t *testing.T) {
	t.Parallel()
	read := func(string) ([]byte, error) {
		return []byte("soft:\n  add: [yze/invariant]\nsoft-baseline:\n  yze/invariant: 2\nformat: json\n"), nil
	}

	layers, err := config.LoadLayers(read, config.Layer{Path: "repo", Scope: config.ScopeRepo})

	require.NoError(t, err)
	resolved := config.Resolve(layers...)
	assert.Equal(t, []string{"yze/invariant"}, resolved.Soft)
	assert.Equal(t, map[string]int{"yze/invariant": 2}, resolved.SoftBaseline)
	assert.Empty(t, resolved.Probe)
}
