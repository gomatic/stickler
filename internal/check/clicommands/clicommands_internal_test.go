package clicommands

import (
	"context"
	"testing"

	goyze "github.com/gomatic/go-yze"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gomatic/stickler/internal/suite"
)

// run walks one fixture module.
func run(t *testing.T, root suite.Root) []goyze.Diagnostic {
	t.Helper()
	diags, err := Runner{}.Run(context.Background(), root)
	require.NoError(t, err)
	return diags
}

// TestFlatCLIIsReported is the case this check exists for, and the shape
// stickler itself shipped: a real urfave/cli program whose implementation
// lives anywhere but internal/app/commands. Every other layout rule passes it
// vacuously.
func TestFlatCLIIsReported(t *testing.T) {
	t.Parallel()

	diags := run(t, "testdata/flat")

	require.Len(t, diags, 1)
	assert.Equal(t, Rule, diags[0].Rule)
	assert.Equal(t, "testdata/flat/cmd/app/main.go", diags[0].Path)
	assert.Equal(t, goyze.SeverityError, diags[0].Severity)
	assert.Contains(t, diags[0].Message, "testdata/flat/cmd/app")
	assert.Contains(t, diags[0].Message, "internal/app/commands/<verb>",
		"the diagnostic must say what the fix is, not merely that something is wrong")
}

// TestTieredCLIIsSilent pins the passing case: a urfave program WITH a command
// tier reports nothing.
func TestTieredCLIIsSilent(t *testing.T) {
	t.Parallel()

	assert.Empty(t, run(t, "testdata/tiered"))
}

// TestSingleCheckerIsSilent pins the precision that makes this check usable at
// all. All 36 yze-go-* analyzer repositories are a `main` under cmd/ with no
// command tier; demanding tiers of them would be 36 false positives and would
// train everyone to ignore the rule. A framework-driver binary declares no
// verbs, so it is not a command tree.
func TestSingleCheckerIsSilent(t *testing.T) {
	t.Parallel()

	assert.Empty(t, run(t, "testdata/singlechecker"))
}

// TestSpecDriverIsSilent pins the discrimination the fleet sweep forced. A
// single-verb wrapper declares a spec, hands it to a framework driver, and
// imports urfave only to spell its flag types and a *cli.Command parameter. It
// constructs no command, so it owns no verbs and no tree. Keying this check on
// the urfave IMPORT instead reported 26 correct repositories.
func TestSpecDriverIsSilent(t *testing.T) {
	t.Parallel()

	assert.Empty(t, run(t, "testdata/specdriver"),
		"naming the type is not building one")
}

// TestAliasedImportIsFollowed pins that the discrimination survives an aliased
// import; if it did not, renaming the import would silently exempt a repo.
func TestAliasedImportIsFollowed(t *testing.T) {
	t.Parallel()

	require.Len(t, run(t, "testdata/aliased"), 1)
}

// TestOneProgramIsOneFinding pins the anchoring rule: a command tree split
// across several files of one main package is still ONE program missing ONE
// tier. Reporting per file would make a four-file main look four times as
// broken as a one-file main — the shape yupsh/commander has.
func TestOneProgramIsOneFinding(t *testing.T) {
	t.Parallel()

	diags := run(t, "testdata/multifile")

	require.Len(t, diags, 1, "one program, one finding")
	assert.Equal(t, "testdata/multifile/cmd/app/commands.go", diags[0].Path,
		"anchored at the package's first command-building file in walk order")
}

// TestSkipDirNeverJudgesFixturesInsideTestdata names skipDir's invariant. An
// analyzer repository keeps deliberately-broken layouts under testdata/ for its
// own tests to walk; judging those as the repository's own would report a
// finding nobody can fix and that no correct change would ever clear.
func TestSkipDirNeverJudgesFixturesInsideTestdata(t *testing.T) {
	t.Parallel()

	assert.Empty(t, run(t, "testdata/nested"),
		"a flat CLI inside testdata/ is somebody else's fixture, not this module's layout")
}

// TestLibraryIsSilent pins that a module with no program at all is not asked
// for a CLI layout.
func TestLibraryIsSilent(t *testing.T) {
	t.Parallel()

	assert.Empty(t, run(t, "testdata/library"))
}

