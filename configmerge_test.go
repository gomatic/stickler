package stickler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeReader serves canned file bytes; an unknown path is reported absent.
func fakeReader(files map[string][]byte) FileReader {
	return func(path string) ([]byte, error) {
		if data, ok := files[path]; ok {
			return data, nil
		}
		return nil, os.ErrNotExist
	}
}

// captureTemp records the bytes it is asked to write and returns a fixed path.
func captureTemp(captured *[]byte) TempWriter {
	return func(data []byte) (string, func(), error) {
		*captured = data
		return "/tmp/effective.yaml", func() {}, nil
	}
}

func TestConfigMergerNoOverlaysIsNoOp(t *testing.T) {
	want := assert.New(t)
	args, cleanup, err := ConfigMerger{}.Args()
	want.NoError(err)
	want.Nil(args)
	want.NotNil(cleanup)
	cleanup() // no-op, must not panic
}

func TestConfigMergerMergesBaseAndOverlay(t *testing.T) {
	want := assert.New(t)
	var written []byte
	merger := ConfigMerger{
		BaseNames: []string{golangciBaseYAML},
		Flag:      "--config={path}",
		Overlays: []Overlay{
			{
				"linters": map[string]any{
					"settings": map[string]any{
						"gosec": map[string]any{"excludes": map[string]any{"add": []any{"G204"}}},
					},
				},
			},
		},
		BaseDir: "/repo",
		Read: fakeReader(
			map[string][]byte{
				"/repo/.golangci.yaml": []byte("linters:\n  settings:\n    gosec:\n      excludes: [G101]\n"),
			},
		),
		Temp: captureTemp(&written),
	}

	args, cleanup, err := merger.Args()
	defer cleanup()

	want.NoError(err)
	want.Equal([]Arg{"--config=/tmp/effective.yaml"}, args)
	effective, err := ParseTree(written)
	want.NoError(err)
	excludes := effective["linters"].(map[string]any)["settings"].(map[string]any)["gosec"].(map[string]any)["excludes"]
	want.Equal([]any{"G101", "G204"}, excludes, "re-parsed YAML yields a generic sequence")
}

// failMarshal is a value whose MarshalYAML returns an error, so yaml.Marshal fails
// (rather than panicking, as it does for a chan/func), exercising the error paths.
type failMarshal struct{}

func (failMarshal) MarshalYAML() (any, error) { return nil, errors.New("cannot encode") }

func TestConfigMergerMarshalErrorPropagates(t *testing.T) {
	merger := ConfigMerger{
		BaseNames: []string{golangciBaseYAML},
		Overlays:  []Overlay{{"x": failMarshal{}}}, // unencodable -> MarshalTree fails
		BaseDir:   "/repo",
		Read:      fakeReader(nil),
		Temp:      captureTemp(new([]byte)),
	}
	_, _, err := merger.Args()
	assert.ErrorIs(t, err, ErrConfig)
}

func TestOSTempWriterSurfacesWriteError(t *testing.T) {
	// substitute a create that hands back an already-closed file, so the write fails.
	file, err := os.CreateTemp(t.TempDir(), "x")
	require.NoError(t, err)
	require.NoError(t, file.Close())
	original := osTempCreate
	t.Cleanup(func() { osTempCreate = original })
	osTempCreate = func() (*os.File, error) { return file, nil }

	_, _, writeErr := OSTempWriter([]byte("data"))
	assert.Error(t, writeErr)
}

func TestConfigMergerBaseParseErrorPropagates(t *testing.T) {
	merger := ConfigMerger{
		BaseNames: []string{golangciBaseYAML},
		Overlays:  []Overlay{{"x": 1}},
		BaseDir:   "/repo",
		Read:      fakeReader(map[string][]byte{"/repo/.golangci.yaml": []byte("\tnot: yaml")}),
		Temp:      captureTemp(new([]byte)),
	}
	_, _, err := merger.Args()
	assert.ErrorIs(t, err, ErrConfig)
}

func TestConfigMergerTempWriteErrorPropagates(t *testing.T) {
	sentinel := errors.New("disk full")
	merger := ConfigMerger{
		BaseNames: []string{golangciBaseYAML},
		Overlays:  []Overlay{{"x": 1}},
		BaseDir:   "/repo",
		Read:      fakeReader(nil),
		Temp:      func([]byte) (string, func(), error) { return "", func() {}, sentinel },
	}
	_, _, err := merger.Args()
	assert.ErrorIs(t, err, sentinel)
}

func TestConfigMergerReadBaseFallsBackThenNone(t *testing.T) {
	want := assert.New(t)
	names := []string{golangciBaseYAML, golangciBaseYML}

	yaml := ConfigMerger{
		BaseNames: names,
		BaseDir:   "/r",
		Read:      fakeReader(map[string][]byte{"/r/.golangci.yaml": []byte("a: 1")}),
	}
	want.Equal([]byte("a: 1"), yaml.readBase())

	yml := ConfigMerger{
		BaseNames: names,
		BaseDir:   "/r",
		Read:      fakeReader(map[string][]byte{"/r/.golangci.yml": []byte("b: 2")}),
	}
	want.Equal([]byte("b: 2"), yml.readBase())

	none := ConfigMerger{BaseNames: names, BaseDir: "/r", Read: fakeReader(nil)}
	want.Nil(none.readBase())
}

