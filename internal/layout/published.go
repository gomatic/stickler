package layout

// What a repository PUBLISHES, alongside what it builds.
//
// A command repository publishes nothing. Everything it implements lives under
// internal/, and the only package outside it is the cmd/<name> main. This file
// collects the two facts that decide whether that holds: which directories
// build a program, and which directories anyone outside the module could
// import.
//
// The reason to measure it is that the alternative is unfalsifiable. A
// repository shipping cmd/ AND an importable package is a CLI and a library at
// once, and the markers then describe neither — go.mod plus cmd/<binary>/
// classifies it a CLI, library.go at the root classifies it a library, and a
// repository holding both is whichever one a given tool happened to look for.
// Every layout rule keyed on that answer is then deciding against a coin flip.
//
// It was found the expensive way. Forty-two gomatic analyzer repositories sat in
// exactly that state: each was imported as a library by the yze suite, and each
// also built and published a singlechecker binary that no tool installed and
// nothing ever invoked. The binary existed because a layout rule had objected to
// the repository's shape, and adding cmd/ quieted it — which is the failure this
// rule is built to make impossible. Under it, adding cmd/ to silence a
// complaint forces the code under internal/, and the repository STOPS being
// importable; if that breaks a consumer, the repository was never a command and
// the question gets asked at the moment it is still cheap to answer.

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gomatic/stickler/internal/suite"
)

// cmdMarker is where a repository's programs live.
const cmdMarker tierMarker = "cmd/"

// internalMarker is where a command repository's implementation lives.
const internalMarker tierMarker = "internal/"

// mainPackage is the package clause that makes a directory a program rather
// than something importable.
const mainPackage = "main"

// Published is one importable package a repository exposes: a directory of
// non-test Go files whose package is not main, which lies outside internal/,
// and which actually declares something.
type Published struct {
	path SourcePath
	dir  string
}

// Path is the file that anchors a diagnostic about this package.
func (p Published) Path() SourcePath { return p.path }

// Describe names the package as a diagnostic's subject, RELATIVE to the
// repository.
//
// The absolute path is already carried by Path, where a tool needs it to open
// the file; repeating it in the prose names a location true only on the machine
// that ran the check.
//
// The package at the repository root has no directory to name — its relative
// path is "." — so naming it positionally says nothing about what to move.
// Naming it as what it is does.
func (p Published) Describe() string {
	if p.dir == rootDir {
		return rootPackage
	}

	return "package " + p.dir
}

// rootDir is the relative path of the repository root, and rootPackage is how a
// diagnostic names the package sitting in it.
const (
	rootDir     = "."
	rootPackage = "the root package"
)

// BuildsProgram reports whether the repository builds a program under cmd/.
//
// It is deliberately NOT limited to programs built from a particular framework.
// Whether a repository is a command is decided by its shipping one, whoever
// wrote the driver — and keying on the framework is precisely what let the
// analyzer repositories through, since their mains hand one analyzer to
// go/analysis's singlechecker and construct no command tree at all. That
// exemption is right for the tier rule, which asks whether a command tree is
// SPLIT correctly and has nothing to say about a program with no verbs. It is
// wrong here, because publishing a package is not a question about verbs.
func (t Tree) BuildsProgram() bool { return len(t.mains) > 0 }

// PublishedPackages is every importable package the repository exposes,
// ordered by directory so a report is deterministic.
func (t Tree) PublishedPackages() []Published {
	dirs := make([]string, 0, len(t.published))
	for dir := range t.published {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	out := make([]Published, 0, len(dirs))
	for _, dir := range dirs {
		out = append(out, t.published[dir])
	}

	return out
}

// recordVisibility records one file as either part of a program or part of an
// importable package.
func (t Tree) recordVisibility(path SourcePath, root suite.Dir) {
	relative := relativeTo(path, root)
	name, declares, ok := packageOf(path)
	if !ok {
		return
	}
	if name == mainPackage {
		if strings.HasPrefix(relative, string(cmdMarker)) {
			t.mains[filepath.Dir(string(path))] = struct{}{}
		}

		return
	}
	if !declares || strings.HasPrefix(relative, string(internalMarker)) {
		return
	}
	dir := filepath.ToSlash(filepath.Dir(relative))
	if _, seen := t.published[dir]; !seen {
		t.published[dir] = Published{path: path, dir: dir}
	}
}

// relativeTo is path expressed from the module root, slash-separated, so the
// markers can be matched as prefixes.
//
// A path that cannot be expressed relative to the root falls back to its own
// spelling, which classifies it as published. That branch is unreachable
// through [Collect] — every path it passes here came from a walk rooted at the
// same directory — but the fallback direction is still a choice, and it is the
// loud one on purpose: an unclassifiable path becomes a finding somebody reads
// rather than a silent pass on a file the rule could not place.
func relativeTo(path SourcePath, root suite.Dir) string {
	rel, err := filepath.Rel(string(root), string(path))
	if err != nil {
		return string(path)
	}

	return filepath.ToSlash(rel)
}

// packageOf is a file's package clause and whether the file declares anything
// at all.
//
// The second answer is what exempts a bare marker file. A file holding nothing
// but a package clause — template.cli's project.go is the fleet's example —
// publishes no code, and a rule that flagged it would be objecting to the
// existence of a name rather than to anything reachable through it. A file with
// even one declaration is different in kind: it is code somebody outside the
// module can call.
//
// An unparseable file is reported as neither. It is not this check's business
// to diagnose a syntax error the compiler will report far better, and guessing
// at the package of a file that does not parse would attribute a finding to a
// directory that may not hold one.
func packageOf(path SourcePath) (string, bool, bool) {
	file, err := parser.ParseFile(token.NewFileSet(), string(path), nil, parser.SkipObjectResolution)
	if err != nil {
		return "", false, false
	}

	return file.Name.Name, len(file.Decls) > 0, true
}