// TestPackagePatternRootIsWalked pins that the check accepts the same "./..."
// target every other runner is given, rather than only a bare directory.
func TestPackagePatternRootIsWalked(t *testing.T) {
	t.Parallel()

	assert.Len(t, run(t, "testdata/flat/..."), 1)
}

// TestWalkErrorSurfaces pins the fail-loud contract: a root that cannot be
// walked is an error, never silence. A check that could not look must never
// report a pass.
func TestWalkErrorSurfaces(t *testing.T) {
	t.Parallel()

	_, err := Runner{}.Run(context.Background(), "testdata/does-not-exist")

	assert.Error(t, err)
}

// TestNameAndRuleAreStable pins the identifiers a repository's soft list and
// baseline are written against; changing either silently un-softens the rule
// everywhere it was named.
func TestNameAndRuleAreStable(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "clicommands", Runner{}.Name())
	assert.Equal(t, "stickler/clicommands", Rule)
}

// TestAnalyzerRepoShapeIsReported is the defect this second rule was added for,
// reproduced exactly: the whole implementation in an importable root package,
// consumed as a library by the yze suite, PLUS a cmd/ binary that nothing
// installed and nothing invoked.
//
// It is the case the tier rule deliberately exempts — a singlechecker main owns
// no verbs, so demanding a command tier of it would be a false positive — and
// that exemption is why the shape survived across 42 repositories. Publication
// is a different question from tiers, and this test is what keeps the two from
// being conflated again.
func TestAnalyzerRepoShapeIsReported(t *testing.T) {
	t.Parallel()

	diags := run(t, "testdata/analyzer")

	require.Len(t, diags, 1, "the tier rule stays silent; only the publication rule fires")
	assert.Equal(t, Rule, diags[0].Rule)
	assert.Equal(t, "testdata/analyzer/anonstruct.go", diags[0].Path)
	assert.Equal(t, goyze.SeverityError, diags[0].Severity)
	assert.Contains(t, diags[0].Message, "the root package",
		"the prose names what to move, not the checkout directory it happened to be found in")
	assert.Contains(t, diags[0].Message, "internal/",
		"the diagnostic must say where the code belongs, not merely that it is wrong")
}

// TestCorrectedShapeIsSilent is the same repository after the fix, and is what
// stops the rule from being satisfiable only by deleting cmd/.
func TestCorrectedShapeIsSilent(t *testing.T) {
	t.Parallel()

	assert.Empty(t, run(t, "testdata/corrected"))
}

// TestBareMarkerFileIsSilent pins the one exemption: a file holding nothing but
// a package clause. template.cli's project.go is the fleet's example, and
// flagging it would be objecting to the existence of a name rather than to any
// code reachable through it.
func TestBareMarkerFileIsSilent(t *testing.T) {
	t.Parallel()

	assert.Empty(t, run(t, "testdata/marker"),
		"a package clause with no declarations publishes nothing")
}

// TestNonMainPackageUnderCmdIsReported pins that cmd/ is not a hiding place.
// Nothing about the prefix makes a package private — cmd/app/helper is as
// importable as any other path — so the rule is "everything under internal/",
// not "everything outside cmd/".
func TestNonMainPackageUnderCmdIsReported(t *testing.T) {
	t.Parallel()

	diags := run(t, "testdata/cmdhelper")

	require.Len(t, diags, 1)
	assert.Equal(t, "testdata/cmdhelper/cmd/app/helper/helper.go", diags[0].Path)
	assert.Contains(t, diags[0].Message, "package cmd/app/helper",
		"named relative to the repository, so the diagnostic reads the same on every machine")
}

// TestLibraryIsSilent pins the other half of the conjunction. A repository that
// publishes packages and builds nothing is a library, which is the ordinary
// case; only building a program makes publication a contradiction.
func TestLibraryWithoutAProgramIsSilent(t *testing.T) {
	t.Parallel()

	assert.Empty(t, run(t, "testdata/library"),
		"publishing is only a finding for a repository that also builds a program")
}
