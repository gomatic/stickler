package main

import (
	"bytes"
	"context"
	"os"
	"testing"

	errs "github.com/gomatic/go-error"
	goyze "github.com/gomatic/go-yze"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gomatic/stickler"
)

type fakeRunner struct {
	err   error
	diags []goyze.Diagnostic
}

func (fakeRunner) Name() string { return "fake" }

func (f fakeRunner) Run(context.Context, string) ([]goyze.Diagnostic, error) {
	return f.diags, f.err
}

func swapRunners(t *testing.T, runners ...stickler.Runner) {
	t.Helper()
	original := runnersFor
	t.Cleanup(func() { runnersFor = original })
	runnersFor = func() []stickler.Runner { return runners }
}

func runApp(t *testing.T, args ...string) (string, error) {
	t.Helper()
	app := createApp()
	var buf bytes.Buffer
	app.Writer = &buf
	err := app.Run(context.Background(), args)
	return buf.String(), err
}

func TestActionCleanRunSucceeds(t *testing.T) {
	swapRunners(t, fakeRunner{})

	out, err := runApp(t, appName)

	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestActionReportsFindingsAndFails(t *testing.T) {
	swapRunners(t, fakeRunner{diags: []goyze.Diagnostic{
		{Path: "a.go", Line: 3, Col: 2, Severity: goyze.SeverityError, Message: "boom", Rule: "yze/go/gotostmt"},
	}})

	out, err := runApp(t, appName)

	require.Error(t, err)
	assert.Contains(t, out, "a.go:3:2: boom [error] (yze/go/gotostmt)")
}

func TestActionRunnerErrorFails(t *testing.T) {
	swapRunners(t, fakeRunner{err: errs.Const("tool crashed")})

	_, err := runApp(t, appName)

	require.Error(t, err)
}

func TestActionRejectsUnknownFormat(t *testing.T) {
	swapRunners(t, fakeRunner{})

	_, err := runApp(t, appName, "--format", "nope")

	require.Error(t, err)
}

func TestDefaultRunnersAreYzeAndGolangci(t *testing.T) {
	runners := defaultRunners()

	require.Len(t, runners, 2)
	assert.Equal(t, "yze", runners[0].Name())
	assert.Equal(t, "golangci-lint", runners[1].Name())
}

func TestRootOfDefaultsToModule(t *testing.T) {
	assert.Equal(t, "./...", rootOf(nil))
	assert.Equal(t, "pkg/x", rootOf([]string{"pkg/x"}))
}

func TestRunExitCodes(t *testing.T) {
	swapRunners(t, fakeRunner{})
	assert.Equal(t, 0, run([]string{appName}))

	swapRunners(t, fakeRunner{diags: []goyze.Diagnostic{{Path: "a.go", Message: "x"}}})
	assert.Equal(t, 1, run([]string{appName}))
}

func TestMainExits(t *testing.T) {
	swapRunners(t, fakeRunner{})
	originalExit, originalArgs := osExit, os.Args
	t.Cleanup(func() { osExit, os.Args = originalExit, originalArgs })

	var code int
	osExit = func(c int) { code = c }
	os.Args = []string{appName}

	main()

	assert.Equal(t, 0, code)
}
