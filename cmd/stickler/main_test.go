package main

import (
	"bytes"
	"context"
	"os"
	"testing"

	app "github.com/gomatic/go-app"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	"github.com/gomatic/stickler/internal/app/commands/lint"
)

// runApp executes the assembled CLI, capturing what it writes.
func runApp(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cliApp := createApp(app.GetLogger)
	var buf bytes.Buffer
	cliApp.Writer = &buf
	err := cliApp.Run(context.Background(), args)
	return buf.String(), err
}

func TestVersionFlagReportsVersion(t *testing.T) {
	out, err := runApp(t, name, "--version")

	require.NoError(t, err)
	assert.Contains(t, out, version)
}

// TestBareInvocationRunsLint pins the compatibility contract every CI job
// depends on: `stickler` and `stickler <root>` still mean a lint pass now that
// the tool has a command tree, because urfave prepends the default command to
// the positional arguments.
func TestBareInvocationRunsLint(t *testing.T) {
	t.Parallel()
	cliApp := createApp(app.GetLogger)

	assert.Equal(t, lint.Name, cliApp.DefaultCommand,
		"without this, a bare `stickler` would print help and exit zero — a gate that silently stopped gating")
	assert.NotNil(t, cliApp.Command(lint.Name), "the default command must exist")
}

// TestGlobalFlagsAreSortedAndBindEnvironment pins the uniform root: the shared
// logging flags, each reachable from the environment, in a stable help order.
func TestGlobalFlagsAreSortedAndBindEnvironment(t *testing.T) {
	t.Parallel()
	flags := createApp(app.GetLogger).Flags
	require.Len(t, flags, 2)

	assert.Equal(t, "log-format", flags[0].Names()[0], "flags are sorted for a stable --help")
	assert.Equal(t, "log-level", flags[1].Names()[0])
	for _, f := range flags {
		assert.NotEmpty(t, f.(cli.DocGenerationFlag).GetEnvVars())
	}
}

// TestBeforeHookPublishesTheLogger pins the convention every command action
// reads through: the logger is built after flag parsing and left in the root
// metadata, so `--log-level` reaches the domain tier.
func TestBeforeHookPublishesTheLogger(t *testing.T) {
	cliApp := createApp(loggerCreator)
	require.NotNil(t, cliApp.Before)
	// urfave's own setup does this before it calls Before (command_setup.go);
	// the hook is exercised here without a full run, so the test establishes
	// the same precondition.
	cliApp.Metadata = map[string]any{}

	_, err := cliApp.Before(context.Background(), cliApp)

	require.NoError(t, err)
	assert.NotNil(t, cliApp.Metadata[app.LoggerMetadataKey])
	assert.NotNil(t, productionLogger(cliApp), "the production logger is buildable")
}

func TestRunExitCodes(t *testing.T) {
	assert.Equal(t, 0, run([]string{name, "--version"}))
	assert.Equal(t, 1, run([]string{name, "--not-a-flag"}), "a failing run must exit non-zero")
}

func TestMainExits(t *testing.T) {
	originalExit, originalArgs, originalCreator := osExit, os.Args, appCreator
	t.Cleanup(func() { osExit, os.Args, appCreator = originalExit, originalArgs, originalCreator })

	var code int
	osExit = func(c int) { code = c }
	os.Args = []string{name, "--version"}
	appCreator = createApp

	main()

	assert.Equal(t, 0, code)
}
