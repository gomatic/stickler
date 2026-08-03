package stickler_test

import (
	"context"
	"errors"
	"testing"

	errs "github.com/gomatic/go-error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gomatic/stickler"
)

func fakeCommand(out string, err error) stickler.Command {
	return func(context.Context, stickler.RunnerName, []stickler.EnvVar, ...stickler.Arg) ([]byte, error) {
		return []byte(out), err
	}
}

// capturingCommand records the arguments it is called with so a test can assert
// how a runner builds its command line.
func capturingCommand(out string, err error, gotArgs *[]stickler.Arg) stickler.Command {
	return func(_ context.Context, _ stickler.RunnerName, _ []stickler.EnvVar, args ...stickler.Arg) ([]byte, error) {
		*gotArgs = args
		return []byte(out), err
	}
}

// runnerByName builds the single named runner from the default specs over command,
// with no config overlays (config-file wiring is covered white-box in configmerge_test).
func runnerByName(t *testing.T, command stickler.Command, name string) stickler.Runner {
	t.Helper()
	runners := stickler.BuildRunners(command, stickler.DefaultRunnerSpecs(), []string{name}, stickler.RunnerContext{})
	require.Len(t, runners, 1)
	return runners[0]
}

func TestYzeSpecReportsExecFailureWhenOutputUnparseable(t *testing.T) {
	runner := runnerByName(t, fakeCommand("", errs.Const("exec boom")), "yze")

	_, err := runner.Run(context.Background(), ".")

	require.Error(t, err)
	assert.True(t, errors.Is(err, stickler.ErrRunnerFailed))
}

func TestYzeSpecCleanPassWithZeroExit(t *testing.T) {
	diags, err := runnerByName(t, fakeCommand(`{"diagnostics":[]}`, nil), "yze").
		Run(context.Background(), ".")

	require.NoError(t, err)
	assert.Empty(t, diags)
}

func TestYzeSpecReturnsFindingsDespiteNonZeroExit(t *testing.T) {
	out := `{"diagnostics":[{"tool":"yze","rule":"yze/gotostmt","path":"a.go",` +
		`"line":3,"col":2,"severity":"error","message":"goto"}]}`
	runner := runnerByName(t, fakeCommand(out, errors.New("exit status 1")), "yze")
	diags, err := runner.Run(context.Background(), ".")

	require.NoError(t, err)
	require.Len(t, diags, 1)
	assert.Equal(t, "yze/gotostmt", diags[0].Rule)
}

func TestYzeSpecSurfacesToolFailureWhenExitNonZeroAndNoFindings(t *testing.T) {
	command := fakeCommand(`{"diagnostics":[]}`, errs.Const("config boom"))
	_, err := runnerByName(t, command, "yze").Run(context.Background(), ".")

	require.Error(t, err)
	assert.True(t, errors.Is(err, stickler.ErrRunnerFailed))
}

func TestYzeSpecSubstitutesRootAfterDoubleDash(t *testing.T) {
	var got []stickler.Arg
	_, err := runnerByName(t, capturingCommand(`{"diagnostics":[]}`, nil, &got), "yze").
		Run(context.Background(), "-x")

	require.NoError(t, err)
	assert.Equal(t, []stickler.Arg{"--format", "stickler-json", "--", "-x"}, got)
}

func TestBuildRunnersSelectsKnownAndIgnoresUnknown(t *testing.T) {
	runners := stickler.BuildRunners(
		fakeCommand("", nil),
		stickler.DefaultRunnerSpecs(),
		[]string{"yze", "nope", "golangci-lint"},
		stickler.RunnerContext{},
	)

	require.Len(t, runners, 2)
	assert.Equal(t, "yze", runners[0].Name())
	assert.Equal(t, "golangci-lint", runners[1].Name())
}

func TestBuildRunnersDefaultsToEveryDefinedSpecSorted(t *testing.T) {
	runners := stickler.BuildRunners(fakeCommand("", nil), stickler.DefaultRunnerSpecs(), nil, stickler.RunnerContext{})

	require.Len(t, runners, 4)
	assert.Equal(t, "binaries", runners[0].Name())
	assert.Equal(t, "clilayout", runners[1].Name())
	assert.Equal(t, "golangci-lint", runners[2].Name())
	assert.Equal(t, "yze", runners[3].Name())
}

func TestMergeSpecsOverridesDefaultAndAddsNew(t *testing.T) {
	defined := map[string]stickler.RunnerSpec{
		"yze":   {Name: "yze", Command: []string{"yze2"}, Format: stickler.ParserSticklerJSON},
		"extra": {Name: "extra", Command: []string{"extra"}, Format: stickler.ParserSticklerJSON},
	}
	merged := stickler.MergeSpecs(stickler.DefaultRunnerSpecs(), defined)

	require.Len(t, merged, 3)
	assert.Equal(t, []string{"yze2"}, merged["yze"].Command)
	assert.Equal(t, "extra", merged["extra"].Name)
	assert.Equal(t, []string{"golangci-lint", "run"}, merged["golangci-lint"].Command)
}
