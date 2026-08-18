// Package librarymarker is the native check that a repository's library.go
// marker declares the package it marks.
//
// library.go carries `//go:build library_marker`, a tag no build ever sets. The
// file therefore exists but is NEVER COMPILED, which is exactly what makes it a
// classification marker — and exactly why nothing in the Go toolchain can tell
// you it is wrong. It is excluded from the package's GoFiles, so `go build`,
// `go vet`, staticcheck and every go/analysis analyzer in the yze suite are
// structurally incapable of seeing it. A marker whose package clause names a
// package that does not exist compiles clean, tests clean, and gates green
// forever.
//
// That is not hypothetical. A sweep of 96 markers across the fleet found 31
// declaring `package <name>_test` — 22 in gloo-foo/cmd-* and 9 in the gomatic
// analyzer repositories — all produced by generators that read a package clause
// from the first .go file they happened to list and struck a _test.go file. No
// build failed, no gate objected, and the defect survived in every one of them
// until the files were read by hand.
//
// So the check is here rather than in yze: a whole-repo fact about a file the
// compiler refuses to look at is precisely the class of question a native check
// exists for. It is a GATE — comparing two package clauses has no judgment in
// it, and the fix is one word.
//
// What it does NOT do is invent a convention. A marker beside no root package
// has nothing to disagree with, and the fleet spells those three different ways
// (`library`, `git`, `hyt`); reporting them would be enforcing a rule nobody has
// made. Whether a repository should HAVE a marker at all is likewise not this
// check's business — that is classification, and it belongs to the tooling that
// owns the marker.
package librarymarker

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	goyze "github.com/gomatic/go-yze"

	"github.com/gomatic/stickler/internal/suite"
)

// Name is the runner's registry name; Rule its rule id.
const (
	Name = "librarymarker"
	Rule = "stickler/" + Name
)

// markerFile is the file that marks a repository a library.
const markerFile fileName = "library.go"

// Named types for what this check reads: the repository root it walks, one
// file's base name, one file's path, and a package clause.
type (
	repoDir     string // repoDir is the repository root being judged.
	fileName    string // fileName is one directory entry's base name.
	filePath    string // filePath is one file's path on disk.
	packageName string // packageName is a Go package clause's identifier.
)

// message is the diagnostic this check emits.
const message = "library.go declares `package %s`, but this repository's root package is `%s`; the marker carries " +
	"//go:build library_marker so it is never compiled — nothing but this check can see that it names a package " +
	"that does not exist"

// readDir is the seam a test replaces to fail the root listing.
//
// Go offers no way to make os.ReadDir fail at a direct call site, and a fixture
// does not reach the branch either: the listing only happens once library.go has
// already been parsed out of that same directory. The failure is still a real
// one — a permission change or an unmounted volume between the two calls — and
// TestReadDirFailureIsReportedRatherThanSilentlyClean holds the check to reporting it, since answering
// "clean" for a root it failed to list reads, at the exit code, exactly like a
// root it read and found correct.
var readDir = os.ReadDir

// Runner is the native library-marker check.
type Runner struct{}

// Name names the runner in the registry and in error reports.
func (Runner) Name() string { return Name }

// Run reports a marker whose package clause disagrees with the root package it
// marks.
func (Runner) Run(_ context.Context, root suite.Root) ([]goyze.Diagnostic, error) {
	dir := repoDir(root.Dir())
	marker := filePath(filepath.Join(string(dir), string(markerFile)))
	declared, line, ok := clauseOf(marker)
	if !ok {
		return nil, nil
	}
	actual, ok, err := rootPackage(dir)
	if err != nil {
		return nil, err
	}
	if !ok || declared == actual {
		return nil, nil
	}

	return []goyze.Diagnostic{{
		Tool:     Name,
		Rule:     Rule,
		Path:     filepath.ToSlash(string(marker)),
		Line:     line,
		Col:      1,
		Severity: goyze.SeverityError,
		Message:  strings.Replace(strings.Replace(message, "%s", string(declared), 1), "%s", string(actual), 1),
	}}, nil
}

// rootPackage is the package the repository's root directory declares, from its
// non-test Go files other than the marker itself.
func rootPackage(dir repoDir) (packageName, bool, error) {
	names, err := rootFiles(dir)
	if err != nil {
		return "", false, err
	}
	for _, name := range names {
		if declared, _, ok := clauseOf(filePath(filepath.Join(string(dir), string(name)))); ok {
			return declared, true, nil
		}
	}

	return "", false, nil
}

// rootFiles is every file in the root that could carry the repository's package
// clause, in sorted order.
//
// The order is fixed rather than the filesystem's. A root holding two different
// package clauses does not compile, and the compiler says so far better than
// this check could — but while somebody is fixing that, a check reading entries
// in directory order would report a different one on each machine. This one
// reports the same one every time. It is also the very mistake that produced the
// defect being checked for: the generators that wrote `package <name>_test` did
// it by taking whichever file their listing handed them first.
func rootFiles(dir repoDir) ([]fileName, error) {
	entries, err := readDir(string(dir))
	if err != nil {
		return nil, err
	}
	names := make([]fileName, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !candidate(fileName(entry.Name())) {
			continue
		}
		names = append(names, fileName(entry.Name()))
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })

	return names, nil
}

// candidate reports whether a root file could carry the repository's package
// clause: a Go file that is neither a test nor the marker being judged.
func candidate(name fileName) bool {
	return strings.HasSuffix(string(name), ".go") &&
		!strings.HasSuffix(string(name), "_test.go") && name != markerFile
}

// clauseOf is a file's package clause and the line it sits on, read without
// parsing the body.
//
// ONE PARSE, NOT TWO. The line is only ever wanted for a file whose package
// name has already been read, so reading it separately gave one file two reads
// in a single answer: two chances to disagree, and a failure branch on the
// second that nothing could reach, because the first had already returned early
// on the input that would have triggered it.
func clauseOf(path filePath) (packageName, int, bool) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, string(path), nil, parser.PackageClauseOnly)
	if err != nil {
		return "", 0, false
	}

	return packageName(file.Name.Name), fset.Position(file.Name.Pos()).Line, true
}

// instructions is this check's own statement of what it enforces, rendered by
// `stickler instructions`.
const instructions = `## ` + "`" + Rule + "`" + `

` + "`library.go`" + ` must declare the same package as the repository's root.

The marker carries ` + "`//go:build library_marker`" + `, a tag no build ever sets, so
the file is never compiled. That is what makes it a classification marker, and it
is also why no compiler, linter, or analyzer can tell you the package clause is
wrong: the file is excluded from the package's sources, so every one of them is
structurally blind to it.

A marker naming a package that does not exist builds clean and gates green
forever. Across the fleet, 31 of 96 markers declared ` + "`package <name>_test`" + `
— written by generators that read a package clause from whichever ` + "`.go`" + ` file
they listed first and struck a ` + "`_test.go`" + `.

Fix it by naming the package the rest of the root declares. If the root has no
package at all, this check says nothing — there is nothing for the marker to
disagree with.
`

// Explain states the rule this check enforces.
func (Runner) Explain(context.Context) (suite.Instructions, error) { return instructions, nil }
