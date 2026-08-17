package layout

import (
	"go/ast"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMarkerKeySeparatesTreesAndSkipsTheMarkerRoot pins the pairing rules: the
// prefix survives so parallel internal trees never cross-match, a file directly
// in the marker directory is no pair, and a look-alike segment is not the
// marker.
func TestMarkerKeySeparatesTreesAndSkipsTheMarkerRoot(t *testing.T) {
	t.Parallel()
	want := assert.New(t)

	key, ok := markerKey("m/internal/app/commands/greet/command.go", commandsMarker)
	want.True(ok)
	want.Equal(Key{prefix: "m/", verb: "greet"}, key)
	want.Equal(TierDir("m/internal/app/commands/greet"), key.CommandDir())
	want.Equal(TierDir("m/internal/domain/greet"), key.DomainDir())

	key, ok = markerKey("internal/domain/tenant/create/create.go", domainMarker)
	want.True(ok)
	want.Equal(Key{prefix: "", verb: "tenant/create"}, key)

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

	tree := Tree{commands: map[Key]Decl{}, domains: map[Key]Decl{}}
	tree.record("testdata/conformant/internal/app/commands/greet/internal/domain/help/help.go")
	tree.record("testdata/conformant/internal/domain/greet/internal/app/commands/helper/helper.go")

	assert.Empty(t, tree.commands, "neither file declares its OUTER tier's shape, so neither is recorded —"+
		" the nested helper Command belongs to the domain tier, where it is not a contract")
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
		want.ErrorIs(skipDir(SourcePath("m/"+string(name)), "m", name), fs.SkipDir, "%s is pruned", name)
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

// TestRecordDeclMustNotConjureAVerbFromABuildExcludedFile names recordDecl's
// invariant directly: a build-excluded file records nothing under its key,
// while its untagged sibling shape records normally — the seam through which
// a //go:build ignore file would otherwise become a phantom domain verb.
//
// The two files are written here rather than kept as a fixture: the invariant
// is about ONE file's constraint header, so the test that names it should be
// readable without opening a second directory.
func TestRecordDeclMustNotConjureAVerbFromABuildExcludedFile(t *testing.T) {
	t.Parallel()
	const contract = "package greet\n\ntype Config struct{ Name string }\n\n" +
		"type Result struct{ Out string }\n\nfunc Run(cfg Config) (Result, error) { return Result{}, nil }\n"

	dir := t.TempDir()
	excluded := filepath.Join(dir, "excluded.go")
	plain := filepath.Join(dir, "plain.go")
	require.NoError(t, os.WriteFile(excluded, []byte("//go:build ignore\n\n"+contract), 0o600))
	require.NoError(t, os.WriteFile(plain, []byte(contract), 0o600))

	tier := map[Key]Decl{}
	key := Key{prefix: "m/", verb: "greet"}

	recordDecl(tier, key, SourcePath(excluded), declaresContract)
	assert.Empty(t, tier, "the excluded file's contract declarations record nothing")

	recordDecl(tier, key, SourcePath(plain), declaresContract)
	require.Len(t, tier, 1, "an untagged declaring file still records")

	recordDecl(tier, key, SourcePath(filepath.Join(dir, "absent.go")), declaresContract)
	assert.Len(t, tier, 1, "an unreadable file declares nothing")
	assert.Equal(t, SourcePath(plain), tier[key].Path(), "the first declaring file anchors the package")
}

// TestBuildExcludedReadsOnlyTheConstraintHeader names buildExcluded's claims:
// an unsatisfiable constraint (//go:build ignore, its legacy // +build form,
// or an unknown custom tag) excludes the file; a satisfied constraint and an
// unconstrained file do not; and only the header is read — a "constraint"
// after the package clause is ordinary text the go tool would ignore too.
func TestBuildExcludedReadsOnlyTheConstraintHeader(t *testing.T) {
	t.Parallel()
	want := assert.New(t)

	want.True(buildExcluded([]byte("//go:build ignore\n\npackage p\n")))
	want.True(buildExcluded([]byte("// +build ignore\n\npackage p\n")))
	want.True(buildExcluded([]byte("//go:build integration\n\npackage p\n")), "an unknown custom tag is unsatisfied")
	want.True(buildExcluded([]byte("// Copyright.\n\n//go:build ignore\n\npackage p\n")),
		"ordinary comments before the constraint do not end the header")

	want.False(buildExcluded([]byte("package p\n")), "an unconstrained file is part of the default build")
	want.False(buildExcluded([]byte("//go:build go1.1\n\npackage p\n")), "a satisfied constraint keeps the file")
	want.False(buildExcluded([]byte("package p\n\n//go:build ignore\n")),
		"a constraint after the package clause is not a constraint")
	want.False(buildExcluded([]byte("//go:build !!!\npackage p\n")), "an unparsable constraint line is skipped")
}

// TestDefaultTagMirrorsTheDefaultBuild pins which tags the layout's notion of
// the default build satisfies: the host platform, the unix family, gc, cgo,
// and the go1.N versions — and nothing else, so ignore and custom tags stay
// unsatisfied.
func TestDefaultTagMirrorsTheDefaultBuild(t *testing.T) {
	t.Parallel()
	want := assert.New(t)

	for _, tag := range []buildTag{buildTag(runtime.GOOS), buildTag(runtime.GOARCH), "unix", "gc", "cgo", "go1.22"} {
		want.True(defaultTag(tag), "%s is satisfied in the default build", tag)
	}
	for _, tag := range []buildTag{"ignore", "integration", "windows_probe", "gccgo"} {
		want.False(defaultTag(tag), "%s is not satisfied in the default build", tag)
	}
}

// TestHeaderLinesStopsAtTheFirstNonCommentLine pins the header boundary the
// constraint scan relies on: blanks and line comments are collected, and the
// first anything-else — package clause, block comment, code — ends the header.
func TestHeaderLinesStopsAtTheFirstNonCommentLine(t *testing.T) {
	t.Parallel()
	want := assert.New(t)

	header := headerLines([]byte("// a\n\n//go:build ignore\npackage p\n//tail\n"))
	want.Equal([]string{"// a", "", "//go:build ignore"}, header)
	want.Empty(headerLines([]byte("package p\n")))
	want.Equal([]string{"// only comments"}, headerLines([]byte("// only comments\n")), "EOF ends the header too")
	want.Empty(headerLines([]byte("/* block */\n//go:build ignore\npackage p\n")),
		"a block comment ends the header scan — go:build must be a line comment before it")
}

// TestRelativeToFallsBackLoudlyForAPathOutsideTheRoot pins the direction of the
// fallback rather than merely reaching it.
//
// The branch cannot be reached through Collect, whose paths all come from a walk
// rooted at the same directory. It still has a direction, and the direction is
// what matters: a path that cannot be placed relative to the root keeps its own
// spelling, so it fails the internal/ prefix test and is classified as
// published. That makes an unplaceable file a finding somebody reads, instead of
// a quiet pass on the one file the rule could not judge.
func TestRelativeToFallsBackLoudlyForAPathOutsideTheRoot(t *testing.T) {
	t.Parallel()

	const outside SourcePath = "/absolute/elsewhere/pkg/thing.go"

	got := relativeTo(outside, "relative/root")

	assert.Equal(t, string(outside), got, "an unplaceable path keeps its own spelling")
	assert.False(t, strings.HasPrefix(got, string(internalMarker)),
		"so it is not mistaken for internal code and silently passed")
}

// TestRelativeToStripsTheRoot is the ordinary case the fallback exists beside.
func TestRelativeToStripsTheRoot(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "internal/suite/x.go", relativeTo("root/internal/suite/x.go", "root"))
}