func TestSpecMergerWiresConfigFromSpec(t *testing.T) {
	want := assert.New(t)
	overlays := []Overlay{{"x": 1}}
	merger := specMerger(
		DefaultRunnerSpecs()["golangci-lint"],
		RunnerContext{BaseDir: "/repo", Config: map[string][]Overlay{"golangci-lint": overlays}},
		"golangci-lint",
	)
	want.Equal([]string{golangciBaseYAML, golangciBaseYML}, merger.BaseNames)
	want.Equal("--config={path}", merger.Flag)
	want.Equal("/repo", merger.BaseDir)
	want.Equal(overlays, merger.Overlays)
	want.NotNil(merger.Read)
	want.NotNil(merger.Temp)
}

func TestSpecMergerZeroWhenSpecHasNoConfig(t *testing.T) {
	spec := RunnerSpec{Name: "plain", Command: []string{"plain"}, Format: ParserSticklerJSON}
	merger := specMerger(spec, RunnerContext{}, "plain")
	assert.Empty(t, merger.BaseNames)
	assert.Nil(t, merger.Read)
}

// TestSpecMergerWiresYzeConfig pins that yze takes its configuration through
// the same generic merge path as every other tool: the per-analyzer settings
// a repository declares are useless unless they reach the analyzer suite.
func TestSpecMergerWiresYzeConfig(t *testing.T) {
	merger := specMerger(DefaultRunnerSpecs()["yze"], RunnerContext{BaseDir: "/repo"}, "yze")
	assert.Equal(t, []string{".yze.yaml", ".yze.yml"}, merger.BaseNames)
	assert.Equal(t, "--config={path}", merger.Flag)
	assert.NotNil(t, merger.Read)
}

// golangciSpecRunner builds a generic specRunner wired to the golangci parser and a
// given merger, exercising the config-file path white-box with injected seams.
func golangciSpecRunner(command Command, merger ConfigMerger) specRunner {
	return specRunner{
		spec: RunnerSpec{
			Name:    "golangci-lint",
			Command: []string{"golangci-lint", "run"},
			Args:    []string{"--output.json.path=stdout", placeholderConfig, "--", placeholderRoot},
			Format:  ParserGolangciJSON,
		},
		command: command,
		parser:  parseGolangciJSON,
		merger:  merger,
	}
}

func TestOSTempWriterWritesAndCleans(t *testing.T) {
	want := assert.New(t)
	path, cleanup, err := OSTempWriter([]byte("hello"))
	want.NoError(err)
	data, readErr := os.ReadFile(path)
	want.NoError(readErr)
	want.Equal("hello", string(data))
	cleanup()
	_, statErr := os.Stat(path)
	want.True(os.IsNotExist(statErr), "cleanup removes the temp file")
}

func TestOSTempWriterCreateError(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "absent"))
	_, _, err := OSTempWriter([]byte("x"))
	assert.Error(t, err)
}

func TestWriteAndCloseSurfacesWriteError(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "x")
	require.NoError(t, err)
	require.NoError(t, file.Close()) // writing to a closed file fails
	assert.Error(t, writeAndClose(file, []byte("data")))
}

func TestResolveCollectsConfigOverlaysInOrder(t *testing.T) {
	want := assert.New(t)
	global := Config{Config: map[string]Overlay{"golangci-lint": {"a": 1}}}
	plain := Config{}
	repo := Config{Config: map[string]Overlay{"golangci-lint": {"b": 2}, "revive": {"c": 3}}}

	resolved := Resolve(global, plain, repo)

	want.Equal([]Overlay{{"a": 1}, {"b": 2}}, resolved.Config["golangci-lint"], "per-tool, in layer order")
	want.Equal([]Overlay{{"c": 3}}, resolved.Config["revive"])
}

func TestGolangciRunnerSurfacesEffectiveConfigError(t *testing.T) {
	merger := ConfigMerger{
		BaseNames: []string{golangciBaseYAML},
		Overlays:  []Overlay{{"x": 1}},
		BaseDir:   "/repo",
		Read:      fakeReader(map[string][]byte{"/repo/.golangci.yaml": []byte("\tbad")}),
		Temp:      captureTemp(new([]byte)),
	}
	command := func(context.Context, RunnerName, []EnvVar, ...Arg) ([]byte, error) { return nil, nil }
	_, err := golangciSpecRunner(command, merger).Run(context.Background(), ".")
	assert.ErrorIs(t, err, ErrRunnerFailed)
}

func TestGolangciRunnerPassesConfigFlag(t *testing.T) {
	want := assert.New(t)
	var gotArgs []Arg
	merger := ConfigMerger{
		BaseNames: []string{golangciBaseYAML},
		Flag:      "--config={path}",
		Overlays:  []Overlay{{"run": map[string]any{"timeout": "9m"}}},
		BaseDir:   "/repo",
		Read:      fakeReader(nil),
		Temp:      captureTemp(new([]byte)),
	}
	command := func(_ context.Context, _ RunnerName, _ []EnvVar, args ...Arg) ([]byte, error) {
		gotArgs = args
		return []byte(`{"Issues":[],"Report":{}}`), nil
	}

	_, err := golangciSpecRunner(command, merger).Run(context.Background(), "./...")

	want.NoError(err)
	want.Equal([]Arg{"run", "--output.json.path=stdout", "--config=/tmp/effective.yaml", "--", "./..."}, gotArgs)
}
