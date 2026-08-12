package runner

// The spec-runner's config-file path, exercised white-box: the merge itself is
// config's business, but WIRING a spec's declared config onto a merger, and
// passing the resulting flag through to the executed argv, is this package's.

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gomatic/stickler/internal/config"
	"github.com/gomatic/stickler/internal/constants"
)

// fakeReader serves canned file bytes; an unknown path is reported absent.
func fakeReader(files map[string][]byte) config.FileReader {
	return func(path string) ([]byte, error) {
		if data, ok := files[path]; ok {
			return data, nil
		}
		return nil, os.ErrNotExist
	}
}

// captureTemp records the bytes it is asked to write and returns a fixed path.
func captureTemp(captured *[]byte) config.TempWriter {
	return func(data []byte) (string, func(), error) {
		*captured = data
		return "/tmp/effective.yaml", func() {}, nil
	}
}

func TestSpecMergerWiresConfigFromSpec(t *testing.T) {
	want := assert.New(t)
	overlays := []config.Overlay{{"x": 1}}
	merger := specMerger(
		config.DefaultRunnerSpecs()["golangci-lint"],
		Context{BaseDir: "/repo", Config: map[string][]config.Overlay{"golangci-lint": overlays}},
		"golangci-lint",
	)
	want.Equal([]string{".golangci.yaml", ".golangci.yml"}, merger.BaseNames)
	want.Equal("--config={path}", merger.Flag)
	want.Equal("/repo", merger.BaseDir)
	want.Equal(overlays, merger.Overlays)
}

// TestSpecMergerLeavesTheFilesystemSeamsUnset pins the production wiring: the
// merger a spec produces names neither seam, so it reads and writes the real
// filesystem (config defaults them). Naming them here would be a second place
// the production filesystem is chosen.
func TestSpecMergerLeavesTheFilesystemSeamsUnset(t *testing.T) {
	merger := specMerger(config.DefaultRunnerSpecs()["yze"], Context{BaseDir: "/repo"}, "yze")
	assert.Nil(t, merger.Read)
	assert.Nil(t, merger.Temp)
}

func TestSpecMergerZeroWhenSpecHasNoConfig(t *testing.T) {
	want := assert.New(t)
	spec := config.RunnerSpec{Name: "plain", Command: []string{"plain"}, Format: config.ParserSticklerJSON}
	merger := specMerger(spec, Context{}, "plain")
	want.Empty(merger.BaseNames)
	want.Empty(merger.Flag)
	want.Empty(merger.Overlays)
}

// TestSpecMergerWiresYzeConfig pins that yze takes its configuration through
// the same generic merge path as every other tool: the per-analyzer settings
// a repository declares are useless unless they reach the analyzer suite.
func TestSpecMergerWiresYzeConfig(t *testing.T) {
	merger := specMerger(config.DefaultRunnerSpecs()["yze"], Context{BaseDir: "/repo"}, "yze")
	assert.Equal(t, []string{".yze.yaml", ".yze.yml"}, merger.BaseNames)
	assert.Equal(t, "--config={path}", merger.Flag)
}

// golangciSpecRunner builds a generic specRunner wired to the golangci parser and a
// given merger, exercising the config-file path white-box with injected seams.
func golangciSpecRunner(command Command, merger config.Merger) specRunner {
	return specRunner{
		spec: config.RunnerSpec{
			Name:    "golangci-lint",
			Command: []string{"golangci-lint", "run"},
			Args:    []string{"--output.json.path=stdout", config.PlaceholderConfig, "--", config.PlaceholderRoot},
			Format:  config.ParserGolangciJSON,
		},
		command: command,
		parser:  parseGolangciJSON,
		merger:  merger,
	}
}

func TestGolangciRunnerSurfacesEffectiveConfigError(t *testing.T) {
	merger := config.Merger{
		BaseNames: []string{".golangci.yaml"},
		Overlays:  []config.Overlay{{"x": 1}},
		BaseDir:   "/repo",
		Read:      fakeReader(map[string][]byte{"/repo/.golangci.yaml": []byte("\tbad")}),
		Temp:      captureTemp(new([]byte)),
	}
	command := func(context.Context, Name, []EnvVar, ...Arg) ([]byte, error) { return nil, nil }
	_, err := golangciSpecRunner(command, merger).Run(context.Background(), ".")
	assert.ErrorIs(t, err, constants.ErrRunnerFailed)
}

