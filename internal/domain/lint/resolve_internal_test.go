package lint

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	errs "github.com/gomatic/go-error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gomatic/stickler/internal/config"
	"github.com/gomatic/stickler/internal/report"
	"github.com/gomatic/stickler/internal/suite"
)

// TestBuildAssemblesEveryDeclaredToolAndNativeCheck pins the composition root:
// the native checks are injected HERE, so a pass over the defaults resolves all
// four in one deterministic order. Were the injection dropped, the two native
// checks would silently stop running and every repository would still go green.
func TestBuildAssemblesEveryDeclaredToolAndNativeCheck(t *testing.T) {
	runners, err := build(config.Resolved{}, ".")

	require.NoError(t, err)
	require.Len(t, runners, 4)
	assert.Equal(t, "binaries", runners[0].Name())
	assert.Equal(t, "clilayout", runners[1].Name())
	assert.Equal(t, "golangci-lint", runners[2].Name())
	assert.Equal(t, "yze", runners[3].Name())
}

func TestConfigRootPrefersTheExplicitFlag(t *testing.T) {
	t.Parallel()
	assert.Equal(t, config.RepoRoot("/explicit"), configRoot("/explicit", "pkg/x"))
	assert.Equal(t, config.RepoRoot("."), configRoot("", "./..."))
	assert.Equal(t, config.RepoRoot("pkg/x"), configRoot("", "pkg/x"))
}

func TestFormatPrecedenceIsFlagThenConfigThenHuman(t *testing.T) {
	t.Parallel()
	assert.Equal(t, report.OutputGitHub, format("github", "json"))
	assert.Equal(t, report.OutputJSON, format("", "json"))
	assert.Equal(t, report.OutputHuman, format("", ""))
}

func TestRootOfDefaultsToTheWholeModule(t *testing.T) {
	t.Parallel()
	assert.Equal(t, suite.Root("./..."), rootOf(nil))
	assert.Equal(t, suite.Root("pkg/x"), rootOf([]string{"pkg/x"}))
}

// TestWriterDefaultsToStandardOutput pins the default destination: a caller
// that names no writer gets the process's stdout, which is what makes the bare
// CLI invocation print its report.
func TestWriterDefaultsToStandardOutput(t *testing.T) {
	t.Parallel()
	assert.Equal(t, os.Stdout, writer(nil))

	var buf bytes.Buffer
	assert.Equal(t, io.Writer(&buf), writer(&buf))
}

// TestConfigureSkipsAnAbsentHomeDirectory pins that a machine with no
// resolvable home still lints: the global layer is simply absent.
func TestConfigureSkipsAnAbsentHomeDirectory(t *testing.T) {
	originalHome, originalEnv := userHomeDir, getenv
	t.Cleanup(func() { userHomeDir, getenv = originalHome, originalEnv })
	userHomeDir = func() (string, error) { return "", errs.Const("no home") }
	getenv = func(string) string { return "" }
	swapReadFile(t, "format: json\n", nil)

	resolved, err := configure(".")

	require.NoError(t, err)
	assert.Equal(t, "json", resolved.Format)
	assert.True(t, strings.HasSuffix(string(config.Paths(getenv, "", ".")[1]), ".stickler.yaml"))
}
