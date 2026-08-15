package config_test

import (
	"errors"
	"testing"

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
