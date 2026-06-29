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

func TestYzeRunnerParsesSticklerJSON(t *testing.T) {
	out := `{"diagnostics":[{"tool":"yze","rule":"yze/gotostmt","path":"a.go","line":3,"col":2,"severity":"error","message":"goto is not permitted"}]}`
	runner := stickler.NewYzeRunner(fakeCommand(out, nil))

	assert.Equal(t, "yze", runner.Name())
	diags, err := runner.Run(context.Background(), ".")
	require.NoError(t, err)
	require.Len(t, diags, 1)
	assert.Equal(t, "yze/gotostmt", diags[0].Rule)
}

func TestYzeRunnerReportsExecFailureWhenOutputUnparseable(t *testing.T) {
	runner := stickler.NewYzeRunner(fakeCommand("", errs.Const("exec boom")))

	_, err := runner.Run(context.Background(), ".")

	require.Error(t, err)
	assert.True(t, errors.Is(err, stickler.ErrYzeFailed))
}

func TestYzeRunnerReportsParseFailureWithoutExecError(t *testing.T) {
	runner := stickler.NewYzeRunner(fakeCommand("{ not json", nil))

	_, err := runner.Run(context.Background(), ".")

	require.Error(t, err)
	assert.True(t, errors.Is(err, stickler.ErrYzeFailed))
}

