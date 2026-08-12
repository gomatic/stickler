package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/gomatic/stickler/internal/constants"
)

func TestMergeTreeListDirectives(t *testing.T) {
	want := assert.New(t)
	base := map[string]any{"excludes": []any{"G101", "G204"}}

	add := mergeTree(base, []Overlay{{"excludes": map[string]any{"add": []any{"G115"}}}})
	want.Equal([]string{"G101", "G204", "G115"}, add["excludes"])

	remove := mergeTree(base, []Overlay{{"excludes": map[string]any{"remove": []any{"G101"}}}})
	want.Equal([]string{"G204"}, remove["excludes"])

	replace := mergeTree(base, []Overlay{{"excludes": map[string]any{"replace": []any{"G999"}}}})
	want.Equal([]string{"G999"}, replace["excludes"])

	fresh := mergeTree(map[string]any{}, []Overlay{{"disable": map[string]any{"add": []any{"fieldalignment"}}}})
	want.Equal([]string{"fieldalignment"}, fresh["disable"], "directive on absent key starts empty")
}

func TestMergeTreeAppliesDirectivesFromDecodedOverlay(t *testing.T) {
	// Regression: a .stickler.yaml decoded through LoadLayers yields nested maps of
	// the named Overlay type (not plain map[string]any). The merge must still
	// deep-merge and apply directives — a plain-type assertion would silently leave
	// the directive map in place and drop the base list.
	want := assert.New(t)
	read := func(string) ([]byte, error) {
		return []byte("config:\n  golangci-lint:\n    linters:\n      settings:\n" +
			"        gosec:\n          excludes: { add: [G101, G118] }\n" +
			"        govet:\n          disable: { add: [fieldalignment] }\n"), nil
	}
	layers, err := LoadLayers(read, ".stickler.yaml")
	require.NoError(t, err)
	overlays := Resolve(layers...).Config["golangci-lint"]

	base := map[string]any{"linters": map[string]any{"settings": map[string]any{
		"gosec": map[string]any{"excludes": []any{"G115"}},
		"govet": map[string]any{"enable-all": true},
	}}}
	settings := mergeTree(base, overlays)["linters"].(map[string]any)["settings"].(map[string]any)

	want.Equal(
		[]string{"G115", "G101", "G118"},
		settings["gosec"].(map[string]any)["excludes"],
		"directive merged onto base list",
	)
	want.Equal([]string{"fieldalignment"}, settings["govet"].(map[string]any)["disable"])
	want.Equal(true, settings["govet"].(map[string]any)["enable-all"], "untouched base key preserved")
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

// TestCloneMapKeepsAMergeFromMutatingTheCallersMap names cloneMap's claim.
// Config merging layers a repo's .stickler.yaml over shared defaults, and the
// defaults map is reused for every subsequent merge in the process. If a merge
// wrote through it, the first repo's overrides would leak into the second's
// baseline — a linter whose rules depend on the order the repos were visited.
//
// A nil map must clone to an empty one rather than nil, because the merge then
// writes into the result; returning nil would panic on the first assignment.
func TestCloneMapKeepsAMergeFromMutatingTheCallersMap(t *testing.T) {
	t.Parallel()

	original := map[string]any{"a": 1, "nested": map[string]any{"b": 2}}
	clone := cloneMap(original)

	clone["a"] = 99
	clone["new"] = true

	assert.Equal(t, 1, original["a"], "writing to the clone must not reach the original")
	assert.NotContains(t, original, "new")
	assert.Equal(t, map[string]any{"b": 2}, original["nested"])

	fromNil := cloneMap(nil)
	require.NotNil(t, fromNil, "a nil map clones to an empty map, never to nil")
	assert.Empty(t, fromNil)
	assert.NotPanics(t, func() { fromNil["k"] = "v" }, "the result must be writable")
}

// TestApplyToNeverMutatesTheBaseOrAnyLayer names applyTo's claim. Config layers
// are folded in order — shared defaults, then org, then repo — and the SAME
// base slice is handed to every layer. A fold that appended in place would let
// one layer's additions leak into the value a later layer starts from, so the
// effective config would depend on the order the layers happened to be folded
// and on whether the base slice had spare capacity. That is the worst kind of
// config bug: it reproduces only for some inputs.
//
// The spare-capacity case is the one a naive implementation passes by accident.
// A base whose len < cap makes append write into the caller's backing array
// rather than a fresh one, so the mutation is invisible until a slice arrives
// with room to grow.
func TestApplyToNeverMutatesTheBaseOrAnyLayer(t *testing.T) {
	t.Parallel()

	base := make([]string, 2, 8) // len 2, cap 8: append would write in place
	base[0], base[1] = "keep", "drop"
	layer := StringList{add: []string{"added"}, remove: []string{"drop"}}

	got := layer.applyTo(base)

	assert.Equal(t, []string{"keep", "added"}, got)
	assert.Equal(t, []string{"keep", "drop"}, base, "the base must be untouched, spare capacity or not")
	assert.Equal(t, []string{"added"}, layer.add, "and so must the layer's own slices")
	assert.Equal(t, []string{"drop"}, layer.remove)

	// A replace layer must clone too: the same StringList value may be applied
	// to more than one base.
	replacer := StringList{replace: []string{"only"}}
	first := replacer.applyTo([]string{"a"})
	first[0] = "mutated"
	second := replacer.applyTo([]string{"b"})

	assert.Equal(t, []string{"only"}, second, "a later apply must not see an earlier result's edits")
	assert.Equal(t, []string{"only"}, replacer.replace, "the layer's replace slice is untouched")
}

// TestUnmarshalYAMLRefusesEveryNodeKindThatIsNotAListOrDirectiveMap covers the
// node kinds a list setting can be mis-written as. A scalar is the likeliest
// typo by far — `exclude: vendor` instead of `exclude: [vendor]` — and the
// failure it must not have is silent acceptance: decoding a scalar into an
// empty list would turn a setting the author clearly meant into no setting at
// all, and the run would look configured while ignoring them.
func TestUnmarshalYAMLRefusesEveryNodeKindThatIsNotAListOrDirectiveMap(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		yaml string
	}{
		{name: "a bare scalar", yaml: "vendor"},
		{name: "a quoted scalar", yaml: `"vendor"`},
		{name: "an alias to a scalar", yaml: "&a vendor\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var node yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte(tc.yaml), &node))

			var list StringList
			err := list.UnmarshalYAML(node.Content[0])

			assert.ErrorIs(t, err, constants.ErrBadListSetting, "a %s is not a list setting", tc.name)
		})
	}
}

// TestUnmarshalYAMLRefusesAKindlessNode covers the default arm: a node carrying
// no kind at all reaches UnmarshalYAML only from a caller constructing one, and
// it must be refused rather than decoded as an empty list.
func TestUnmarshalYAMLRefusesAKindlessNode(t *testing.T) {
	t.Parallel()
	var list StringList

	assert.ErrorIs(t, list.UnmarshalYAML(&yaml.Node{}), constants.ErrBadListSetting)
}
