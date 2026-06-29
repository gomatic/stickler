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
	return func(context.Context, string, ...string) ([]byte, error) { return []byte(out), err }
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
