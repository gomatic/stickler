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

	goyze "github.com/gomatic/go-yze"

	"github.com/gomatic/stickler/internal/layout"
	"github.com/gomatic/stickler/internal/suite"
)

// Name is the runner's registry name; Rule its rule id.
const (
	Name = "clilayout"
	Rule = "stickler/" + Name
)

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
	tree, err := layout.Collect(root.Dir())
	if err != nil {
		return nil, err
	}
	return unmatched(tree), nil
}

// unmatched reports every verb missing its counterpart, in both directions. A
// command package with a self-declaring command beneath it is a mount-only
// parent and requires no domain verb of its own.
func unmatched(tree layout.Tree) []goyze.Diagnostic {
	return append(commandsWithoutDomains(tree), domainsWithoutCommands(tree)...)
}

// commandsWithoutDomains reports every leaf command package with no domain
// verb behind it.
func commandsWithoutDomains(tree layout.Tree) []goyze.Diagnostic {
	var diags []goyze.Diagnostic
	for key, decl := range tree.Commands() {
		if isMountParent(tree, key) {
			continue
		}
		if _, ok := tree.Domains()[key]; !ok {
			diags = append(diags, diagnose(decl, messageNoDomain, key.CommandDir(), key.DomainDir()))
		}
	}
	return diags
}

// domainsWithoutCommands reports every domain verb with no command package in
// front of it.
func domainsWithoutCommands(tree layout.Tree) []goyze.Diagnostic {
	var diags []goyze.Diagnostic
	for key, decl := range tree.Domains() {
		if _, ok := tree.Commands()[key]; !ok {
			diags = append(diags, diagnose(decl, messageNoCommand, key.DomainDir(), key.CommandDir()))
		}
	}
	return diags
}

// isMountParent reports whether another self-declaring command package lies
// beneath key — the shape of a parent that only mounts subcommands.
func isMountParent(tree layout.Tree, key layout.Key) bool {
	for other := range tree.Commands() {
		if key.Nests(other) {
			return true
		}
	}
	return false
}

// diagnose builds the diagnostic for one unmatched verb, anchored at its
// declaring file.
func diagnose(decl layout.Decl, message messageFormat, subject, counterpart layout.TierDir) goyze.Diagnostic {
	return goyze.Diagnostic{
		Tool:     Name,
		Rule:     Rule,
		Path:     string(decl.Path()),
		Line:     decl.Line(),
		Col:      1,
		Severity: goyze.SeverityError,
		Message:  fmt.Sprintf(string(message), subject, counterpart),
	}
}
