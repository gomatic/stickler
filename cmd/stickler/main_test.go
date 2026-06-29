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

func swapReadFile(t *testing.T, content string, err error) {
	t.Helper()
	original := readFile
	t.Cleanup(func() { readFile = original })
	readFile = func(string) ([]byte, error) {
		if err != nil {
			return nil, err
		}
		return []byte(content), nil
	}
}

func swapRunners(t *testing.T, runners ...stickler.Runner) {
	t.Helper()
	original := buildRunners
	t.Cleanup(func() { buildRunners = original })
	buildRunners = func([]string) []stickler.Runner { return runners }
	swapReadFile(t, "", errs.Const("no config")) // hermetic: no config files
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
		{Path: "a.go", Line: 3, Col: 2, Severity: goyze.SeverityError, Message: "boom", Rule: "yze/gotostmt"},
	}})

	out, err := runApp(t, appName)

	require.Error(t, err)
	assert.Contains(t, out, "a.go:3:2: boom [error] (yze/gotostmt)")
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

func TestActionUsesConfiguredRunnersAndFormat(t *testing.T) {
	var gotNames []string
	originalBuild := buildRunners
	t.Cleanup(func() { buildRunners = originalBuild })
	buildRunners = func(names []string) []stickler.Runner {
		gotNames = names
		return []stickler.Runner{fakeRunner{diags: []goyze.Diagnostic{{Path: "a.go", Rule: "yze/gotostmt", Message: "x"}}}}
	}
	swapReadFile(t, "runners: [yze]\nformat: json\n", nil)

	out, err := runApp(t, appName)

	require.Error(t, err) // findings -> fail
	assert.Equal(t, []string{"yze"}, gotNames)
	assert.Contains(t, out, `"diagnostics"`) // config format json
}

func TestActionReportsConfigError(t *testing.T) {
	swapReadFile(t, "runners: : :\n", nil)

	_, err := runApp(t, appName)

	require.Error(t, err)
}

func TestDefaultBuildRunners(t *testing.T) {
	runners := defaultBuildRunners(nil)

	require.Len(t, runners, 2)
	assert.Equal(t, "yze", runners[0].Name())
	assert.Equal(t, "golangci-lint", runners[1].Name())
}

func TestConfigRoot(t *testing.T) {
	assert.Equal(t, "/explicit", configRoot("/explicit", "pkg/x"))
	assert.Equal(t, ".", configRoot("", "./..."))
	assert.Equal(t, "pkg/x", configRoot("", "pkg/x"))
}

func TestChooseFormat(t *testing.T) {
	assert.Equal(t, stickler.OutputFormat("github"), chooseFormat("github", "json"))
	assert.Equal(t, stickler.OutputFormat("json"), chooseFormat("", "json"))
	assert.Equal(t, stickler.OutputHuman, chooseFormat("", ""))
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
