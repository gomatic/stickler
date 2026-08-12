package instructions

// The document is assembled through the same seams a lint pass uses: fake
// runners instead of subprocesses, canned config instead of files on disk.

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	errs "github.com/gomatic/go-error"
	goyze "github.com/gomatic/go-yze"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gomatic/stickler/internal/config"
	"github.com/gomatic/stickler/internal/constants"
	"github.com/gomatic/stickler/internal/suite"
)

// speaking is a check that states its rules.
type speaking struct {
	name string
	text string
}

func (s speaking) Name() string { return s.name }

func (speaking) Run(context.Context, suite.Root) ([]goyze.Diagnostic, error) { return nil, nil }

func (s speaking) Explain(context.Context) (suite.Instructions, error) {
	return suite.Instructions(s.text), nil
}

// mute is a check with no way to state its rules at all.
type mute struct{ name string }

func (m mute) Name() string { return m.name }

func (mute) Run(context.Context, suite.Root) ([]goyze.Diagnostic, error) { return nil, nil }

// refusing implements Explainer but fails when asked.
type refusing struct{ name string }

func (r refusing) Name() string { return r.name }

func (refusing) Run(context.Context, suite.Root) ([]goyze.Diagnostic, error) { return nil, nil }

func (refusing) Explain(context.Context) (suite.Instructions, error) {
	return "", errs.Const("tool refused")
}

// swapChecks substitutes the assembled runners.
func swapChecks(t *testing.T, runners ...suite.Runner) {
	t.Helper()
	original := assemble
	t.Cleanup(func() { assemble = original })
	assemble = func(config.RepoRoot) ([]suite.Runner, error) { return runners, nil }
}

// document renders one pass.
func document(t *testing.T) (string, Result, error) {
	t.Helper()
	var buf bytes.Buffer
	cfg := Config{Out: &buf}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	result, err := Run(context.Background(), logger, cfg)
	return buf.String(), result, err
}

// TestEveryCheckSpeaksInRuleOrder pins the assembled document: one section per
// check that can state its rules, ordered so the output does not depend on map
// iteration.
func TestEveryCheckSpeaksInRuleOrder(t *testing.T) {
	swapChecks(t, speaking{name: "zeta", text: "## zeta rules"}, speaking{name: "alpha", text: "## alpha rules"})

	out, result, err := document(t)

	require.NoError(t, err)
	require.Len(t, result.Sections, 2)
	assert.Equal(t, "alpha", result.Sections[0].Runner, "sections are ordered by runner")
	assert.Less(t, strings.Index(out, "alpha rules"), strings.Index(out, "zeta rules"))
	assert.Contains(t, out, "# How this repository is checked", "the model comes before the rules")
}

// TestRunNamesASilentCheckRatherThanOmittingIt is the contract that keeps this
// document honest. A reader has to know which rules it has NOT been told
// about; quietly rendering a shorter document would present a partial standard
// as the whole one, which is worse than an admitted gap.
func TestRunNamesASilentCheckRatherThanOmittingIt(t *testing.T) {
	swapChecks(t, speaking{name: "alpha", text: "## alpha rules"}, mute{name: "quiet"}, refusing{name: "refuser"})

	out, result, err := document(t)

	require.NoError(t, err)
	assert.Equal(t, []string{"quiet", "refuser"}, result.Silent)
	assert.Contains(t, out, "Checks that did not explain themselves")
	assert.Contains(t, out, "- `quiet`")
	assert.Contains(t, out, "- `refuser`", "a tool that refuses is as silent as one that cannot")
}

// TestAskRefusesARunnerThatStatesNoInstructions names ask's contract and the
// sentinel behind it: a runner carrying no Explainer is refused with a
// matchable reason, which is what puts it in the silent column instead of
// contributing an empty section that would read as "this check has no rules".
func TestAskRefusesARunnerThatStatesNoInstructions(t *testing.T) {
	t.Parallel()

	_, err := ask(context.Background(), mute{name: "quiet"})

	require.ErrorIs(t, err, errNotAnExplainer)

	spoke, err := ask(context.Background(), speaking{name: "alpha", text: "## alpha rules"})
	require.NoError(t, err)
	assert.Equal(t, suite.Instructions("## alpha rules"), spoke)
}

// TestNoSilentSectionWhenEveryCheckSpoke pins that the caveat only appears when
// it means something.
func TestNoSilentSectionWhenEveryCheckSpoke(t *testing.T) {
	swapChecks(t, speaking{name: "alpha", text: "## alpha rules"})

	out, result, err := document(t)

	require.NoError(t, err)
	assert.Empty(t, result.Silent)
	assert.NotContains(t, out, "did not explain themselves")
}

// TestConfigurationErrorSurfaces pins that a broken .stickler.yaml fails rather
// than producing a document describing checks that were never selected.
func TestConfigurationErrorSurfaces(t *testing.T) {
	original := readFile
	t.Cleanup(func() { readFile = original })
	readFile = func(string) ([]byte, error) { return []byte("runners: : :\n"), nil }

	_, _, err := document(t)

	assert.ErrorIs(t, err, constants.ErrConfig)
}

// TestWriteErrorSurfaces pins that a failed write is reported rather than
// silently truncating the standard — at EVERY point the document is written,
// because a reader cannot tell a short document from a complete one.
func TestWriteErrorSurfaces(t *testing.T) {
	swapChecks(t, speaking{name: "alpha", text: "## alpha rules"}, mute{name: "quiet"})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	for _, after := range []int{0, 1, 2} {
		_, err := Run(context.Background(), logger, Config{Out: &failingWriter{after: after}})
		require.Error(t, err, "a write failing at position %d must surface", after)
	}
}

// failingWriter accepts a fixed number of writes, then refuses — so each stage
// of the document (preamble, a section, the silent caveat) can be failed in
// turn.
type failingWriter struct {
	after int
	seen  int
}

func (w *failingWriter) Write(p []byte) (int, error) {
	if w.seen >= w.after {
		return 0, errs.Const("io failed")
	}
	w.seen++
	return len(p), nil
}

// TestRootOrDefaultFallsBackToTheWorkingDirectory pins the default target: the
// .stickler.yaml a developer standing in a repository means.
func TestRootOrDefaultFallsBackToTheWorkingDirectory(t *testing.T) {
	t.Parallel()
	assert.Equal(t, config.RepoRoot("."), rootOrDefault(""))
	assert.Equal(t, config.RepoRoot("/repo"), rootOrDefault("/repo"))
}

// TestWriterDefaultsToStandardOutput pins the default destination.
func TestWriterDefaultsToStandardOutput(t *testing.T) {
	t.Parallel()
	assert.NotNil(t, writer(nil))
	var buf bytes.Buffer
	assert.Equal(t, io.Writer(&buf), writer(&buf))
}

// TestAssembleReadsTheRealConfiguration pins the production path: the default
// assembly resolves configuration through the seams and builds the selected
// checks, so the document describes what this repository actually runs.
func TestAssembleReadsTheRealConfiguration(t *testing.T) {
	original := readFile
	t.Cleanup(func() { readFile = original })
	readFile = func(string) ([]byte, error) { return []byte("runners: [clitiers]\n"), nil }

	built, err := assemble(".")

	require.NoError(t, err)
	require.Len(t, built, 1)
	assert.Equal(t, "clitiers", built[0].Name())
}
