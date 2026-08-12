package layout

// What a file DECLARES, and whether it counts. Kept apart from the walk that
// finds the files, because these are questions about one file's syntax — does
// it declare a command entry point, does it declare the domain contract, and is
// it part of the default build at all — while the walk knows only paths.

import (
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// declKind decides whether one parsed file declares its tier's shape, returning
// the declaration's line when it does.
type declKind func(file *ast.File, fset *token.FileSet) (int, bool)

// recordDecl parses path and records the package under key when the file
// declares the tier's shape. A file that cannot be read or does not parse
// declares nothing, and neither does a file whose build constraints exclude
// it from the default build — a //go:build ignore file is not part of the
// layout, so its declarations must not conjure a verb demanding a
// counterpart. The first declaring file anchors the package's diagnostics.
func recordDecl(tier map[Key]Decl, key Key, path SourcePath, declares declKind) {
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
		tier[key] = Decl{path: path, line: line}
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

// buildsCLICommand reports whether the file at path declares `package main`
// AND constructs a urfave command. A file that cannot be parsed declares
// nothing: this fails OPEN, because reporting "you have no command tier" on the
// strength of a file that could not be read would be worse than silence.
func buildsCLICommand(path SourcePath) bool {
	file, err := parser.ParseFile(token.NewFileSet(), string(path), nil, parser.SkipObjectResolution)
	if err != nil || file.Name == nil || file.Name.Name != "main" {
		return false
	}
	alias, imported := cliAlias(file)
	return imported && buildsCommand(file, alias)
}

// packageAlias is the local name a file refers to an imported package by.
type packageAlias string

// cliAlias returns the local name this file calls urfave/cli by — the explicit
// alias when it has one, else the package name — and whether it imports it at
// all.
func cliAlias(file *ast.File) (packageAlias, bool) {
	for _, imported := range file.Imports {
		if imported.Path == nil || strings.Trim(imported.Path.Value, `"`) != cliModule {
			continue
		}
		if imported.Name != nil {
			return packageAlias(imported.Name.Name), true
		}
		return defaultCLIAlias, true
	}
	return "", false
}

// defaultCLIAlias is urfave/cli's package name, used when the import is not
// aliased.
const defaultCLIAlias packageAlias = "cli"

// buildsCommand reports whether the file constructs a urfave Command value.
// Constructing one is what makes a program a command TREE; a file that merely
// names the type — a spec handed to a framework driver, a helper taking a
// *cli.Command parameter — builds no tree and owns no verbs.
func buildsCommand(file *ast.File, alias packageAlias) bool {
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		lit, ok := node.(*ast.CompositeLit)
		if !ok || !isCommandType(lit.Type, alias) {
			return !found
		}
		found = true
		return false
	})
	return found
}

// isCommandType reports whether expr names urfave's Command type through
// alias, seeing through the pointer and slice spellings a command tree is
// actually written in: cli.Command, &cli.Command, and []*cli.Command are all
// the same construction. Missing the slice form would exempt any program whose
// tree is built as a literal list of subcommands.
func isCommandType(expr ast.Expr, alias packageAlias) bool {
	switch typed := expr.(type) {
	case *ast.ArrayType:
		return isCommandType(typed.Elt, alias)
	case *ast.StarExpr:
		return isCommandType(typed.X, alias)
	case *ast.SelectorExpr:
		return isCommandSelector(typed, alias)
	}
	return false
}

// isCommandSelector reports whether a qualified name is alias.Command.
func isCommandSelector(selector *ast.SelectorExpr, alias packageAlias) bool {
	if selector.Sel == nil || selector.Sel.Name != commandType {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && packageAlias(pkg.Name) == alias
}

// commandType is urfave/cli's command type name.
const commandType = "Command"

// cliModule is the import path a urfave/cli program is built from.
const cliModule = "github.com/urfave/cli/v3"

// recordProgram records one command-building main package, anchored at the
// first such file seen in the package directory.
func (t Tree) recordProgram(path SourcePath) {
	dir := filepath.Dir(string(path))
	if _, seen := t.programs[dir]; seen || !buildsCLICommand(path) {
		return
	}
	t.programs[dir] = Program{path: path, dir: dir}
}
