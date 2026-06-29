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

func TestMergeTreeDeepMergesMapsAndReplacesScalars(t *testing.T) {
	want := assert.New(t)
	base := map[string]any{
		"run":     map[string]any{"timeout": "5m"},
		"linters": map[string]any{"default": "standard", "enable": []any{"gocognit"}},
	}
	overlay := Overlay{
		"run":     map[string]any{"timeout": "10m"},                       // scalar replace
		"linters": map[string]any{"settings": map[string]any{"gosec": 1}}, // new nested key
	}

	got := MergeTree(base, []Overlay{overlay})

	want.Equal("10m", got["run"].(map[string]any)["timeout"])
	linters := got["linters"].(map[string]any)
	want.Equal("standard", linters["default"], "untouched key preserved")
	want.Equal([]any{"gocognit"}, linters["enable"], "untouched list preserved")
	want.Equal(1, linters["settings"].(map[string]any)["gosec"])
	want.Equal("5m", base["run"].(map[string]any)["timeout"], "base not mutated")
}

func TestMergeTreeListDirectives(t *testing.T) {
	want := assert.New(t)
	base := map[string]any{"excludes": []any{"G101", "G204"}}

	add := MergeTree(base, []Overlay{{"excludes": map[string]any{"add": []any{"G115"}}}})
	want.Equal([]string{"G101", "G204", "G115"}, add["excludes"])

	remove := MergeTree(base, []Overlay{{"excludes": map[string]any{"remove": []any{"G101"}}}})
	want.Equal([]string{"G204"}, remove["excludes"])

	replace := MergeTree(base, []Overlay{{"excludes": map[string]any{"replace": []any{"G999"}}}})
	want.Equal([]string{"G999"}, replace["excludes"])

	fresh := MergeTree(map[string]any{}, []Overlay{{"disable": map[string]any{"add": []any{"fieldalignment"}}}})
	want.Equal([]string{"fieldalignment"}, fresh["disable"], "directive on absent key starts empty")
}

func TestMergeTreeReplaceWithEmptyAndSequence(t *testing.T) {
	want := assert.New(t)
	base := map[string]any{"enable": []any{"a", "b"}}

	cleared := MergeTree(base, []Overlay{{"enable": map[string]any{"replace": []any{}}}})
	want.Equal([]string{}, cleared["enable"], "explicit empty replace clears")

	seq := MergeTree(base, []Overlay{{"enable": []any{"only"}}})
	want.Equal([]any{"only"}, seq["enable"], "a plain sequence replaces wholesale")
}

func TestMergeTreeMapReplacesNonMapBase(t *testing.T) {
	got := MergeTree(map[string]any{"x": "scalar"}, []Overlay{{"x": map[string]any{"deep": 1}}})
	assert.Equal(t, map[string]any{"deep": 1}, got["x"])
}

func TestMergeTreeFoldsLayersInOrder(t *testing.T) {
	got := MergeTree(
		map[string]any{"excludes": []any{}},
		[]Overlay{
			{"excludes": map[string]any{"add": []any{"G1"}}},
			{"excludes": map[string]any{"add": []any{"G2"}, "remove": []any{"G1"}}},
		},
	)
	assert.Equal(t, []string{"G2"}, got["excludes"])
}

func TestAsDirectivesRejectsNonDirectiveValues(t *testing.T) {
	want := assert.New(t)
	_, ok := asDirectives(map[string]any{"add": []any{"x"}, "timeout": "5m"})
	want.False(ok, "a map mixing a directive key with another key is not a directive set")
	_, ok = asDirectives(map[string]any{})
	want.False(ok, "an empty map is not a directive set")
	_, ok = asDirectives("scalar")
	want.False(ok)
}

