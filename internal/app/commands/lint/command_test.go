package lint_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	command "github.com/gomatic/stickler/internal/app/commands/lint"
	domain "github.com/gomatic/stickler/internal/domain/lint"
)

// flagNamed finds one flag on the command by name.
func flagNamed(t *testing.T, name string) cli.Flag {
	t.Helper()
	for _, f := range command.Command().Flags {
		if assert.ObjectsAreEqual([]string{name}, f.Names()[:1]) {
			return f
		}
	}
	require.Fail(t, "no such flag", name)
	return nil
}

func TestCommandIsTheLintVerb(t *testing.T) {
	t.Parallel()
	cmd := command.Command()

	assert.Equal(t, "lint", cmd.Name)
	assert.Equal(t, command.Name, cmd.Name, "the exported name is what main mounts as the default command")
	assert.NotNil(t, cmd.Action, "a command with no action binds nothing")
}

// TestEveryFlagBindsAnEnvironmentVariable pins the CLI standard: a flag with no
// environment source cannot be set in CI without editing the invocation.
func TestEveryFlagBindsAnEnvironmentVariable(t *testing.T) {
	t.Parallel()
	for _, f := range command.Command().Flags {
		assert.NotEmpty(t, f.(cli.DocGenerationFlag).GetEnvVars(), "flag %s binds no environment variable", f.Names())
	}
}

// TestTimeoutDefaultIsTheDomainConstant pins the bound to ONE number. Every
// runner is a subprocess, and a subprocess that never exits would otherwise
// hang the job until the CI platform's own timeout kills it — with no output
// and nothing naming the tool that wedged. Two places carrying the same number
// is how such a bound gets raised in one of them and silently stops applying.
func TestTimeoutDefaultIsTheDomainConstant(t *testing.T) {
	t.Parallel()
	flag, ok := flagNamed(t, "timeout").(*cli.DurationFlag)
	require.True(t, ok)

	assert.Equal(t, time.Duration(domain.DefaultTimeout), flag.Value,
		"the --timeout default must BE the domain constant, not a second copy of the number")
}

// TestFormatAndRootDefaultToUnset pins the precedence seam: both flags default
// empty so the resolved configuration, not the flag, decides — a non-empty
// default here would silently override every repository's .stickler.yaml.
func TestFormatAndRootDefaultToUnset(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"format", "root"} {
		flag, ok := flagNamed(t, name).(*cli.StringFlag)
		require.True(t, ok, name)
		assert.Empty(t, flag.Value, "--%s must default to unset so config can win", name)
	}
}
