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

// Probe is the set of RULE identifiers that never gate a build.
//
// A probe is an analyzer whose precision is bounded by judgment rather than by
// syntax: it reports a finding a human or an agent adjudicates, and it is soft
// EVERYWHERE, permanently and by design. That is what separates it from the
// rollout softening the [Baseline] ratchets — a rollout rule gates by right and
// is only temporarily quiet, so its count is a debt that may only fall, while a
// probe's count is not a debt and no number "fixes" it.
//
// The two have to be DECLARED apart because they cannot be told apart from a
// count: an absent baseline reads as zero, so before this existed every probe
// gated on its first finding in every repository. The only remedy the tooling
// counted was raising a baseline, which is itself a disablement — so the
// cheapest way to a green build was to reword the prose the probe had read.
//
// A probe matches a diagnostic's RULE and never its TOOL. Matching by tool
// would let one configuration line stop an entire suite from gating, which is
// the silent disablement the ratchet exists to prevent.
type Probe []string

// covers reports whether a diagnostic comes from a probe rule.
func (p Probe) covers(diag goyze.Diagnostic) bool {
	return slices.Contains(p, diag.Rule)
}

// ProbeCount is one probe rule's finding count in a pass. A probe does not gate,
// so counting it is the only thing that keeps it visible: a probe nobody counts
// manufactures the appearance of coverage.
type ProbeCount struct {
	Rule  string `json:"rule"`
	Count int    `json:"count"`
}

// Policy is the whole gating decision for one pass: which findings are soft,
// which rules are permanent probes, and the committed ceiling on the rest. The
// three are one value because no one of them decides anything alone — whether a
// finding gates is a question about all three at once.
//
// Fields are ordered for struct-field alignment (the map first, then the
// slices); the decision they describe reads Soft, Probe, Baseline.
type Policy struct {
	Baseline Baseline
	Soft     Soft
	Probe    Probe
}

// gates reports whether one diagnostic fails the build on its own account.
func (p Policy) gates(diag goyze.Diagnostic) bool {
	return !p.Probe.covers(diag) && !p.Soft.covers(diag)
}

// ratchets reports whether a diagnostic is counted against a committed baseline.
// Soft findings are — that is the rollout ratchet. Probe findings are not: a
// probe has no debt to work down, so there is no ceiling to exceed, and a
// baseline recorded beside a probe rule is inert rather than a ceiling.
func (p Policy) ratchets(diag goyze.Diagnostic) bool {
	return !p.Probe.covers(diag) && p.Soft.covers(diag)
}
