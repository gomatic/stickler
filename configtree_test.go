package stickler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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

func TestMergeTreeReplaceWithNonListClears(t *testing.T) {
	// `replace:` present but not a sequence coerces to the empty list, clearing.
	got := MergeTree(map[string]any{"l": []any{"a"}}, []Overlay{{"l": map[string]any{"replace": "scalar"}}})
	assert.Equal(t, []string{}, got["l"])
}

func TestMarshalTreeSurfacesError(t *testing.T) {
	_, err := MarshalTree(map[string]any{"x": failMarshal{}})
	assert.ErrorIs(t, err, ErrConfig)
}
