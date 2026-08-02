package stickler

import (
	"context"
	"go/ast"
	"go/token"
	"io/fs"
	"slices"
	"strings"
	"testing"

	goyze "github.com/gomatic/go-yze"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClilayoutConformantTreeIsSilent pins the whole exemption surface at
// once: a matched pair, a mount-only parent with a self-declaring child, a
// matched NESTED pair, a helper beneath a command, a helper beneath a domain
// verb, a grouping domain package, the shared vocabulary package, and a
// package outside both tiers — none binds a counterpart requirement.
func TestClilayoutConformantTreeIsSilent(t *testing.T) {
	t.Parallel()

	diags, err := clilayoutRunner{}.Run(context.Background(), "testdata/clilayout/conformant")

	require.NoError(t, err)
	assert.Empty(t, diags)
}

// TestClilayoutReportsTheKilroyShape pins the regression this check exists
// for: three verbs living in one command package while three domain verb
// packages sit beneath it. Each unmatched domain verb is named, and the flat
// command package — which has no self-declaring child and no domain verb of
// its own — is named too.
func TestClilayoutReportsTheKilroyShape(t *testing.T) {
	t.Parallel()

	diags, err := clilayoutRunner{}.Run(context.Background(), "testdata/clilayout/kilroy")

	require.NoError(t, err)
	messages := messagesOf(diags)
	assert.Len(t, messages, 4)
	for _, verb := range []string{"tenant/create", "tenant/list", "tenant/migrate"} {
		assert.True(t, slices.ContainsFunc(messages, func(m string) bool {
			return strings.Contains(m, "internal/domain/"+verb+" has no command package")
		}), "the unmatched domain verb %s must be named: %v", verb, messages)
	}
	assert.True(t, slices.ContainsFunc(messages, func(m string) bool {
		return strings.Contains(m, "internal/app/commands/tenant has no domain verb")
	}), "the flat command package must be named: %v", messages)
}

// TestClilayoutReportsTheBrokenShapes ports yze-go-layout's fixture claims: a
// command with no domain directory at all, one whose counterpart holds no Go
// source, one whose counterpart's only file does not parse, and a domain verb
// with no command package are each reported — including a verb that declares
// only Run, the contract's function element.
func TestClilayoutReportsTheBrokenShapes(t *testing.T) {
	t.Parallel()

	diags, err := clilayoutRunner{}.Run(context.Background(), "testdata/clilayout/broken")

	require.NoError(t, err)
	messages := messagesOf(diags)
	assert.Len(t, messages, 5)
	for _, want := range []string{
		"internal/app/commands/orphan has no domain verb",
		"internal/app/commands/stub has no domain verb",
		"internal/app/commands/hollow has no domain verb",
		"internal/domain/lonely has no command package",
		"internal/domain/runonly has no command package",
	} {
		assert.True(t, slices.ContainsFunc(messages, func(m string) bool { return strings.Contains(m, want) }),
			"missing %q in %v", want, messages)
	}
}

// TestClilayoutDiagnosticsAnchorAtTheDeclaringFile pins that a finding lands
// where the developer works: the file declaring the unmatched verb, with the
// stickler/clilayout rule id.
func TestClilayoutDiagnosticsAnchorAtTheDeclaringFile(t *testing.T) {
	t.Parallel()

	diags, err := clilayoutRunner{}.Run(context.Background(), "testdata/clilayout/broken")

	require.NoError(t, err)
	require.NotEmpty(t, diags)
	for _, diag := range diags {
		assert.Equal(t, clilayoutRule, diag.Rule)
		assert.Equal(t, clilayoutName, diag.Tool)
		assert.True(t, strings.HasSuffix(diag.Path, ".go"), "anchored at a source file: %s", diag.Path)
		assert.Positive(t, diag.Line)
	}
}

// TestClilayoutWalkErrorSurfaces pins that an unwalkable root is an error, not
// a silent pass — a check that cannot look must not report clean.
func TestClilayoutWalkErrorSurfaces(t *testing.T) {
	t.Parallel()

	_, err := clilayoutRunner{}.Run(context.Background(), "testdata/clilayout/does-not-exist")

	assert.Error(t, err)
}

// TestRootDirNormalizesPackagePatterns pins the root translation: package
// patterns walk their directory, and an empty root walks the working
// directory.
func TestRootDirNormalizesPackagePatterns(t *testing.T) {
	t.Parallel()
	want := assert.New(t)

	want.Equal(rootPath("."), rootDir("./..."))
	want.Equal(rootPath("."), rootDir(""))
	want.Equal(rootPath("."), rootDir("..."))
	want.Equal(rootPath("sub/dir"), rootDir("sub/dir/..."))
	want.Equal(rootPath("sub/dir"), rootDir("sub/dir"))
}

// TestMarkerKeySeparatesTreesAndSkipsTheMarkerRoot pins the pairing rules: the
// prefix survives so parallel internal trees never cross-match, a file directly
// in the marker directory is no pair, and a look-alike segment is not the
// marker.
func TestMarkerKeySeparatesTreesAndSkipsTheMarkerRoot(t *testing.T) {
	t.Parallel()
	want := assert.New(t)

	key, ok := markerKey("m/internal/app/commands/greet/command.go", commandsMarker)
	want.True(ok)
	want.Equal(pairKey{prefix: "m/", verb: "greet"}, key)
	want.Equal(tierDir("m/internal/app/commands/greet"), key.commandDir())
	want.Equal(tierDir("m/internal/domain/greet"), key.domainDir())

	key, ok = markerKey("internal/domain/tenant/create/create.go", domainMarker)
	want.True(ok)
	want.Equal(pairKey{prefix: "", verb: "tenant/create"}, key)

	_, ok = markerKey("m/internal/domain/domain.go", domainMarker)
	want.False(ok, "the marker directory itself pairs nothing")

	_, ok = markerKey("m/notinternal/domain/greet/greet.go", domainMarker)
	want.False(ok, "a look-alike segment is not the marker")
}

// TestRecordNeverCreatesAPhantomInnerTier names record's claim: a file whose
// path nests the other tier's marker beneath a command or domain package
// belongs to the OUTER tier, never to a phantom inner one whose counterpart
// path would be nonsense.
func TestRecordNeverCreatesAPhantomInnerTier(t *testing.T) {
	t.Parallel()

	tree := tierTree{commands: map[pairKey]tierDecl{}, domains: map[pairKey]tierDecl{}}
	tree.record("testdata/clilayout/conformant/internal/app/commands/greet/internal/domain/help/help.go")
	tree.record("testdata/clilayout/conformant/internal/domain/greet/internal/app/commands/aux/aux.go")

	assert.Empty(t, tree.commands, "neither file declares its OUTER tier's shape, so neither is recorded —"+
		" the nested aux Command belongs to the domain tier, where it is not a contract")
	assert.Empty(t, tree.domains, "the nested help Config belongs to the command tier, where it is not an entry point")
}

// TestSkipDirPrunesWhatTheGoToolIgnores names skipDir's claims: testdata,
// vendor, and hidden or underscore-prefixed directories are pruned so an
// analyzer repo's own fixtures are not judged as its layout — while the walk
// root itself is exempt, so walking "." must not skip everything.
func TestSkipDirPrunesWhatTheGoToolIgnores(t *testing.T) {
	t.Parallel()
	want := assert.New(t)

	want.NoError(skipDir(".", ".", "."), "the walk root is exempt, whatever its name")
	want.NoError(skipDir("m/internal", "m", "internal"), "an ordinary directory walks on")
	for _, name := range []dirName{"testdata", "vendor", ".git", "_tools"} {
		want.ErrorIs(skipDir(sourcePath("m/"+string(name)), "m", name), fs.SkipDir, "%s is pruned", name)
	}
}

// TestContractDeclIgnoresNonDeclarations pins the fallthrough for the one
// declaration kind that is neither a func nor a gen decl: the parser's BadDecl
// placeholder. Walked files that fail to parse record nothing, so this arm is
// reachable only by contract — and the contract is that it declares nothing
// rather than panicking.
func TestContractDeclIgnoresNonDeclarations(t *testing.T) {
	t.Parallel()

	line, ok := contractDecl(&ast.BadDecl{}, token.NewFileSet())

	assert.False(t, ok)
	assert.Zero(t, line)
}

// messagesOf projects diagnostics onto their messages.
func messagesOf(diags []goyze.Diagnostic) []string {
	messages := make([]string, 0, len(diags))
	for _, diag := range diags {
		messages = append(messages, diag.Message)
	}
	return messages
}
