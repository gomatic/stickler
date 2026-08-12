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
)

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

func TestYzeSpecReportsParseFailureWithoutExecError(t *testing.T) {
	runner := runnerByName(t, fakeCommand("{ not json", nil), "yze")

	_, err := runner.Run(context.Background(), ".")

	require.Error(t, err)
	assert.True(t, errors.Is(err, constants.ErrRunnerFailed))
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
	assert.True(t, errors.Is(err, constants.ErrRunnerFailed))
}

func TestGolangciSpecReportsParseFailureOnMalformedJSON(t *testing.T) {
	// Non-empty, non-JSON stdout is a real decode error (not the clean io.EOF path).
	_, err := runnerByName(t, fakeCommand("nope", nil), "golangci-lint").Run(context.Background(), ".")

	require.Error(t, err)
	assert.True(t, errors.Is(err, constants.ErrRunnerFailed))
}

func TestGolangciSpecSurfacesTopLevelReportError(t *testing.T) {
	out := `{"Issues":[],"Report":{"Error":"linter X panicked"}}`
	_, err := runnerByName(t, fakeCommand(out, nil), "golangci-lint").
		Run(context.Background(), ".")

	require.Error(t, err)
	assert.True(t, errors.Is(err, constants.ErrRunnerFailed))
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
	var got []runner.Arg
	command := capturingCommand(`{"Issues":[],"Report":{}}`, nil, &got)
	_, err := runnerByName(t, command, "golangci-lint").Run(context.Background(), "-x")

	require.NoError(t, err)
	assert.Equal(t, []runner.Arg{"run", "--output.json.path=stdout", "--allow-parallel-runners", "--", "-x"}, got)
}

func TestBuildRunnersFailsLoudOnUnknownParser(t *testing.T) {
	specs := config.MergeSpecs(config.DefaultRunnerSpecs(), map[string]config.RunnerSpec{
		"custom": {Name: "custom", Command: []string{"custom"}, Format: "no-such-parser"},
	})
	_, err := runner.Build(
		fakeCommand("", nil),
		runner.Registry{Specs: specs},
		[]string{"custom", "yze"},
		runner.Context{},
	)

	require.Error(t, err)
	assert.True(t, errors.Is(err, constants.ErrUnknownRunner))
	assert.Contains(t, err.Error(), "custom")
}
