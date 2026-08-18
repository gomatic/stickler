package librarymarker

import (
	"context"
	"os"
	"testing"

	errs "github.com/gomatic/go-error"
	goyze "github.com/gomatic/go-yze"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gomatic/stickler/internal/suite"
)

// run walks one fixture repository.
func run(t *testing.T, root suite.Root) []goyze.Diagnostic {
	t.Helper()
	diags, err := Runner{}.Run(context.Background(), root)
	require.NoError(t, err)

	return diags
}

// TestMismatchedMarkerIsReported is the defect this check exists for,
// reproduced exactly as it occurred: a generator read the package clause from
// whichever .go file it listed first, struck the _test.go, and wrote
// `package command_test` into a file no compiler will ever parse.
//
// The fixture is deliberately a compiling repository. That is the whole point —
// nothing about this state is detectable by building, vetting, or analyzing it,
// because //go:build library_marker excludes the marker from the package's
// sources. It was found across 31 of the fleet's 96 markers only by reading
// them.
func TestMismatchedMarkerIsReported(t *testing.T) {
	t.Parallel()

	diags := run(t, "testdata/mismatched")

	require.Len(t, diags, 1)
	assert.Equal(t, Rule, diags[0].Rule)
	assert.Equal(t, "testdata/mismatched/library.go", diags[0].Path)
	assert.Equal(t, goyze.SeverityError, diags[0].Severity)
	assert.Equal(t, 3, diags[0].Line, "the finding points at the package clause, not the top of the file")
	assert.Contains(t, diags[0].Message, "`package command_test`", "the diagnostic quotes what is written")
	assert.Contains(t, diags[0].Message, "`command`", "and what it should say")
}

// TestCorrectMarkerIsSilent is the same repository after the one-word fix, and
// is what stops the check from being satisfiable only by deleting the marker.
func TestCorrectMarkerIsSilent(t *testing.T) {
	t.Parallel()

	assert.Empty(t, run(t, "testdata/correct"))
}

// TestRootlessMarkerIsSilent pins the limit of this check. A marker beside no
// root package has nothing to disagree with, and the fleet spells those three
// different ways — `library`, `git`, `hyt`. Reporting them would be enforcing a
// convention nobody has made, which is how a gate earns the habit of being
// ignored.
func TestRootlessMarkerIsSilent(t *testing.T) {
	t.Parallel()

	assert.Empty(t, run(t, "testdata/rootless"),
		"a marker with no root package to contradict is not this check's business")
}

// TestMarkerBesideOnlyATestFileIsSilent pins the same limit at the exact input
// that produced the bug. A root holding only a _test.go declares no package the
// marker could be wrong about; `package thing_test` is then simply what is
// there. This case is silent BECAUSE there is nothing to compare, not because
// the spelling is endorsed.
func TestMarkerBesideOnlyATestFileIsSilent(t *testing.T) {
	t.Parallel()

	assert.Empty(t, run(t, "testdata/testfileonly"))
}

// TestNoMarkerIsSilent pins that this check never asks whether a repository
// SHOULD have a marker. That is classification, and it belongs to the tooling
// that owns the marker.
func TestNoMarkerIsSilent(t *testing.T) {
	t.Parallel()

	assert.Empty(t, run(t, "testdata/nomarker"))
}

// TestReadDirFailureIsReportedRatherThanSilentlyClean pins that a root the check cannot list fails the
// run rather than passing it. A check that answered "clean" for a directory it
// never opened would be indistinguishable, at the exit code, from one it read
// and found correct — which is the one outcome a gate must never produce.
//
// The listing is stubbed because the branch cannot be staged from a fixture:
// reaching it at all requires library.go to have been parsed out of that same
// directory. It does not run in parallel with the other cases, because the seam
// is process-global.
func TestReadDirFailureIsReportedRatherThanSilentlyClean(t *testing.T) {
	const unreadable errs.Const = "permission denied"
	original := readDir
	t.Cleanup(func() { readDir = original })
	readDir = func(string) ([]os.DirEntry, error) { return nil, unreadable }

	_, err := Runner{}.Run(context.Background(), "testdata/mismatched")

	require.ErrorIs(t, err, unreadable, "the cause survives, rather than becoming a silent clean run")
}

// TestDisagreeingRootReportsTheFirstFileByName pins the determinism the listing
// is sorted for. A root whose files disagree about their package clause does not
// compile, and the compiler says so far better than this check could — but while
// somebody is fixing that, an unsorted check would read the directory in
// whatever order the filesystem handed it back and accuse the marker of
// mismatching a different package on each machine. alpha.go sorts before
// beta.go, so alpha wins here and on every other run.
func TestDisagreeingRootReportsTheFirstFileByName(t *testing.T) {
	diags := run(t, "testdata/disagreeing")

	require.Len(t, diags, 1)
	assert.Contains(t, diags[0].Message, "alpha")
	assert.NotContains(t, diags[0].Message, "beta")
}

// TestNameAndExplainAreTheRegistrySurface covers the two methods the suite
// calls on every check but no other test here reaches: the registry key it is
// stored under, and the instructions `stickler instructions` renders.
func TestNameAndExplainAreTheRegistrySurface(t *testing.T) {
	assert.Equal(t, Name, Runner{}.Name())

	explained, err := Runner{}.Explain(context.Background())

	require.NoError(t, err)
	assert.Equal(t, suite.Instructions(instructions), explained)
	assert.Contains(t, string(explained), Rule, "the instructions name the rule they explain")
}
