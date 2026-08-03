package stickler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hermeticRunner builds the single named default-spec runner over command.
func hermeticRunner(t *testing.T, command Command, name string) Runner {
	t.Helper()
	runners, err := BuildRunners(command, DefaultRunnerSpecs(), []string{name}, RunnerContext{})
	require.NoError(t, err)
	require.Len(t, runners, 1)
	return runners[0]
}

// TestGolangciDefaultSpecRunsHermeticCache pins the hermetic-cache contract:
// the built-in golangci-lint spec binds GOLANGCI_LINT_CACHE to a fresh
// absolute temporary directory that exists for the duration of the run, is
// removed afterwards, and differs between invocations — so no ambient or
// prior cache state can influence what the gate reports.
func TestGolangciDefaultSpecRunsHermeticCache(t *testing.T) {
	want := assert.New(t)
	var seen []string
	command := func(_ context.Context, _ RunnerName, env []EnvVar, _ ...Arg) ([]byte, error) {
		for _, entry := range env {
			value, ok := strings.CutPrefix(string(entry), golangciCacheEnv+"=")
			if !ok {
				continue
			}
			seen = append(seen, value)
			info, err := os.Stat(value)
			require.NoError(t, err, "the hermetic cache directory must exist during the run")
			require.True(t, info.IsDir())
		}
		return []byte(`{"Issues":[],"Report":{}}`), nil
	}

	runner := hermeticRunner(t, command, toolGolangci)
	for range 2 {
		_, err := runner.Run(context.Background(), ".")
		require.NoError(t, err)
	}

	require.Len(t, seen, 2, "each run must bind exactly one hermetic cache")
	want.NotEqual(seen[0], seen[1], "each invocation gets its own cache directory")
	for _, dir := range seen {
		want.True(filepath.IsAbs(dir), "the cache directory is absolute: %s", dir)
		_, statErr := os.Stat(dir)
		want.True(os.IsNotExist(statErr), "the cache directory is removed after the run: %s", dir)
	}
}

// TestYzeDefaultSpecPassesNoEnv pins the inverse: a spec with no Env template
// hands the subprocess a nil overlay, inheriting the ambient environment
// untouched.
func TestYzeDefaultSpecPassesNoEnv(t *testing.T) {
	var got [][]EnvVar
	command := func(_ context.Context, _ RunnerName, env []EnvVar, _ ...Arg) ([]byte, error) {
		got = append(got, env)
		return []byte(`{"diagnostics":[]}`), nil
	}

	_, err := hermeticRunner(t, command, toolYze).Run(context.Background(), ".")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Nil(t, got[0])
}

// TestResolveEnvCreationFailureSurfaces pins the fail-loud contract of the
// hermetic-cache seam: when the per-invocation cache directory cannot be
// created, the runner fails with ErrRunnerFailed instead of running the tool
// against ambient cache state.
func TestResolveEnvCreationFailureSurfaces(t *testing.T) {
	original := osMkdirTemp
	t.Cleanup(func() { osMkdirTemp = original })
	boom := errors.New("cannot create temp dir")
	osMkdirTemp = func(string, string) (string, error) { return "", boom }

	command := func(context.Context, RunnerName, []EnvVar, ...Arg) ([]byte, error) {
		t.Fatal("the tool must not run without its hermetic cache")
		return nil, nil
	}

	_, err := hermeticRunner(t, command, toolGolangci).Run(context.Background(), ".")
	assert.ErrorIs(t, err, ErrRunnerFailed)
	assert.ErrorIs(t, err, boom)
}