func TestGolangciRunnerAdaptsIssues(t *testing.T) {
	out := `{"Issues":[
		{"FromLinter":"errcheck","Text":"unchecked error","Severity":"error","Pos":{"Filename":"a.go","Line":10,"Column":3}},
		{"FromLinter":"gosec","Text":"weak rand","Severity":"warning","Pos":{"Filename":"b.go","Line":4,"Column":1}}
	]}`
	runner := stickler.NewGolangciRunner(fakeCommand(out, errors.New("exit status 1")))

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

func TestGolangciRunnerMapsInfoAndDefaultSeverity(t *testing.T) {
	out := `{"Issues":[
		{"FromLinter":"a","Text":"x","Severity":"info","Pos":{"Filename":"a.go","Line":1,"Column":1}},
		{"FromLinter":"b","Text":"y","Severity":"","Pos":{"Filename":"a.go","Line":1,"Column":1}}
	]}`
	diags, err := stickler.NewGolangciRunner(fakeCommand(out, nil)).Run(context.Background(), ".")

	require.NoError(t, err)
	assert.Equal(t, goyze.SeverityInfo, diags[0].Severity)
	assert.Equal(t, goyze.SeverityError, diags[1].Severity)
}

func TestYzeRunnerCleanPassWithZeroExit(t *testing.T) {
	diags, err := stickler.NewYzeRunner(fakeCommand(`{"diagnostics":[]}`, nil)).Run(context.Background(), ".")

	require.NoError(t, err)
	assert.Empty(t, diags)
}

func TestYzeRunnerReturnsFindingsDespiteNonZeroExit(t *testing.T) {
	out := `{"diagnostics":[{"tool":"yze","rule":"yze/gotostmt","path":"a.go","line":3,"col":2,"severity":"error","message":"goto"}]}`
	diags, err := stickler.NewYzeRunner(fakeCommand(out, errors.New("exit status 1"))).Run(context.Background(), ".")

	require.NoError(t, err)
	require.Len(t, diags, 1)
	assert.Equal(t, "yze/gotostmt", diags[0].Rule)
}

func TestYzeRunnerSurfacesToolFailureWhenExitNonZeroAndNoFindings(t *testing.T) {
	// Valid-but-empty JSON with a non-zero exit means the tool failed for a
	// non-finding reason; it must not be reported as a clean pass.
	_, err := stickler.NewYzeRunner(fakeCommand(`{"diagnostics":[]}`, errs.Const("config boom"))).Run(context.Background(), ".")

	require.Error(t, err)
	assert.True(t, errors.Is(err, stickler.ErrYzeFailed))
}

func TestYzeRunnerInsertsDoubleDashBeforeRoot(t *testing.T) {
	var got []stickler.Arg
	_, err := stickler.NewYzeRunner(capturingCommand(`{"diagnostics":[]}`, nil, &got)).Run(context.Background(), "-x")

	require.NoError(t, err)
	assert.Equal(t, []stickler.Arg{"--format", "stickler-json", "--", "-x"}, got)
}

func TestGolangciRunnerCleanPassWithEmptyIssues(t *testing.T) {
	diags, err := stickler.NewGolangciRunner(fakeCommand(`{"Issues":[],"Report":{}}`, nil)).Run(context.Background(), ".")

	require.NoError(t, err)
	assert.Empty(t, diags)
}

func TestGolangciRunnerEmptyStdoutOnCleanRunIsNotAFailure(t *testing.T) {
	// Empty stdout with a zero exit is a clean, no-findings run (io.EOF on decode).
	diags, err := stickler.NewGolangciRunner(fakeCommand("", nil)).Run(context.Background(), ".")

	require.NoError(t, err)
	assert.Empty(t, diags)
}

func TestGolangciRunnerSurfacesToolFailureWhenExitNonZeroAndNoIssues(t *testing.T) {
	// A config error: empty stdout, non-zero exit. Must surface, not pass clean.
	_, err := stickler.NewGolangciRunner(fakeCommand("", errs.Const("invalid config"))).Run(context.Background(), ".")

	require.Error(t, err)
	assert.True(t, errors.Is(err, stickler.ErrGolangciFailed))
}

func TestGolangciRunnerSurfacesTopLevelReportError(t *testing.T) {
	out := `{"Issues":[],"Report":{"Error":"linter X panicked"}}`
	_, err := stickler.NewGolangciRunner(fakeCommand(out, nil)).Run(context.Background(), ".")

	require.Error(t, err)
	assert.True(t, errors.Is(err, stickler.ErrGolangciFailed))
	assert.Contains(t, err.Error(), "linter X panicked")
}

func TestGolangciRunnerToleratesTrailingSummaryFooter(t *testing.T) {
	// golangci-lint v2 appends a human summary after the JSON; the streaming decoder
	// (not json.Unmarshal, which rejects trailing data) reads the first value only.
	out := `{"Issues":[{"FromLinter":"errcheck","Text":"x","Severity":"error","Pos":{"Filename":"a.go","Line":1,"Column":1}}]}` +
		"\n1 issues:\n* errcheck: 1\n"
	diags, err := stickler.NewGolangciRunner(fakeCommand(out, errors.New("exit status 1"))).Run(context.Background(), ".")

	require.NoError(t, err)
	require.Len(t, diags, 1)
	assert.Equal(t, "errcheck", diags[0].Rule)
}

func TestGolangciRunnerInsertsDoubleDashBeforeRoot(t *testing.T) {
	var got []stickler.Arg
	_, err := stickler.NewGolangciRunner(capturingCommand(`{"Issues":[],"Report":{}}`, nil, &got)).Run(context.Background(), "-x")

	require.NoError(t, err)
	assert.Equal(t, []stickler.Arg{"run", "--output.json.path=stdout", "--", "-x"}, got)
}

func TestExecCommandSurfacesStderrInError(t *testing.T) {
	_, err := stickler.ExecCommand(context.Background(), "sh", "-c", "printf 'the real reason' 1>&2; exit 7")

	require.Error(t, err)
	assert.True(t, errors.Is(err, stickler.ErrExec))
	assert.Contains(t, err.Error(), "the real reason")
}

func TestGolangciRunnerReportsExecFailureWhenUnparseable(t *testing.T) {
	_, err := stickler.NewGolangciRunner(fakeCommand("", errs.Const("boom"))).Run(context.Background(), ".")
	require.Error(t, err)
	assert.True(t, errors.Is(err, stickler.ErrGolangciFailed))
}

func TestGolangciRunnerReportsParseFailureWithoutExecError(t *testing.T) {
	_, err := stickler.NewGolangciRunner(fakeCommand("nope", nil)).Run(context.Background(), ".")
	require.Error(t, err)
	assert.True(t, errors.Is(err, stickler.ErrGolangciFailed))
}

func TestExecCommandRunsRealProcess(t *testing.T) {
	out, err := stickler.ExecCommand(context.Background(), "go", "version")
	require.NoError(t, err)
	assert.Contains(t, string(out), "go version")

	_, err = stickler.ExecCommand(context.Background(), "stickler-no-such-binary-xyz")
	require.Error(t, err)
}

func TestBuildRunnersSelectsKnownAndIgnoresUnknown(t *testing.T) {
	runners := stickler.BuildRunners(fakeCommand("", nil), []string{"yze", "nope", "golangci-lint"})

	require.Len(t, runners, 2)
	assert.Equal(t, "yze", runners[0].Name())
	assert.Equal(t, "golangci-lint", runners[1].Name())
}

func TestBuildRunnersDefaultsToFullSet(t *testing.T) {
	runners := stickler.BuildRunners(fakeCommand("", nil), nil)

	require.Len(t, runners, 2)
}
