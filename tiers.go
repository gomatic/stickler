package stickler

// Collecting the tier trees: one filesystem walk parses every non-test Go
// file beneath a tier marker and records which packages declare themselves —
// a command entry point on the app side, the Config/Result/Run contract on
// the domain side.

import (
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// tierTree is every self-declaring package of each tier, by pair key.
type tierTree struct {
	commands map[pairKey]tierDecl
	domains  map[pairKey]tierDecl
}

// collectTiers walks the module once, parsing every non-test Go file beneath a
// tier marker and recording the packages that declare themselves.
func collectTiers(dir rootPath) (tierTree, error) {
	tree := tierTree{commands: map[pairKey]tierDecl{}, domains: map[pairKey]tierDecl{}}
	err := filepath.WalkDir(string(dir), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return skipDir(sourcePath(path), dir, dirName(entry.Name()))
		}
		if isSourceFile(sourcePath(path)) {
			tree.record(sourcePath(filepath.ToSlash(path)))
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
func skipDir(path sourcePath, root rootPath, name dirName) error {
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
func isSourceFile(path sourcePath) bool {
	return strings.HasSuffix(string(path), ".go") && !strings.HasSuffix(string(path), "_test.go")
}

// record parses one source file and records its package's self-declaration, if
// the file lies beneath a tier marker and declares the tier's shape. A file's
// tier is decided by the FIRST marker on its path: a helper tree that happens
// to nest the other tier's marker beneath a command or domain package
// (commands/greet/internal/domain/help) belongs to the OUTER tier — where the
// self-declaration gate judges it — never to a phantom inner one whose
// counterpart path would be nonsense.
func (t tierTree) record(path sourcePath) {
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
func markerIndex(path sourcePath, marker tierMarker) int {
	return strings.Index(string(path), string(marker))
}

// markerKey extracts the pair key for a file beneath the given tier marker:
// the prefix before internal/ joined with the package path beneath the marker.
// A file directly in the marker directory (the shared domain vocabulary
// package) has no verb path and is not a pair.
func markerKey(path sourcePath, marker tierMarker) (pairKey, bool) {
	prefix, rest, found := strings.Cut(string(path), string(marker))
	if !found || (prefix != "" && !strings.HasSuffix(prefix, "/")) {
		return pairKey{}, false
	}
	verb := rest[:max(strings.LastIndex(rest, "/"), 0)]
	if verb == "" {
		return pairKey{}, false
	}
	return pairKey{prefix: prefix, verb: verb}, true
}

// declKind decides whether one parsed file declares its tier's shape, returning
// the declaration's line when it does.
type declKind func(file *ast.File, fset *token.FileSet) (int, bool)

// recordDecl parses path and records the package under key when the file
// declares the tier's shape. A file that cannot be read or does not parse
// declares nothing, and neither does a file whose build constraints exclude
// it from the default build — a //go:build ignore file is not part of the
// layout, so its declarations must not conjure a verb demanding a
// counterpart. The first declaring file anchors the package's diagnostics.
func recordDecl(tier map[pairKey]tierDecl, key pairKey, path sourcePath, declares declKind) {
	src, err := os.ReadFile(string(path))
	if err != nil || buildExcluded(src) {
		return
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, string(path), src, parser.SkipObjectResolution)
	if err != nil {
		return
	}
	line, ok := declares(file, fset)
	if !ok {
		return
	}
	if _, exists := tier[key]; !exists {
		tier[key] = tierDecl{path: path, line: line}
	}
}

// buildExcluded reports whether the file's build constraints exclude it from
// the default build. Only the header is scanned — the region before the first
// non-blank, non-line-comment line — matching where the go tool requires a
// constraint to appear, so the scan stays as cheap as the tier walk's own
// file reads.
func buildExcluded(src []byte) bool {
	satisfied := func(tag string) bool { return defaultTag(buildTag(tag)) }
	for _, line := range headerLines(src) {
		if expr, err := constraint.Parse(line); err == nil && !expr.Eval(satisfied) {
			return true
		}
	}
	return false
}

// headerLines returns the file's leading blank and line-comment lines — the
// only region where a build constraint may appear.
func headerLines(src []byte) []string {
	var lines []string
	for line := range strings.Lines(string(src)) {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "//") {
			return lines
		}
		lines = append(lines, trimmed)
	}
	return lines
}

// buildTag is one tag queried while evaluating a build constraint.
type buildTag string

// defaultTag reports whether one build tag is satisfied in the default build
// the layout describes: the host OS and architecture, the unix family, the gc
// toolchain with cgo available, and the go1.N language versions — mirroring
// go/build's defaults without loading a build.Context. An unknown custom tag
// is unsatisfied, exactly as it is for an untagged `go build`.
func defaultTag(tag buildTag) bool {
	return string(tag) == runtime.GOOS || string(tag) == runtime.GOARCH || tag == "unix" ||
		tag == "gc" || tag == "cgo" || strings.HasPrefix(string(tag), "go1.")
}

// declaresCommand reports a file declaring a command entry point: an exported
// top-level Command or <Verb>Command function, mirroring yze/cliapp's gate.
func declaresCommand(file *ast.File, fset *token.FileSet) (int, bool) {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv == nil && ast.IsExported(fn.Name.Name) && strings.HasSuffix(fn.Name.Name, "Command") {
			return fset.Position(fn.Pos()).Line, true
		}
	}
	return 0, false
}

// declaresContract reports a file declaring any element of the domain contract
// (Config, Result, Run), mirroring yze/clidomain's gate.
func declaresContract(file *ast.File, fset *token.FileSet) (int, bool) {
	for _, decl := range file.Decls {
		if line, ok := contractDecl(decl, fset); ok {
			return line, ok
		}
	}
	return 0, false
}

// contractDecl reports whether one declaration names an element of the domain
// contract.
func contractDecl(decl ast.Decl, fset *token.FileSet) (int, bool) {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Recv == nil && isContractName(declName(d.Name.Name)) {
			return fset.Position(d.Pos()).Line, true
		}
	case *ast.GenDecl:
		return contractSpec(d, fset)
	}
	return 0, false
}

// contractSpec reports whether a type declaration names an element of the
// domain contract.
func contractSpec(decl *ast.GenDecl, fset *token.FileSet) (int, bool) {
	for _, spec := range decl.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if ok && isContractName(declName(ts.Name.Name)) {
			return fset.Position(ts.Pos()).Line, true
		}
	}
	return 0, false
}

// declName is a declared top-level identifier.
type declName string

// isContractName reports whether name is one of the domain contract's
// declarations.
func isContractName(name declName) bool {
	return name == "Config" || name == "Result" || name == "Run"
}
