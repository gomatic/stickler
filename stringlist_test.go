package stickler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestApplyToDoesNotMutateBase proves applyTo is purely functional: removing an
// entry compacts a fresh copy, never the caller's slice. Before the clone fix,
// slices.DeleteFunc compacted base in place, corrupting base[0].
func TestApplyToDoesNotMutateBase(t *testing.T) {
	base := []string{"a", "b"}
	list := StringList{remove: []string{"a"}}

	out := list.applyTo(base)

	assert.Equal(t, []string{"b"}, out)
	assert.Equal(t, []string{"a", "b"}, base, "applyTo must not mutate its input slice")
}

// TestApplyToReplaceDoesNotMutateReplaceSlice proves the replace path also copies,
// so a layer's directive slice is never aliased into the resolved result.
func TestApplyToReplaceDoesNotMutateReplaceSlice(t *testing.T) {
	list := StringList{replace: []string{"x"}, add: []string{"y"}}

	out := list.applyTo([]string{"base"})

	assert.Equal(t, []string{"x", "y"}, out)
	assert.Equal(t, []string{"x"}, list.replace, "applyTo must not mutate the layer's replace slice")
}
