package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gomatic/stickler/internal/constants"
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

func TestMergerNoOverlaysIsNoOp(t *testing.T) {
	want := assert.New(t)
	args, cleanup, err := Merger{}.Args()
	want.NoError(err)
	want.Nil(args)
	want.NotNil(cleanup)
	cleanup() // no-op, must not panic
}

func TestMergerMergesBaseAndOverlay(t *testing.T) {
	want := assert.New(t)
	var written []byte
	merger := Merger{
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
	want.Equal([]string{"--config=/tmp/effective.yaml"}, args)
	effective, err := parseTree(written)
	want.NoError(err)
	excludes := effective["linters"].(map[string]any)["settings"].(map[string]any)["gosec"].(map[string]any)["excludes"]
	want.Equal([]any{"G101", "G204"}, excludes, "re-parsed YAML yields a generic sequence")
}

// TestMergerDefaultsItsFilesystemSeams pins the production wiring: a merger
// built with neither seam named reads and writes the REAL filesystem, which is
// what lets the composition root leave both unset. Without this, "unset" could
// silently mean "reads nothing" and every tool would run against an empty base
// config.
func TestMergerDefaultsItsFilesystemSeams(t *testing.T) {
	want := assert.New(t)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, golangciBaseYAML), []byte("run:\n  timeout: 3m\n"), 0o600))

	merger := Merger{
		BaseNames: []string{golangciBaseYAML},
		Flag:      "--config={path}",
		BaseDir:   dir,
		Overlays:  []Overlay{{"run": map[string]any{"tests": true}}},
	}

	want.Equal([]byte("run:\n  timeout: 3m\n"), merger.readBase(), "an unset Read reads the real file")

	args, cleanup, err := merger.Args()
	require.NoError(t, err)
	require.Len(t, args, 1)

	path := strings.TrimPrefix(args[0], "--config=")
	written, readErr := os.ReadFile(path)
	require.NoError(t, readErr, "an unset Temp writes a real file")
	effective, parseErr := parseTree(written)
	require.NoError(t, parseErr)
	want.Equal(map[string]any{"timeout": "3m", "tests": true}, effective["run"])

	cleanup()
	_, statErr := os.Stat(path)
	want.True(os.IsNotExist(statErr), "the cleanup removes the effective config")
}

// failMarshal is a value whose MarshalYAML returns an error, so yaml.Marshal fails
// (rather than panicking, as it does for a chan/func), exercising the error paths.
type failMarshal struct{}

func (failMarshal) MarshalYAML() (any, error) { return nil, errors.New("cannot encode") }

func TestMergerMarshalErrorPropagates(t *testing.T) {
	merger := Merger{
		BaseNames: []string{golangciBaseYAML},
		Overlays:  []Overlay{{"x": failMarshal{}}}, // unencodable -> marshalTree fails
		BaseDir:   "/repo",
		Read:      fakeReader(nil),
		Temp:      captureTemp(new([]byte)),
	}
	_, _, err := merger.Args()
	assert.ErrorIs(t, err, constants.ErrConfig)
}

func TestOSTempWriterSurfacesWriteError(t *testing.T) {
	// substitute a create that hands back an already-closed file, so the write fails.
	file, err := os.CreateTemp(t.TempDir(), "x")
	require.NoError(t, err)
	require.NoError(t, file.Close())
	original := osTempCreate
	t.Cleanup(func() { osTempCreate = original })
	osTempCreate = func() (*os.File, error) { return file, nil }

	_, _, writeErr := osTempWriter([]byte("data"))
	assert.Error(t, writeErr)
}

func TestMergerBaseParseErrorPropagates(t *testing.T) {
	merger := Merger{
		BaseNames: []string{golangciBaseYAML},
		Overlays:  []Overlay{{"x": 1}},
		BaseDir:   "/repo",
		Read:      fakeReader(map[string][]byte{"/repo/.golangci.yaml": []byte("\tnot: yaml")}),
		Temp:      captureTemp(new([]byte)),
	}
	_, _, err := merger.Args()
	assert.ErrorIs(t, err, constants.ErrConfig)
}

func TestMergerTempWriteErrorPropagates(t *testing.T) {
	sentinel := errors.New("disk full")
	merger := Merger{
		BaseNames: []string{golangciBaseYAML},
		Overlays:  []Overlay{{"x": 1}},
		BaseDir:   "/repo",
		Read:      fakeReader(nil),
		Temp:      func([]byte) (string, func(), error) { return "", func() {}, sentinel },
	}
	_, _, err := merger.Args()
	assert.ErrorIs(t, err, sentinel)
}

func TestMergerReadBaseFallsBackThenNone(t *testing.T) {
	want := assert.New(t)
	names := []string{golangciBaseYAML, golangciBaseYML}

	yaml := Merger{
		BaseNames: names,
		BaseDir:   "/r",
		Read:      fakeReader(map[string][]byte{"/r/.golangci.yaml": []byte("a: 1")}),
	}
	want.Equal([]byte("a: 1"), yaml.readBase())

	yml := Merger{
		BaseNames: names,
		BaseDir:   "/r",
		Read:      fakeReader(map[string][]byte{"/r/.golangci.yml": []byte("b: 2")}),
	}
	want.Equal([]byte("b: 2"), yml.readBase())

	none := Merger{BaseNames: names, BaseDir: "/r", Read: fakeReader(nil)}
	want.Nil(none.readBase())
}

func TestOSTempWriterWritesAndCleans(t *testing.T) {
	want := assert.New(t)
	path, cleanup, err := osTempWriter([]byte("hello"))
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
	_, _, err := osTempWriter([]byte("x"))
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