func TestParseTree(t *testing.T) {
	want := assert.New(t)

	empty, err := ParseTree(nil)
	want.NoError(err)
	want.Equal(map[string]any{}, empty)

	null, err := ParseTree([]byte("null\n"))
	want.NoError(err)
	want.Equal(map[string]any{}, null)

	tree, err := ParseTree([]byte("run:\n  timeout: 5m\n"))
	want.NoError(err)
	want.Equal("5m", tree["run"].(map[string]any)["timeout"])

	_, err = ParseTree([]byte("\tnot: yaml"))
	want.ErrorIs(err, ErrConfig)
}

func TestMarshalTreeRoundTrips(t *testing.T) {
	want := assert.New(t)
	data, err := MarshalTree(map[string]any{"run": map[string]any{"timeout": "5m"}})
	want.NoError(err)
	back, err := ParseTree(data)
	want.NoError(err)
	want.Equal("5m", back["run"].(map[string]any)["timeout"])
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
		BaseNames: []string{".golangci.yaml"},
		Flag:      "--config=",
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

func TestMergeTreeReplaceWithNonListClears(t *testing.T) {
	// `replace:` present but not a sequence coerces to the empty list, clearing.
	got := MergeTree(map[string]any{"l": []any{"a"}}, []Overlay{{"l": map[string]any{"replace": "scalar"}}})
	assert.Equal(t, []string{}, got["l"])
}

// failMarshal is a value whose MarshalYAML returns an error, so yaml.Marshal fails
// (rather than panicking, as it does for a chan/func), exercising the error paths.
type failMarshal struct{}

func (failMarshal) MarshalYAML() (any, error) { return nil, errors.New("cannot encode") }

func TestMarshalTreeSurfacesError(t *testing.T) {
	_, err := MarshalTree(map[string]any{"x": failMarshal{}})
	assert.ErrorIs(t, err, ErrConfig)
}

func TestConfigMergerMarshalErrorPropagates(t *testing.T) {
	merger := ConfigMerger{
		BaseNames: []string{".golangci.yaml"},
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
		BaseNames: []string{".golangci.yaml"},
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
		BaseNames: []string{".golangci.yaml"},
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
	names := []string{".golangci.yaml", ".golangci.yml"}

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

func TestGolangciMergerWiresSpecAndSeams(t *testing.T) {
	want := assert.New(t)
	overlays := []Overlay{{"x": 1}}
	merger := GolangciMerger(overlays, "/repo")
	want.Equal([]string{".golangci.yaml", ".golangci.yml"}, merger.BaseNames)
	want.Equal("--config=", merger.Flag)
	want.Equal("/repo", merger.BaseDir)
	want.Equal(overlays, merger.Overlays)
	want.NotNil(merger.Read)
	want.NotNil(merger.Temp)
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
		BaseNames: []string{".golangci.yaml"},
		Overlays:  []Overlay{{"x": 1}},
		BaseDir:   "/repo",
		Read:      fakeReader(map[string][]byte{"/repo/.golangci.yaml": []byte("\tbad")}),
		Temp:      captureTemp(new([]byte)),
	}
	command := func(context.Context, RunnerName, ...Arg) ([]byte, error) { return nil, nil }
	_, err := NewGolangciRunner(command, merger).Run(context.Background(), ".")
	assert.ErrorIs(t, err, ErrGolangciFailed)
}

func TestGolangciRunnerPassesConfigFlag(t *testing.T) {
	want := assert.New(t)
	var gotArgs []Arg
	merger := ConfigMerger{
		BaseNames: []string{".golangci.yaml"},
		Flag:      "--config=",
		Overlays:  []Overlay{{"run": map[string]any{"timeout": "9m"}}},
		BaseDir:   "/repo",
		Read:      fakeReader(nil),
		Temp:      captureTemp(new([]byte)),
	}
	command := func(_ context.Context, _ RunnerName, args ...Arg) ([]byte, error) {
		gotArgs = args
		return []byte(`{"Issues":[],"Report":{}}`), nil
	}

	_, err := NewGolangciRunner(command, merger).Run(context.Background(), "./...")

	want.NoError(err)
	want.Equal([]Arg{"run", "--output.json.path=stdout", "--config=/tmp/effective.yaml", "--", "./..."}, gotArgs)
}
