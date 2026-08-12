// Package clilayout is the whole-repo correspondence check for the opinionated
// three-tier CLI layout. It replaces the retired yze/layout analyzer, which
// could only see one package per pass and therefore checked correspondence
// solely at depth 1 —
// the hole that let a nested `tenant create` ship with no domain tier behind
// it. Correspondence between two package trees is a whole-module fact, which is
// why this lives in stickler rather than go/analysis.
//
// The rule, in both directions and at every depth:
//
//   - a command package (one declaring a Command()/<Verb>Command() entry
//     point) beneath internal/app/commands/<path> must have a domain verb
//     declaring the Config/Result/Run contract at internal/domain/<path> —
//     unless it is a mount-only parent, recognized by a self-declaring command
//     package beneath it;
//   - a domain verb beneath internal/domain/<path> must have a self-declaring
//     command package at internal/app/commands/<path>.
//
// A package that declares neither shape — a helper under a command
// (commands/greet/flags), a shared grouping under a domain (domain/config) —
// binds nothing in either direction; depth never makes a package a verb.
package clilayout

import (
	"context"
	"fmt"
	"strings"

	goyze "github.com/gomatic/go-yze"

	"github.com/gomatic/stickler/internal/suite"
)

// Name is the runner's registry name; Rule its rule id.
const (
	Name = "clilayout"
	Rule = "stickler/" + Name
)

// tierMarker is one of the layout's marker path segments.
type tierMarker string

// The tier markers, segment-bounded.
const (
	commandsMarker tierMarker = "internal/app/commands/"
	domainMarker   tierMarker = "internal/domain/"
)

// sourcePath is the path of one Go source file under the walked root.
type sourcePath string

// tierDir is one tier's directory for a pair, spelled for a diagnostic.
type tierDir string

// messageFormat is one diagnostic message template.
type messageFormat string

// The two diagnostic messages, one per direction.
const (
	messageNoDomain  messageFormat = "command package %s has no domain verb %s declaring the Config/Result/Run contract"
	messageNoCommand messageFormat = "domain verb %s has no command package %s declaring a Command entry point"
)

// Runner is the native whole-repo correspondence check, run alongside the
// external tool runners.
type Runner struct{}

// Name names the runner in the registry and in error reports.
func (Runner) Name() string { return Name }

// Run walks the module below root once and reports every unmatched verb.
func (Runner) Run(_ context.Context, root suite.Root) ([]goyze.Diagnostic, error) {
	tree, err := collectTiers(root.Dir())
	if err != nil {
		return nil, err
	}
	return tree.unmatched(), nil
}

// pairKey identifies one corresponding command/domain pair: the tree prefix
// holding the internal/ directory, and the verb path beneath the marker — kept
// separate so parallel internal trees stay distinct and either tier's
// directory can be spelled back for a diagnostic.
type pairKey struct {
	prefix string
	verb   string
}

// commandDir and domainDir spell the key's directory in each tier.
func (k pairKey) commandDir() tierDir { return tierDir(k.prefix + string(commandsMarker) + k.verb) }
func (k pairKey) domainDir() tierDir  { return tierDir(k.prefix + string(domainMarker) + k.verb) }

// tierDecl is what one package directory declares, and where — the anchor for
// any diagnostic about it.
type tierDecl struct {
	path sourcePath
	line int
}

// unmatched reports every verb missing its counterpart, in both directions. A
// command package with a self-declaring command beneath it is a mount-only
// parent and requires no domain verb of its own.
func (t tierTree) unmatched() []goyze.Diagnostic {
	return append(t.commandsWithoutDomains(), t.domainsWithoutCommands()...)
}

// commandsWithoutDomains reports every leaf command package with no domain
// verb behind it.
func (t tierTree) commandsWithoutDomains() []goyze.Diagnostic {
	var diags []goyze.Diagnostic
	for key, decl := range t.commands {
		if t.isMountParent(key) {
			continue
		}
		if _, ok := t.domains[key]; !ok {
			diags = append(diags, diagnose(decl, messageNoDomain, key.commandDir(), key.domainDir()))
		}
	}
	return diags
}

// domainsWithoutCommands reports every domain verb with no command package in
// front of it.
func (t tierTree) domainsWithoutCommands() []goyze.Diagnostic {
	var diags []goyze.Diagnostic
	for key, decl := range t.domains {
		if _, ok := t.commands[key]; !ok {
			diags = append(diags, diagnose(decl, messageNoCommand, key.domainDir(), key.commandDir()))
		}
	}
	return diags
}

// isMountParent reports whether another self-declaring command package lies
// beneath key — the shape of a parent that only mounts subcommands.
func (t tierTree) isMountParent(key pairKey) bool {
	for other := range t.commands {
		if other.prefix == key.prefix && strings.HasPrefix(other.verb, key.verb+"/") {
			return true
		}
	}
	return false
}

// diagnose builds the diagnostic for one unmatched verb, anchored at its
// declaring file.
func diagnose(decl tierDecl, message messageFormat, subject, counterpart tierDir) goyze.Diagnostic {
	return goyze.Diagnostic{
		Tool:     Name,
		Rule:     Rule,
		Path:     string(decl.path),
		Line:     decl.line,
		Col:      1,
		Severity: goyze.SeverityError,
		Message:  fmt.Sprintf(string(message), subject, counterpart),
	}
}
