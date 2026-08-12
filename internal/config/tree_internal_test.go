package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gomatic/stickler/internal/constants"
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

	got := mergeTree(base, []Overlay{overlay})

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

	cleared := mergeTree(base, []Overlay{{"enable": map[string]any{"replace": []any{}}}})
	want.Equal([]string{}, cleared["enable"], "explicit empty replace clears")

	seq := mergeTree(base, []Overlay{{"enable": []any{"only"}}})
	want.Equal([]any{"only"}, seq["enable"], "a plain sequence replaces wholesale")
}

func TestMergeTreeMapReplacesNonMapBase(t *testing.T) {
	got := mergeTree(map[string]any{"x": "scalar"}, []Overlay{{"x": map[string]any{"deep": 1}}})
	assert.Equal(t, map[string]any{"deep": 1}, got["x"])
}

func TestMergeTreeFoldsLayersInOrder(t *testing.T) {
	got := mergeTree(
		map[string]any{"excludes": []any{}},
		[]Overlay{
			{"excludes": map[string]any{"add": []any{"G1"}}},
			{"excludes": map[string]any{"add": []any{"G2"}, "remove": []any{"G1"}}},
		},
	)
	assert.Equal(t, []string{"G2"}, got["excludes"])
}

func TestParseTreeErrConfigOnUnparseableYAML(t *testing.T) {
	want := assert.New(t)

	empty, err := parseTree(nil)
	want.NoError(err)
	want.Equal(map[string]any{}, empty)

	null, err := parseTree([]byte("null\n"))
	want.NoError(err)
	want.Equal(map[string]any{}, null)

	tree, err := parseTree([]byte("run:\n  timeout: 5m\n"))
	want.NoError(err)
	want.Equal("5m", tree["run"].(map[string]any)["timeout"])

	_, err = parseTree([]byte("\tnot: yaml"))
	want.ErrorIs(err, constants.ErrConfig)
}

func TestMarshalTreeRoundTrips(t *testing.T) {
	want := assert.New(t)
	data, err := marshalTree(map[string]any{"run": map[string]any{"timeout": "5m"}})
	want.NoError(err)
	back, err := parseTree(data)
	want.NoError(err)
	want.Equal("5m", back["run"].(map[string]any)["timeout"])
}

func TestMergeTreeReplaceWithNonListClears(t *testing.T) {
	// `replace:` present but not a sequence coerces to the empty list, clearing.
	got := mergeTree(map[string]any{"l": []any{"a"}}, []Overlay{{"l": map[string]any{"replace": "scalar"}}})
	assert.Equal(t, []string{}, got["l"])
}

func TestMarshalTreeSurfacesError(t *testing.T) {
	_, err := marshalTree(map[string]any{"x": failMarshal{}})
	assert.ErrorIs(t, err, constants.ErrConfig)
}

// TestEffectiveConfigNarrowsWholeNumberFloats records a KNOWN DEFECT, found by
// fuzzing the round trip and pinned here so a fix is detectable rather than
// invisible.
//
// The merge decodes a document into `any`, so a whole-number float loses its
// fractional spelling: a tool's `confidence: 1.0` is re-emitted into the
// effective config as `confidence: 1`. Every tool stickler currently runs
// decodes that setting into a typed float field, so nothing breaks today — but
// the effective config is supposed to be a faithful rendering of what the
// repository wrote, and this is not faithful.
//
// This test asserts the CURRENT behaviour on purpose. When the values survive
// exactly, the defect is fixed: delete it.
func TestEffectiveConfigNarrowsWholeNumberFloats(t *testing.T) {
	t.Parallel()

	tree, err := parseTree([]byte("confidence: 1.0\nscale: 1e3\nratio: 0.8\n"))
	require.NoError(t, err)
	require.Equal(t, float64(1), tree["confidence"], "the first parse still sees a float")

	again := reparse(t, tree)
	assert.Equal(t, 1, again["confidence"], "DEFECT: 1.0 is re-emitted as an integer")
	assert.Equal(t, 1000, again["scale"], "DEFECT: 1e3 is re-emitted as an integer")
	assert.Equal(t, 0.8, again["ratio"], "a fractional value is unaffected")
}

// TestEffectiveConfigDropsALeadingNewline records the second KNOWN DEFECT from
// the same root cause, and the more serious of the two: it is silent data loss,
// not a change of spelling.
//
// A string value beginning with a newline is emitted as a block scalar with a
// strip indicator, and re-reading that block scalar loses the leading newline —
// so the value stickler hands the tool is not the value the repository wrote.
// No setting in the tools stickler runs today holds such a string, which is the
// only reason this is not urgent.
//
// This test asserts the CURRENT behaviour on purpose. When the value survives,
// the defect is fixed: delete it.
func TestEffectiveConfigDropsALeadingNewline(t *testing.T) {
	t.Parallel()

	tree, err := parseTree([]byte("key: \"\\nabc\"\n"))
	require.NoError(t, err)
	require.Equal(t, "\nabc", tree["key"], "the first parse reads the value intact")

	assert.Equal(t, "abc", reparse(t, tree)["key"], "DEFECT: the leading newline is lost on re-emission")
}

// reparse renders a tree and reads it back, as a tool reading an effective
// config does.
func reparse(t *testing.T, tree map[string]any) map[string]any {
	t.Helper()
	encoded, err := marshalTree(tree)
	require.NoError(t, err)
	again, err := parseTree(encoded)
	require.NoError(t, err)
	return again
}
