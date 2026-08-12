package clitiers

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

	assert.Equal(t, "clitiers", Runner{}.Name())
	assert.Equal(t, "stickler/clitiers", Rule)
}
