package runner_test

import (
	"context"
	"errors"
	"testing"

	errs "github.com/gomatic/go-error"
	goyze "github.com/gomatic/go-yze"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gomatic/stickler/internal/config"
	"github.com/gomatic/stickler/internal/constants"
	"github.com/gomatic/stickler/internal/runner"
	"github.com/gomatic/stickler/internal/suite"
)

func fakeCommand(out string, err error) runner.Command {
	return func(context.Context, runner.Name, []runner.EnvVar, ...runner.Arg) ([]byte, error) {
		return []byte(out), err
	}
}

// capturingCommand records the arguments it is called with so a test can assert
// how a runner builds its command line.
func capturingCommand(out string, err error, gotArgs *[]runner.Arg) runner.Command {
	return func(_ context.Context, _ runner.Name, _ []runner.EnvVar, args ...runner.Arg) ([]byte, error) {
		*gotArgs = args
		return []byte(out), err
	}
}

// runnerByName builds the single named runner from the default specs over command,
// with no config overlays (config-file wiring is covered white-box in configmerge_test).
func runnerByName(t *testing.T, command runner.Command, name string) suite.Runner {
	t.Helper()
	runners, err := runner.Build(
		command,
		runner.Registry{Specs: config.DefaultRunnerSpecs()},
		[]string{name},
		runner.Context{},
	)
	require.NoError(t, err)
	require.Len(t, runners, 1)
	return runners[0]
}

func TestYzeSpecReportsExecFailureWhenOutputUnparseable(t *testing.T) {
	runner := runnerByName(t, fakeCommand("", errs.Const("exec boom")), "yze")

	_, err := runner.Run(context.Background(), ".")

	require.Error(t, err)
	assert.True(t, errors.Is(err, constants.ErrRunnerFailed))
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
	assert.True(t, errors.Is(err, constants.ErrRunnerFailed))
}

func TestYzeSpecSubstitutesRootAfterDoubleDash(t *testing.T) {
	var got []runner.Arg
	_, err := runnerByName(t, capturingCommand(`{"diagnostics":[]}`, nil, &got), "yze").
		Run(context.Background(), "-x")

	require.NoError(t, err)
	assert.Equal(t, []runner.Arg{"--format", "stickler-json", "--", "-x"}, got)
}

// TestBuildRunnersErrUnknownRunnerOnUnknownName pins the fail-closed contract: a
// selected runner that resolves to nothing is an error naming it, because a
// silent skip would let a repo ask for a check and pass greenly while it
// went unrun.
func TestBuildRunnersErrUnknownRunnerOnUnknownName(t *testing.T) {
	_, err := runner.Build(
		fakeCommand("", nil),
		runner.Registry{Specs: config.DefaultRunnerSpecs()},
		[]string{"yze", "nope", "golangci-lint"},
		runner.Context{},
	)

	require.Error(t, err)
	assert.True(t, errors.Is(err, constants.ErrUnknownRunner))
	assert.Contains(t, err.Error(), "nope")
}

// stubCheck is a stand-in native check: Build's contract is that it resolves
// whatever natives it is GIVEN, so the real checks are not this package's
// business to import.
type stubCheck string

func (s stubCheck) Name() string { return string(s) }

func (stubCheck) Run(context.Context, suite.Root) ([]goyze.Diagnostic, error) { return nil, nil }

func TestBuildRunnersDefaultsToEveryDefinedSpecSorted(t *testing.T) {
	runners, err := runner.Build(
		fakeCommand("", nil),
		runner.Registry{
			Specs:  config.DefaultRunnerSpecs(),
			Native: map[string]suite.Runner{"binaries": stubCheck("binaries"), "clilayout": stubCheck("clilayout")},
		},
		nil,
		runner.Context{},
	)

	require.NoError(t, err)
	require.Len(t, runners, 4)
	assert.Equal(t, "binaries", runners[0].Name())
	assert.Equal(t, "clilayout", runners[1].Name())
	assert.Equal(t, "golangci-lint", runners[2].Name())
	assert.Equal(t, "yze", runners[3].Name())
}

func TestMergeSpecsOverridesDefaultAndAddsNew(t *testing.T) {
	defined := map[string]config.RunnerSpec{
		"yze":   {Name: "yze", Command: []string{"yze2"}, Format: config.ParserSticklerJSON},
		"extra": {Name: "extra", Command: []string{"extra"}, Format: config.ParserSticklerJSON},
	}
	merged := config.MergeSpecs(config.DefaultRunnerSpecs(), defined)

	require.Len(t, merged, 3)
	assert.Equal(t, []string{"yze2"}, merged["yze"].Command)
	assert.Equal(t, "extra", merged["extra"].Name)
	assert.Equal(t, []string{"golangci-lint", "run"}, merged["golangci-lint"].Command)
}
