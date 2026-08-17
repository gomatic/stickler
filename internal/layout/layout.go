package layout

// Package layout reads the opinionated three-tier CLI layout off disk: one
// filesystem walk parses every non-test Go file beneath a tier marker and
// records which packages declare themselves — a command entry point on the app
// side, the Config/Result/Run contract on the domain side.
//
// It JUDGES nothing. What the collected tree means is the business of the
// checks that read it: stickler/clilayout asks whether the two tiers
// correspond, stickler/clicommands asks whether a command tier exists at all.
// Both read one implementation of the walk, so they cannot come to disagree
// about what a command package is.

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gomatic/stickler/internal/suite"
)

// Tree is every self-declaring package of each tier, by pair key.
type Tree struct {
	commands  map[Key]Decl
	domains   map[Key]Decl
	programs  map[string]Program
	mains     map[string]struct{}
	published map[string]Published
}

// Program is one main package that BUILDS a urfave command — a command tree,
// as opposed to a main that hands a declaration to a framework driver.
type Program struct {
	path SourcePath
	dir  string
}

// Path is the file that anchors a diagnostic about this program.
func (p Program) Path() SourcePath { return p.path }

// Dir is the program's package directory.
func (p Program) Dir() string { return p.dir }

// Programs is every command-building main package found, anchored at its first
// such file and ordered by directory so a report is deterministic.
//
// One program, one entry: a tree split across several files of one main package
// is still one program, and counting files would make a four-file main look
// four times as significant as a one-file one.
func (t Tree) Programs() []Program {
	dirs := make([]string, 0, len(t.programs))
	for dir := range t.programs {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	out := make([]Program, 0, len(dirs))
	for _, dir := range dirs {
		out = append(out, t.programs[dir])
	}
	return out
}

// Commands is every self-declaring command package found, by pair key.
func (t Tree) Commands() map[Key]Decl { return t.commands }

// Domains is every self-declaring domain verb found, by pair key.
func (t Tree) Domains() map[Key]Decl { return t.domains }

// Collect walks the module once, parsing every non-test Go file beneath a
// tier marker and recording the packages that declare themselves.
func Collect(dir suite.Dir) (Tree, error) {
	tree := Tree{
		commands:  map[Key]Decl{},
		domains:   map[Key]Decl{},
		programs:  map[string]Program{},
		mains:     map[string]struct{}{},
		published: map[string]Published{},
	}
	err := filepath.WalkDir(string(dir), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return skipDir(SourcePath(path), dir, dirName(entry.Name()))
		}
		if isSourceFile(SourcePath(path)) {
			slashed := SourcePath(filepath.ToSlash(path))
			tree.record(slashed)
			tree.recordProgram(slashed)
			tree.recordVisibility(slashed, dir)
		}
		return nil
	})
	return tree, err
}

// dirName is one directory's base name during the walk.
type dirName string

// The directory names the go tool ignores wholesale.
const (
	dirTestdata dirName = "testdata"
	dirVendor   dirName = "vendor"
)

// skipDir prunes the directories the go tool itself ignores — testdata,
// vendor, and hidden or underscore-prefixed names — so an analyzer repo's own
// fixtures are not judged as its layout. The walk root is exempt: walking "."
// must not skip everything.
func skipDir(path SourcePath, root suite.Dir, name dirName) error {
	if string(path) == string(root) {
		return nil
	}
	if name == dirTestdata || name == dirVendor ||
		strings.HasPrefix(string(name), ".") || strings.HasPrefix(string(name), "_") {
		return fs.SkipDir
	}
	return nil
}

// isSourceFile reports whether path names a non-test Go source file.
func isSourceFile(path SourcePath) bool {
	return strings.HasSuffix(string(path), ".go") && !strings.HasSuffix(string(path), "_test.go")
}

// record parses one source file and records its package's self-declaration, if
// the file lies beneath a tier marker and declares the tier's shape. A file's
// tier is decided by the FIRST marker on its path: a helper tree that happens
// to nest the other tier's marker beneath a command or domain package
// (commands/greet/internal/domain/help) belongs to the OUTER tier — where the
// self-declaration gate judges it — never to a phantom inner one whose
// counterpart path would be nonsense.
func (t Tree) record(path SourcePath) {
	commandKey, isCommand := markerKey(path, commandsMarker)
	domainKey, isDomain := markerKey(path, domainMarker)
	if isCommand && isDomain {
		isDomain = markerIndex(path, domainMarker) < markerIndex(path, commandsMarker)
		isCommand = !isDomain
	}
	if isCommand {
		recordDecl(t.commands, commandKey, path, declaresCommand)
	}
	if isDomain {
		recordDecl(t.domains, domainKey, path, declaresContract)
	}
}

// markerIndex is the position of the marker's first occurrence in path.
func markerIndex(path SourcePath, marker tierMarker) int {
	return strings.Index(string(path), string(marker))
}

// markerKey extracts the pair key for a file beneath the given tier marker:
// the prefix before internal/ joined with the package path beneath the marker.
// A file directly in the marker directory (the shared domain vocabulary
// package) has no verb path and is not a pair.
func markerKey(path SourcePath, marker tierMarker) (Key, bool) {
	prefix, rest, found := strings.Cut(string(path), string(marker))
	if !found || (prefix != "" && !strings.HasSuffix(prefix, "/")) {
		return Key{}, false
	}
	verb := rest[:max(strings.LastIndex(rest, "/"), 0)]
	if verb == "" {
		return Key{}, false
	}
	return Key{prefix: prefix, verb: verb}, true
}

// tierMarker is one of the layout's marker path segments.
type tierMarker string

// The tier markers, segment-bounded.
const (
	commandsMarker tierMarker = "internal/app/commands/"
	domainMarker   tierMarker = "internal/domain/"
)

// SourcePath is the path of one Go source file under the walked root.
type SourcePath string

// TierDir is one tier's directory for a pair, spelled for a diagnostic.
type TierDir string

// Key identifies one corresponding command/domain pair: the tree prefix
// holding the internal/ directory, and the verb path beneath the marker — kept
// separate so parallel internal trees stay distinct and either tier's
// directory can be spelled back for a diagnostic.
type Key struct {
	prefix string
	verb   string
}

// Nests reports whether other lies beneath k in the SAME internal tree — the
// shape of a parent package that only mounts subcommands.
func (k Key) Nests(other Key) bool {
	return other.prefix == k.prefix && strings.HasPrefix(other.verb, k.verb+"/")
}

// CommandDir and DomainDir spell the key's directory in each tier.
func (k Key) CommandDir() TierDir { return TierDir(k.prefix + string(commandsMarker) + k.verb) }
func (k Key) DomainDir() TierDir  { return TierDir(k.prefix + string(domainMarker) + k.verb) }

// Decl is what one package directory declares, and where — the anchor for
// any diagnostic about it.
type Decl struct {
	path SourcePath
	line int
}

// Path is the file that anchors a diagnostic about this package.
func (d Decl) Path() SourcePath { return d.path }

// Line is the line that declaration sits on.
func (d Decl) Line() int { return d.line }
