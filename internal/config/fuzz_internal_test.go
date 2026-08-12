package config

// The config documents stickler parses are untrusted: a .stickler.yaml and a
// tool's base config both come from whatever repository is being linted, and
// Merger.Args parses a base config, folds overlays onto it, and re-emits the
// result for a TOOL to read.
//
// What follows fuzzes the two properties that hold, and two named tests below
// pin two that do NOT — the re-emission is lossy for some scalars. See
// [TestEffectiveConfigNarrowsWholeNumberFloats] and
// [TestEffectiveConfigDropsALeadingNewline].

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gomatic/stickler/internal/constants"
)

// fuzzSeeds are the document shapes the merge pipeline actually meets.
var fuzzSeeds = []string{
	"",
	"null\n",
	"run:\n  timeout: 5m\n",
	"linters:\n  enable: [gocognit, gocyclo]\n",
	"soft:\n  add: [yze/invariant]\n",
	"a: {b: {c: [1, 2, 3]}}\n",
	"\tnot: yaml\n",
	"key: 2001-12-14\n",
	"key: !!binary aGVsbG8=\n",
	"dup: 1\ndup: 2\n",
}

// FuzzParseTreeClassifiesEveryDocument asserts the total contract of the parse
// step: every byte sequence is either a configuration error or a NON-NIL tree,
// and neither outcome is a panic. Both halves matter to a caller — a panic
// takes down a lint pass that was only reading a file, and a nil tree returned
// without an error would let a malformed base config quietly become "no
// settings at all", which is a gate silently relaxing itself.
func FuzzParseTreeClassifiesEveryDocument(f *testing.F) {
	for _, seed := range fuzzSeeds {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		tree, err := parseTree(data)
		if err != nil {
			require.ErrorIs(t, err, constants.ErrConfig, "a rejected document is a configuration error")
			require.Nil(t, tree, "a rejected document yields no tree to merge onto")
			return
		}
		require.NotNil(t, tree, "an accepted document is never a nil tree, so a merge onto it cannot panic")

		encoded, marshalErr := marshalTree(tree)
		require.NoError(t, marshalErr, "whatever parses must be re-emittable: a tool has to read it")
		require.NotNil(t, encoded)
	})
}

// FuzzMergeTreeNeverMutatesTheBase asserts the invariant the layering model is
// built on: overlays are applied onto a base that later layers, other runners,
// and the next invocation all still share. A merge that wrote through would
// make one tool's overlay leak into another's effective config, and the leak
// would depend on runner order — the hardest possible bug to see.
func FuzzMergeTreeNeverMutatesTheBase(f *testing.F) {
	for _, seed := range fuzzSeeds {
		f.Add([]byte(seed), []byte("run:\n  timeout: 9m\n"))
	}

	f.Fuzz(func(t *testing.T, base, overlay []byte) {
		baseTree, err := parseTree(base)
		if err != nil {
			return
		}
		overlayTree, err := parseTree(overlay)
		if err != nil {
			return
		}
		before, err := marshalTree(baseTree)
		require.NoError(t, err)

		mergeTree(baseTree, []Overlay{Overlay(overlayTree)})

		after, err := marshalTree(baseTree)
		require.NoError(t, err)
		require.Equal(t, string(before), string(after), "the base survives the merge unchanged")
	})
}
