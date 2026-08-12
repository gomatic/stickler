// Package suite is stickler's core vocabulary: the target a check runs over,
// the check interface every runner satisfies, and the aggregated result with
// its soft-fail ratchet.
//
// It is a leaf. Nothing here knows how a check is implemented, configured, or
// rendered — which is what lets the runner, report, and check packages all
// depend on it without depending on each other.
package suite

import (
	"context"
	"slices"
	"strings"

	goyze "github.com/gomatic/go-yze"
)

// Root is the directory or package pattern a runner analyzes (e.g. "./..." or a
// path); it is the target every Runner operates over.
type Root string

// Dir is a directory on disk, resolved from a Root.
type Dir string

// Dir converts the root (a directory or a "./..." package pattern) into the
// directory a filesystem walk starts from.
func (r Root) Dir() Dir {
	dir := strings.TrimSuffix(string(r), "...")
	dir = strings.TrimSuffix(dir, "/")
	if dir == "" {
		return "."
	}
	return Dir(dir)
}

// Runner executes one analyzer tool over a root directory and returns its
// findings as normalized diagnostics.
type Runner interface {
	Name() string
	Run(ctx context.Context, root Root) ([]goyze.Diagnostic, error)
}

// Soft is the set of soft-fail identifiers: a diagnostic whose tool (e.g. "yze")
// or rule (e.g. "yze/ptrrecv") is listed is reported but does NOT fail the run.
// It is the rollout ratchet — a tool starts soft and is moved to hard, whole or
// analyzer by analyzer, as a repo cleans up.
type Soft []string

// covers reports whether a diagnostic is soft: its tool or its rule is listed.
func (s Soft) covers(diag goyze.Diagnostic) bool {
	return slices.Contains(s, diag.Tool) || slices.Contains(s, diag.Rule)
}

// Baseline is the committed per-rule ceiling on SOFT findings: rule id to the
// number of findings that rule is currently permitted. It is a RATCHET, not a
// budget — the count may only fall, and raising an entry is a reviewable diff
// with an author and a date.
//
// Without it, softening a rule makes its findings invisible rather than
// non-blocking, which is the quieter form of a permanently red build: a probe
// nobody counts manufactures the appearance of coverage. A rule absent from the
// baseline is ceilinged at zero, so a repository that has never carried a
// finding cannot silently acquire one.
type Baseline map[string]int

// Overage is one soft rule reporting more findings than its baseline permits.
type Overage struct {
	Rule     string `json:"rule"`
	Count    int    `json:"count"`
	Baseline int    `json:"baseline"`
}