func TestGolangciRunnerPassesConfigFlag(t *testing.T) {
	want := assert.New(t)
	var gotArgs []Arg
	merger := config.Merger{
		BaseNames: []string{".golangci.yaml"},
		Flag:      "--config={path}",
		Overlays:  []config.Overlay{{"run": map[string]any{"timeout": "9m"}}},
		BaseDir:   "/repo",
		Read:      fakeReader(nil),
		Temp:      captureTemp(new([]byte)),
	}
	command := func(_ context.Context, _ Name, _ []EnvVar, args ...Arg) ([]byte, error) {
		gotArgs = args
		return []byte(`{"Issues":[],"Report":{}}`), nil
	}

	_, err := golangciSpecRunner(command, merger).Run(context.Background(), "./...")

	want.NoError(err)
	want.Equal([]Arg{"run", "--output.json.path=stdout", "--config=/tmp/effective.yaml", "--", "./..."}, gotArgs)
}

// TestExplainRunsTheDeclaredInstructionsInvocation pins that a tool's own
// catalog is what stickler prints: the declared arguments are appended to the
// tool's command and its stdout is returned verbatim. Anything else would mean
// stickler keeping prose about a tool it does not own.
func TestExplainRunsTheDeclaredInstructionsInvocation(t *testing.T) {
	var got []Arg
	command := func(_ context.Context, name Name, _ []EnvVar, args ...Arg) ([]byte, error) {
		assert.Equal(t, Name("yze"), name)
		got = args
		return []byte("# yze rule catalog\n"), nil
	}
	spec := config.DefaultRunnerSpecs()["yze"]
	runner, ok := newSpecRunner(command, spec, "yze", Context{})
	require.True(t, ok)

	text, err := runner.(specRunner).Explain(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "# yze rule catalog\n", string(text))
	assert.Equal(t, []Arg{"--emit-rules", "grit"}, got, "the declared invocation, not a guess")
}

// TestExplainRefusesWhenTheToolDeclaresNoInstructions pins the honest gap: a
// spec that declares no catalog invocation cannot explain itself, and says so
// rather than contributing an empty section that would read as "no rules".
func TestExplainRefusesWhenTheToolDeclaresNoInstructions(t *testing.T) {
	spec := config.RunnerSpec{Name: "quiet", Command: []string{"quiet"}, Format: config.ParserSticklerJSON}
	runner, ok := newSpecRunner(fakeExplainCommand, spec, "quiet", Context{})
	require.True(t, ok)

	_, err := runner.(specRunner).Explain(context.Background())

	assert.ErrorIs(t, err, constants.ErrNoInstructions)
}

// TestExplainSurfacesAToolFailure pins that a catalog invocation that fails is
// reported, not rendered as an empty rule set.
func TestExplainSurfacesAToolFailure(t *testing.T) {
	spec := config.DefaultRunnerSpecs()["yze"]
	failing := func(context.Context, Name, []EnvVar, ...Arg) ([]byte, error) {
		return nil, errors.New("tool crashed")
	}
	runner, ok := newSpecRunner(failing, spec, "yze", Context{})
	require.True(t, ok)

	_, err := runner.(specRunner).Explain(context.Background())

	assert.ErrorIs(t, err, constants.ErrRunnerFailed)
}

// TestExplainSurfacesAHermeticCacheFailure pins that the env seam's failure
// reaches the caller here too, rather than running the tool without it.
func TestExplainSurfacesAHermeticCacheFailure(t *testing.T) {
	original := osMkdirTemp
	t.Cleanup(func() { osMkdirTemp = original })
	osMkdirTemp = func(string, string) (string, error) { return "", errors.New("no space") }

	spec := config.DefaultRunnerSpecs()["golangci-lint"]
	spec.Instructions = []string{"help", "linters"}
	runner, ok := newSpecRunner(fakeExplainCommand, spec, "golangci-lint", Context{})
	require.True(t, ok)

	_, err := runner.(specRunner).Explain(context.Background())

	assert.ErrorIs(t, err, constants.ErrRunnerFailed)
}

// fakeExplainCommand stands in for a tool that prints nothing.
func fakeExplainCommand(context.Context, Name, []EnvVar, ...Arg) ([]byte, error) { return nil, nil }
