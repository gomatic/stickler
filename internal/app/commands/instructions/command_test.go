package instructions_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	command "github.com/gomatic/stickler/internal/app/commands/instructions"
)

func TestCommandIsTheInstructionsVerb(t *testing.T) {
	t.Parallel()
	cmd := command.Command()

	assert.Equal(t, "instructions", cmd.Name)
	assert.Equal(t, command.Name, cmd.Name)
	assert.NotNil(t, cmd.Action, "a command with no action binds nothing")
	assert.Contains(t, cmd.Description, "stickler instructions", "the help shows how to run it")
}

// TestEveryFlagBindsAnEnvironmentVariable pins the CLI standard: a flag with no
// environment source cannot be set in CI without editing the invocation.
func TestEveryFlagBindsAnEnvironmentVariable(t *testing.T) {
	t.Parallel()
	flags := command.Command().Flags
	require.NotEmpty(t, flags)
	for _, f := range flags {
		assert.NotEmpty(t, f.(cli.DocGenerationFlag).GetEnvVars(), "flag %s binds no environment variable", f.Names())
	}
}

// TestRootDefaultsToUnset pins that the flag does not pre-empt the working
// directory the domain falls back to.
func TestRootDefaultsToUnset(t *testing.T) {
	t.Parallel()
	flag, ok := command.Command().Flags[0].(*cli.StringFlag)
	require.True(t, ok)
	assert.Empty(t, flag.Value)
}
