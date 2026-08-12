package runner_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gomatic/stickler/internal/constants"
	"github.com/gomatic/stickler/internal/runner"
)

func TestExecCommandSurfacesStderrInError(t *testing.T) {
	_, err := runner.ExecCommand(context.Background(), "sh", nil, "-c", "printf 'the real reason' 1>&2; exit 7")

	require.Error(t, err)
	assert.True(t, errors.Is(err, constants.ErrExec))
	assert.Contains(t, err.Error(), "the real reason")
}

func TestExecCommandRunsRealProcess(t *testing.T) {
	out, err := runner.ExecCommand(context.Background(), "go", nil, "version")
	require.NoError(t, err)
	assert.Contains(t, string(out), "go version")

	_, err = runner.ExecCommand(context.Background(), "stickler-no-such-binary-xyz", nil)
	require.Error(t, err)
}

// TestExecCommandPassesEnvToTheChild asserts the env actually REACHES the
// started process, which is the only thing the env parameter promises.
//
// Every existing test passed a nil env, so the whole len(env) > 0 path — and
// the conversion behind it — never ran. Asserting the child's own view of the
// variable is what makes this a contract test: a conversion that dropped or
// mangled entries would still have built a []string and still have satisfied
// any assertion made on the caller's side.
func TestExecCommandPassesEnvToTheChild(t *testing.T) {
	out, err := runner.ExecCommand(
		context.Background(),
		"sh",
		[]runner.EnvVar{"STICKLER_TEST_VAR=reached-the-child"},
		"-c", `printf %s "${STICKLER_TEST_VAR}"`,
	)

	require.NoError(t, err)
	assert.Equal(t, "reached-the-child", string(out))
}
