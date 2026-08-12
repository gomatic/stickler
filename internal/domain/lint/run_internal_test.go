package lint

// A pass is exercised through its seams: fake runners instead of subprocesses,
// canned config bytes instead of files on disk. That is what makes every branch
// — a wedged linter, a broken config, a rule grown past its baseline —
// reachable without a repository built to provoke it.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	errs "github.com/gomatic/go-error"
	goyze "github.com/gomatic/go-yze"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gomatic/stickler/internal/config"
	"github.com/gomatic/stickler/internal/constants"
	"github.com/gomatic/stickler/internal/suite"
)

type fakeRunner struct {
	err   error
	diags []goyze.Diagnostic
}

func (fakeRunner) Name() string { return "fake" }

func (f fakeRunner) Run(context.Context, suite.Root) ([]goyze.Diagnostic, error) {
	return f.diags, f.err
}

// blockingRunner blocks until the context is cancelled, then returns the context
// error — it lets a test prove the overall timeout cancels a wedged linter.
type blockingRunner struct{}

func (blockingRunner) Name() string { return "block" }

func (blockingRunner) Run(ctx context.Context, _ suite.Root) ([]goyze.Diagnostic, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// swapReadFile substitutes the config reader with canned bytes (or a failure,
// which reads as "no such config layer").
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

// swapRunners substitutes the assembled runners, and leaves no config on disk.
func swapRunners(t *testing.T, runners ...suite.Runner) {
	t.Helper()
	original := buildRunners
	t.Cleanup(func() { buildRunners = original })
	buildRunners = func(config.Resolved, config.RepoRoot) ([]suite.Runner, error) {
		return runners, nil
	}
	swapReadFile(t, "", errs.Const("no config")) // hermetic: no config files
}

// discardLogger keeps a pass's log off the test output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// runPass executes one pass, capturing the rendered report.
func runPass(t *testing.T, cfg Config) (string, Result, error) {
	t.Helper()
	var buf bytes.Buffer
	cfg.Out = &buf
	result, err := Run(context.Background(), discardLogger(), cfg)
	return buf.String(), result, err
}

func TestRunCleanPassSucceeds(t *testing.T) {
	swapRunners(t, fakeRunner{})

	out, result, err := runPass(t, Config{})

	require.NoError(t, err)
	assert.Empty(t, out)
	assert.False(t, result.HasFailures)
}

func TestRunReportsFindingsAndFails(t *testing.T) {
	swapRunners(t, fakeRunner{diags: []goyze.Diagnostic{
		{Path: "a.go", Line: 3, Col: 2, Severity: goyze.SeverityError, Message: "boom", Rule: "yze/gotostmt"},
	}})

	out, result, err := runPass(t, Config{})

	require.ErrorIs(t, err, constants.ErrLintFailed)
	assert.Contains(t, out, "a.go:3:2: boom [error] (yze/gotostmt)")
	assert.True(t, result.HasFailures)
	assert.Len(t, result.Diagnostics, 1, "the findings come back as data, not only as text")
}

func TestRunRunnerErrorFails(t *testing.T) {
	swapRunners(t, fakeRunner{err: errs.Const("tool crashed")})

	_, result, err := runPass(t, Config{})

	require.Error(t, err)
	assert.NotEmpty(t, result.Errors, "a tool failure is reported, never swallowed")
}

func TestRunRejectsUnknownFormat(t *testing.T) {
	swapRunners(t, fakeRunner{})

	_, _, err := runPass(t, Config{Format: "nope"})

	assert.ErrorIs(t, err, constants.ErrUnknownOutput)
}

func TestRunUsesConfiguredRunnersAndFormat(t *testing.T) {
	var gotNames []string
	original := buildRunners
	t.Cleanup(func() { buildRunners = original })
	buildRunners = func(resolved config.Resolved, _ config.RepoRoot) ([]suite.Runner, error) {
		gotNames = resolved.Runners
		return []suite.Runner{
			fakeRunner{diags: []goyze.Diagnostic{{Path: "a.go", Rule: "yze/gotostmt", Message: "x"}}},
		}, nil
	}
	swapReadFile(t, "runners: [yze]\nformat: json\n", nil)

	out, _, err := runPass(t, Config{})

	require.Error(t, err) // findings -> fail
	assert.Equal(t, []string{"yze"}, gotNames)
	assert.Contains(t, out, `"diagnostics"`, "the configured format is honored")
}

func TestRunTimeoutCancelsWedgedRunner(t *testing.T) {
	swapRunners(t, blockingRunner{})

	out, _, err := runPass(t, Config{Timeout: Timeout(10 * time.Millisecond)})

	require.Error(t, err)
	assert.Contains(t, out, "context deadline exceeded", "the overall timeout must cancel a wedged linter")
}

func TestRunReportsConfigError(t *testing.T) {
	swapReadFile(t, "runners: : :\n", nil)

	_, _, err := runPass(t, Config{})

	assert.ErrorIs(t, err, constants.ErrConfig)
}

// TestRunFailsLoudOnUnknownRunner pins the wiring end to end: a config
// selecting a runner nothing defines is an execution error naming it, not a
// silent pass over zero runners.
func TestRunFailsLoudOnUnknownRunner(t *testing.T) {
	swapReadFile(t, "runners: [no-such-runner]\n", nil)

	_, _, err := runPass(t, Config{})

	require.ErrorIs(t, err, constants.ErrUnknownRunner)
	assert.Contains(t, err.Error(), "no-such-runner")
}

// TestErrLintFailedSeparatesFindingsFromBreakage names the signal: it means
// "the pass ran correctly and found problems", as opposed to "the pass itself
// broke". A CI job that cannot tell them apart either ignores real findings or
// treats a crashed linter as a clean run.
func TestErrLintFailedSeparatesFindingsFromBreakage(t *testing.T) {
	swapRunners(t, fakeRunner{diags: []goyze.Diagnostic{{Path: "a.go", Message: "x"}}})
	_, _, findings := runPass(t, Config{})
	assert.ErrorIs(t, findings, constants.ErrLintFailed)

	swapRunners(t, fakeRunner{err: errs.Const("tool crashed")})
	_, _, broke := runPass(t, Config{})
	require.Error(t, broke)

	assert.NotErrorIs(t, errors.New("some other failure"), constants.ErrLintFailed,
		"an unrelated error must not be mistaken for a lint failure")
}

// TestRunSoftFindingsGateOnlyOnceTheyGrowPastTheBaseline pins the ratchet: a
// soft rule inside its committed baseline is reported and does not gate; one
// finding more gates and names the rule with both counts.
func TestRunSoftFindingsGateOnlyOnceTheyGrowPastTheBaseline(t *testing.T) {
	soft := goyze.Diagnostic{Path: "a.go", Rule: "yze/invariant", Message: "claims a property"}
	const layer = "soft: [yze/invariant]\nsoft-baseline:\n  yze/invariant: 1\n"

	swapRunners(t, fakeRunner{diags: []goyze.Diagnostic{soft}})
	swapReadFile(t, layer, nil)
	out, within, err := runPass(t, Config{})
	require.NoError(t, err, "a soft finding within its baseline does not gate")
	assert.Contains(t, out, "claims a property", "but it is still reported")
	assert.Empty(t, within.Overages)

	swapRunners(t, fakeRunner{diags: []goyze.Diagnostic{soft, soft}})
	swapReadFile(t, layer, nil)
	_, grown, err := runPass(t, Config{})
	require.ErrorIs(t, err, constants.ErrLintFailed, "growth past the baseline gates")
	require.Len(t, grown.Overages, 1)
	assert.Equal(t, suite.Overage{Rule: "yze/invariant", Count: 2, Baseline: 1}, grown.Overages[0])
}

// TestReportOveragesNamesEachGrownRule pins that an overage report says which
// rule grew and by how much. A bare "failed" would leave the reader to diff two
// counts by hand, and a ratchet nobody can read is a ratchet nobody acts on.
func TestReportOveragesNamesEachGrownRule(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	reportOverages(slog.New(slog.NewTextHandler(&out, nil)), []suite.Overage{
		{Rule: "yze/invariant", Count: 5, Baseline: 3},
		{Rule: "yze/filesize", Count: 2, Baseline: 0},
	})

	got := out.String()
	assert.Contains(t, got, `rule=yze/invariant findings=5 baseline=3`)
	assert.Contains(t, got, `rule=yze/filesize findings=2 baseline=0`)
}

// TestReportOveragesIsSilentWhenNothingGrew pins that a run inside its baseline
// says nothing, so the report only ever appears when it means something.
func TestReportOveragesIsSilentWhenNothingGrew(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	reportOverages(slog.New(slog.NewTextHandler(&out, nil)), nil)

	assert.Empty(t, out.String())
}

// TestMessagesRendersEveryRunnerError pins that a runner failure survives the
// crossing into data: a Result handed to a protocol carries no live error
// values, so the message is all that is left to carry the reason.
func TestMessagesRendersEveryRunnerError(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"first", "second"},
		messages([]error{errs.Const("first"), errs.Const("second")}))
	assert.Empty(t, messages(nil))
}
