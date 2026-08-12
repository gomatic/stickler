// Package clitiers is the whole-repo check that a CLI has the three-tier
// layout AT ALL.
//
// It exists because every other layout rule is scoped INSIDE the trees it
// judges: yze/cliapp, yze/clidomain, and yze/cliflags each return early unless
// the package path is already beneath internal/app/commands/ or
// internal/domain/, and stickler/clilayout reports only CORRESPONDENCE between
// the two trees. So a repository with neither tree satisfies all of them
// vacuously — it has no command package to judge and nothing to correspond.
// That is the hole stickler itself sat in: a CLI whose entire implementation
// was one exported root package, passing a gate that could not see it.
//
// The subject is a main that CONSTRUCTS a urfave command — not merely one that
// imports urfave. That distinction is the whole precision of this check, and it
// was measured rather than assumed: a sweep of 242 modules showed that keying
// on the import alone reports 26 yupsh/yup-* repositories, each of which
// declares a single-verb spec and hands it to a framework driver, importing
// urfave only to spell its flag TYPES. Those have no command tree because they
// have no verbs, and the three tiers exist to split a tree. The same is true of
// the 36 yze-go-* analyzer repositories, whose mains hand one analyzer to
// golang.org/x/tools/go/analysis/singlechecker.
//
// A rule that fires on 62 repositories which are all correct is not a strict
// rule, it is a broken one — it teaches everybody to ignore the gate.
package clitiers

import (
	"context"
	"strings"

	goyze "github.com/gomatic/go-yze"

	"github.com/gomatic/stickler/internal/layout"
	"github.com/gomatic/stickler/internal/suite"
)

// Name is the runner's registry name; Rule its rule id.
const (
	Name = "clitiers"
	Rule = "stickler/" + Name
)

// message is the single diagnostic this check emits.
const message = "urfave/cli program %s has no command tier: a CLI's flags, orchestration, " +
	"and logic belong in internal/app/commands/<verb>, internal/domain/<verb>, and internal/<capability>"

// Runner is the native check that a urfave/cli program has a command tier.
type Runner struct{}

// Name names the runner in the registry and in error reports.
func (Runner) Name() string { return Name }

// Run reports every urfave/cli main package in the module that has no command
// tier behind it. The walk is the module's, not one package's: whether a
// command tier exists ANYWHERE is precisely what no single-package analyzer
// pass can answer.
func (Runner) Run(_ context.Context, root suite.Root) ([]goyze.Diagnostic, error) {
	tree, err := layout.Collect(root.Dir())
	if err != nil {
		return nil, err
	}
	if len(tree.Commands()) > 0 {
		return nil, nil
	}
	return diagnostics(tree.Programs()), nil
}

// diagnostics reports one finding per urfave/cli program found.
func diagnostics(programs []layout.Program) []goyze.Diagnostic {
	diags := make([]goyze.Diagnostic, 0, len(programs))
	for _, program := range programs {
		diags = append(diags, goyze.Diagnostic{
			Tool:     Name,
			Rule:     Rule,
			Path:     string(program.Path()),
			Line:     1,
			Col:      1,
			Severity: goyze.SeverityError,
			Message:  strings.Replace(message, "%s", program.Dir(), 1),
		})
	}
	return diags
}
