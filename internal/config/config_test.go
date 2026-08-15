package config_test

import (
	"errors"
	"testing"

	errs "github.com/gomatic/go-error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/gomatic/stickler/internal/config"
	"github.com/gomatic/stickler/internal/constants"
)

func parseConfig(t *testing.T, text string) config.Config {
	t.Helper()
	var cfg config.Config
	require.NoError(t, yaml.Unmarshal([]byte(text), &cfg))
	return cfg
}

func TestResolveFoldsGlobalThenRepo(t *testing.T) {
	global := parseConfig(t, `
runners: [yze, golangci-lint]
format: human
analyzers:
  ptrrecv:
    allow: [pkg.A]
`)
	repo := parseConfig(t, `
runners:
  add: [revive]
  remove: [golangci-lint]
analyzers:
  ptrrecv:
    allow:
      add: [pkg.B]
  namedtypes:
    allow: [pkg.C]
`)

	got := config.Resolve(global, repo)

	assert.Equal(t, []string{"yze", "revive"}, got.Runners)
	assert.Equal(t, "human", got.Format) // repo omits format, global wins
	assert.Equal(t, []string{"pkg.A", "pkg.B"}, got.Analyzers["ptrrecv"]["allow"])
	assert.Equal(t, []string{"pkg.C"}, got.Analyzers["namedtypes"]["allow"])
}

func TestResolveLaterFormatAndReplaceWin(t *testing.T) {
	global := parseConfig(t, "format: human\nrunners: [yze, golangci-lint]\n")
	repo := parseConfig(t, "format: json\nrunners: [yze]\n")

	got := config.Resolve(global, repo)

	assert.Equal(t, "json", got.Format)
	assert.Equal(t, []string{"yze"}, got.Runners) // sequence replaces
}

func TestStringListRejectsScalar(t *testing.T) {
	var cfg config.Config
	err := yaml.Unmarshal([]byte("runners: nope\n"), &cfg)

	require.Error(t, err)
	assert.True(t, errors.Is(err, constants.ErrBadListSetting))
}

func TestStringListRejectsMistypedDirective(t *testing.T) {
	var cfg config.Config
	err := yaml.Unmarshal([]byte("runners:\n  add: 5\n"), &cfg)

	require.Error(t, err)
}

func TestStringListRejectsUnknownDirectiveKey(t *testing.T) {
	var cfg config.Config
	err := yaml.Unmarshal([]byte("runners:\n  addd: [revive]\n"), &cfg)

	require.Error(t, err)
	assert.True(t, errors.Is(err, constants.ErrBadListSetting))
}

func TestStringListReplaceMappingReplacesAccumulatedBase(t *testing.T) {
	global := parseConfig(t, "runners: [yze, golangci-lint]\n")
	repo := parseConfig(t, "runners:\n  replace: [revive]\n")

	got := config.Resolve(global, repo)

	assert.Equal(t, []string{"revive"}, got.Runners)
}

func TestResolveDeepMergesUntouchedAnalyzerSetting(t *testing.T) {
	global := parseConfig(t, `
analyzers:
  ptrrecv:
    allow: [pkg.A]
    deny: [pkg.X]
`)
	repo := parseConfig(t, `
analyzers:
  ptrrecv:
    allow:
      add: [pkg.B]
`)

	got := config.Resolve(global, repo)

	assert.Equal(t, []string{"pkg.A", "pkg.B"}, got.Analyzers["ptrrecv"]["allow"])
	assert.Equal(
		t,
		[]string{"pkg.X"},
		got.Analyzers["ptrrecv"]["deny"],
		"a setting the repo never touches must survive",
	)
}

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

// TestRepositoryMayNotEmptyTheProbeList covers the other direction: a repo that
// writes an empty `probe:` sequence is declaring one (a replace), and replacing
// the global list with nothing would re-gate every probe fleet-wide from one
// repository's file.
func TestRepositoryMayNotEmptyTheProbeList(t *testing.T) {
	read := func(string) ([]byte, error) { return []byte("probe: []\n"), nil }

	_, err := config.LoadLayers(read, config.Layer{Path: "repo", Scope: config.ScopeRepo})

	require.Error(t, err)
	assert.True(t, errors.Is(err, constants.ErrProbeNotGlobal))
}

// TestResolveDeliversAnalyzerSettingsToYze pins the whole point of the
// `analyzers:` block: the resolved settings must reach yze as a config
// overlay. Without this they parse, merge, and are silently discarded — a
// repository's declared analyzer configuration would have no effect at all.
func TestResolveDeliversAnalyzerSettingsToYze(t *testing.T) {
	resolved := config.Resolve(parseConfig(t, `
analyzers:
  ptrparam:
    allow: [google.golang.org/grpc.UnaryServerInfo]
`))

	overlays := resolved.Config["yze"]
	require.Len(t, overlays, 1, "the analyzer settings must reach yze")
	assert.Equal(t, config.Overlay{
		"analyzers": map[string]any{
			"ptrparam": map[string]any{
				"allow": []any{"google.golang.org/grpc.UnaryServerInfo"},
			},
		},
	}, overlays[0])
}

// TestResolveFoldsLayeredAnalyzerSettingsIntoOneOverlay pins that the
// delivered overlay carries the FOLDED result: a repo's add/remove directives
// are applied before delivery, so yze sees one effective list per setting.
func TestResolveFoldsLayeredAnalyzerSettingsIntoOneOverlay(t *testing.T) {
	global := parseConfig(t, `
analyzers:
  ptrparam:
    allow: [pkg.A, pkg.B]
`)
	repo := parseConfig(t, `
analyzers:
  ptrparam:
    allow:
      add: [pkg.C]
      remove: [pkg.A]
`)
	resolved := config.Resolve(global, repo)

	overlays := resolved.Config["yze"]
	require.Len(t, overlays, 1)
	analyzers, ok := overlays[0]["analyzers"].(map[string]any)
	require.True(t, ok)
	settings, ok := analyzers["ptrparam"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{"pkg.B", "pkg.C"}, settings["allow"])
}

// TestAppendAnalyzerOverlayAddsNothingWithoutSettings pins the no-op case
// appendAnalyzerOverlay documents: an empty overlay list is what leaves yze
// discovering its own .yze.yaml, so a repository that configures nothing
// must not gain a synthesized config.
func TestAppendAnalyzerOverlayAddsNothingWithoutSettings(t *testing.T) {
	resolved := config.Resolve(parseConfig(t, "runners: [yze]\n"))
	assert.Empty(t, resolved.Config["yze"])
}
