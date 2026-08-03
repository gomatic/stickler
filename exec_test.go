package stickler_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gomatic/stickler"
)

func TestExecCommandSurfacesStderrInError(t *testing.T) {
	_, err := stickler.ExecCommand(context.Background(), "sh", nil, "-c", "printf 'the real reason' 1>&2; exit 7")

	require.Error(t, err)
	assert.True(t, errors.Is(err, stickler.ErrExec))
	assert.Contains(t, err.Error(), "the real reason")
}

func TestExecCommandRunsRealProcess(t *testing.T) {
	out, err := stickler.ExecCommand(context.Background(), "go", nil, "version")
	require.NoError(t, err)
	assert.Contains(t, string(out), "go version")

	_, err = stickler.ExecCommand(context.Background(), "stickler-no-such-binary-xyz", nil)
	require.Error(t, err)
}
