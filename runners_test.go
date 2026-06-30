package stickler_test

import (
	"context"
	"errors"
	"testing"

	errs "github.com/gomatic/go-error"
	goyze "github.com/gomatic/go-yze"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gomatic/stickler"
)

func fakeCommand(out string, err error) stickler.Command {
	return func(context.Context, stickler.RunnerName, ...stickler.Arg) ([]byte, error) {
		return []byte(out), err
	}
}

// capturingCommand records the arguments it is called with so a test can assert
// how a runner builds its command line.
func capturingCommand(out string, err error, gotArgs *[]stickler.Arg) stickler.Command {
	return func(_ context.Context, _ stickler.RunnerName, args ...stickler.Arg) ([]byte, error) {
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

func TestYzeSpecParsesSticklerJSON(t *testing.T) {
	out := `{"diagnostics":[{"tool":"yze","rule":"yze/gotostmt","path":"a.go",` +
		`"line":3,"col":2,"severity":"error","message":"goto is not permitted"}]}`
	runner := runnerByName(t, fakeCommand(out, nil), "yze")

	assert.Equal(t, "yze", runner.Name())
	diags, err := runner.Run(context.Background(), ".")
	require.NoError(t, err)
	require.Len(t, diags, 1)
	assert.Equal(t, "yze/gotostmt", diags[0].Rule)
}

func TestYzeSpecReportsExecFailureWhenOutputUnparseable(t *testing.T) {
	runner := runnerByName(t, fakeCommand("", errs.Const("exec boom")), "yze")

	_, err := runner.Run(context.Background(), ".")

	require.Error(t, err)
	assert.True(t, errors.Is(err, stickler.ErrRunnerFailed))
}

func TestYzeSpecReportsParseFailureWithoutExecError(t *testing.T) {
	runner := runnerByName(t, fakeCommand("{ not json", nil), "yze")

	_, err := runner.Run(context.Background(), ".")

	require.Error(t, err)
	assert.True(t, errors.Is(err, stickler.ErrRunnerFailed))
}

func TestGolangciSpecAdaptsIssues(t *testing.T) {
	out := `{"Issues":[
		{"FromLinter":"errcheck","Text":"unchecked error","Severity":"error","Pos":{"Filename":"a.go","Line":10,"Column":3}},
		{"FromLinter":"gosec","Text":"weak rand","Severity":"warning","Pos":{"Filename":"b.go","Line":4,"Column":1}}
	]}`
	runner := runnerByName(t, fakeCommand(out, errors.New("exit status 1")), "golangci-lint")

	assert.Equal(t, "golangci-lint", runner.Name())
	diags, err := runner.Run(context.Background(), ".")
	require.NoError(t, err)
	require.Len(t, diags, 2)
	assert.Equal(t, "errcheck", diags[0].Rule)
	assert.Equal(t, goyze.SeverityError, diags[0].Severity)
	assert.Equal(t, "golangci-lint", diags[0].Tool)
	assert.Equal(t, goyze.SeverityWarning, diags[1].Severity)
	assert.Equal(t, "b.go", diags[1].Path)
}

func TestGolangciSpecMapsInfoAndDefaultSeverity(t *testing.T) {
	out := `{"Issues":[
		{"FromLinter":"a","Text":"x","Severity":"info","Pos":{"Filename":"a.go","Line":1,"Column":1}},
		{"FromLinter":"b","Text":"y","Severity":"","Pos":{"Filename":"a.go","Line":1,"Column":1}}
	]}`
	diags, err := runnerByName(t, fakeCommand(out, nil), "golangci-lint").
		Run(context.Background(), ".")

	require.NoError(t, err)
	assert.Equal(t, goyze.SeverityInfo, diags[0].Severity)
	assert.Equal(t, goyze.SeverityError, diags[1].Severity)
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

func TestGolangciSpecCleanPassWithEmptyIssues(t *testing.T) {
	command := fakeCommand(`{"Issues":[],"Report":{}}`, nil)
	diags, err := runnerByName(t, command, "golangci-lint").Run(context.Background(), ".")

	require.NoError(t, err)
	assert.Empty(t, diags)
}

func TestGolangciSpecEmptyStdoutOnCleanRunIsNotAFailure(t *testing.T) {
	diags, err := runnerByName(t, fakeCommand("", nil), "golangci-lint").
		Run(context.Background(), ".")

	require.NoError(t, err)
	assert.Empty(t, diags)
}

func TestGolangciSpecSurfacesToolFailureWhenExitNonZeroAndNoIssues(t *testing.T) {
	_, err := runnerByName(t, fakeCommand("", errs.Const("invalid config")), "golangci-lint").
		Run(context.Background(), ".")

	require.Error(t, err)
	assert.True(t, errors.Is(err, stickler.ErrRunnerFailed))
}

func TestGolangciSpecReportsParseFailureOnMalformedJSON(t *testing.T) {
	// Non-empty, non-JSON stdout is a real decode error (not the clean io.EOF path).
	_, err := runnerByName(t, fakeCommand("nope", nil), "golangci-lint").Run(context.Background(), ".")

	require.Error(t, err)
	assert.True(t, errors.Is(err, stickler.ErrRunnerFailed))
}

func TestGolangciSpecSurfacesTopLevelReportError(t *testing.T) {
	out := `{"Issues":[],"Report":{"Error":"linter X panicked"}}`
	_, err := runnerByName(t, fakeCommand(out, nil), "golangci-lint").
		Run(context.Background(), ".")

	require.Error(t, err)
	assert.True(t, errors.Is(err, stickler.ErrRunnerFailed))
	assert.Contains(t, err.Error(), "linter X panicked")
}

func TestGolangciSpecToleratesTrailingSummaryFooter(t *testing.T) {
	out := `{"Issues":[{"FromLinter":"errcheck","Text":"x","Severity":"error",` +
		`"Pos":{"Filename":"a.go","Line":1,"Column":1}}]}` +
		"\n1 issues:\n* errcheck: 1\n"
	command := fakeCommand(out, errors.New("exit status 1"))
	diags, err := runnerByName(t, command, "golangci-lint").Run(context.Background(), ".")

	require.NoError(t, err)
	require.Len(t, diags, 1)
	assert.Equal(t, "errcheck", diags[0].Rule)
}

func TestGolangciSpecSubstitutesRootAndDropsConfigWhenNoOverlay(t *testing.T) {
	var got []stickler.Arg
	command := capturingCommand(`{"Issues":[],"Report":{}}`, nil, &got)
	_, err := runnerByName(t, command, "golangci-lint").Run(context.Background(), "-x")

	require.NoError(t, err)
	assert.Equal(t, []stickler.Arg{"run", "--output.json.path=stdout", "--", "-x"}, got)
}

func TestExecCommandSurfacesStderrInError(t *testing.T) {
	_, err := stickler.ExecCommand(context.Background(), "sh", "-c", "printf 'the real reason' 1>&2; exit 7")

	require.Error(t, err)
	assert.True(t, errors.Is(err, stickler.ErrExec))
	assert.Contains(t, err.Error(), "the real reason")
}

func TestExecCommandRunsRealProcess(t *testing.T) {
	out, err := stickler.ExecCommand(context.Background(), "go", "version")
	require.NoError(t, err)
	assert.Contains(t, string(out), "go version")

	_, err = stickler.ExecCommand(context.Background(), "stickler-no-such-binary-xyz")
	require.Error(t, err)
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

	require.Len(t, runners, 2)
	assert.Equal(t, "golangci-lint", runners[0].Name())
	assert.Equal(t, "yze", runners[1].Name())
}

func TestBuildRunnersSkipsSpecWithUnknownParser(t *testing.T) {
	specs := stickler.MergeSpecs(stickler.DefaultRunnerSpecs(), map[string]stickler.RunnerSpec{
		"custom": {Name: "custom", Command: []string{"custom"}, Format: "no-such-parser"},
	})
	runners := stickler.BuildRunners(fakeCommand("", nil), specs, []string{"custom", "yze"}, stickler.RunnerContext{})

	require.Len(t, runners, 1)
	assert.Equal(t, "yze", runners[0].Name())
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
