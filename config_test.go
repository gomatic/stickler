package stickler_test

import (
	"errors"
	"testing"

	errs "github.com/gomatic/go-error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/gomatic/stickler"
)

func parseConfig(t *testing.T, text string) stickler.Config {
	t.Helper()
	var cfg stickler.Config
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

	got := stickler.Resolve(global, repo)

	assert.Equal(t, []string{"yze", "revive"}, got.Runners)
	assert.Equal(t, "human", got.Format) // repo omits format, global wins
	assert.Equal(t, []string{"pkg.A", "pkg.B"}, got.Analyzers["ptrrecv"]["allow"])
	assert.Equal(t, []string{"pkg.C"}, got.Analyzers["namedtypes"]["allow"])
}

func TestResolveLaterFormatAndReplaceWin(t *testing.T) {
	global := parseConfig(t, "format: human\nrunners: [yze, golangci-lint]\n")
	repo := parseConfig(t, "format: json\nrunners: [yze]\n")

	got := stickler.Resolve(global, repo)

	assert.Equal(t, "json", got.Format)
	assert.Equal(t, []string{"yze"}, got.Runners) // sequence replaces
}

func TestStringListRejectsScalar(t *testing.T) {
	var cfg stickler.Config
	err := yaml.Unmarshal([]byte("runners: nope\n"), &cfg)

	require.Error(t, err)
	assert.True(t, errors.Is(err, stickler.ErrBadListSetting))
}

func TestStringListRejectsMistypedDirective(t *testing.T) {
	var cfg stickler.Config
	err := yaml.Unmarshal([]byte("runners:\n  add: 5\n"), &cfg)

	require.Error(t, err)
}

func TestStringListRejectsUnknownDirectiveKey(t *testing.T) {
	var cfg stickler.Config
	err := yaml.Unmarshal([]byte("runners:\n  addd: [revive]\n"), &cfg)

	require.Error(t, err)
	assert.True(t, errors.Is(err, stickler.ErrBadListSetting))
}

func TestStringListReplaceMappingReplacesAccumulatedBase(t *testing.T) {
	global := parseConfig(t, "runners: [yze, golangci-lint]\n")
	repo := parseConfig(t, "runners:\n  replace: [revive]\n")

	got := stickler.Resolve(global, repo)

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

	got := stickler.Resolve(global, repo)

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

	layers, err := stickler.LoadLayers(read, "global", "repo")

	require.NoError(t, err)
	require.Len(t, layers, 1)
	assert.Equal(t, "github", stickler.Resolve(layers...).Format)
}

func TestLoadLayersReportsParseError(t *testing.T) {
	read := func(string) ([]byte, error) { return []byte("runners: : :\n"), nil }

	_, err := stickler.LoadLayers(read, "bad.yaml")

	require.Error(t, err)
	assert.True(t, errors.Is(err, stickler.ErrConfig))
}

func TestConfigPaths(t *testing.T) {
	withXDG := stickler.ConfigPaths(func(string) string { return "/xdg" }, "/home/u", "/repo")
	assert.Equal(t, "/xdg/stickler/config.yaml", withXDG[0])
	assert.Equal(t, "/repo/.stickler.yaml", withXDG[1])

	noXDG := stickler.ConfigPaths(func(string) string { return "" }, "/home/u", "/repo")
	assert.Equal(t, "/home/u/.config/stickler/config.yaml", noXDG[0])

	// XDG spec: a relative $XDG_CONFIG_HOME is invalid and must be ignored.
	relXDG := stickler.ConfigPaths(func(string) string { return "relative/dir" }, "/home/u", "/repo")
	assert.Equal(t, "/home/u/.config/stickler/config.yaml", relXDG[0])
}
