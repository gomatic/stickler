package suite_test

import (
	"context"
	"errors"
	"testing"

	errs "github.com/gomatic/go-error"
	goyze "github.com/gomatic/go-yze"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gomatic/stickler/internal/constants"
	"github.com/gomatic/stickler/internal/suite"
)

type fakeRunner struct {
	err   error
	ran   *bool
	name  string
	diags []goyze.Diagnostic
}

func (f fakeRunner) Name() string { return f.name }

func (f fakeRunner) Run(context.Context, suite.Root) ([]goyze.Diagnostic, error) {
	if f.ran != nil {
		*f.ran = true
	}
	return f.diags, f.err
}

func diag(rule string) goyze.Diagnostic {
	return goyze.Diagnostic{
		Tool:     "yze",
		Rule:     rule,
		Path:     "a.go",
		Line:     1,
		Severity: goyze.SeverityError,
		Message:  rule,
	}
}

func TestOrchestrateCollectsDiagnosticsFromEveryRunner(t *testing.T) {
	result := suite.Orchestrate(context.Background(), ".", []suite.Runner{
		fakeRunner{name: "yze", diags: []goyze.Diagnostic{diag("yze/gotostmt")}},
		fakeRunner{name: "golangci", diags: []goyze.Diagnostic{diag("staticcheck/SA1000")}},
	})

	assert.Len(t, result.Diagnostics, 2)
	assert.Empty(t, result.Errors)
	assert.True(t, result.Failed(suite.Policy{}))
}

func TestOrchestrateRunsAllToCompletionDespiteAnError(t *testing.T) {
	secondRan := false
	result := suite.Orchestrate(context.Background(), ".", []suite.Runner{
		fakeRunner{name: "broken", err: errs.Const("tool crashed")},
		fakeRunner{name: "yze", diags: []goyze.Diagnostic{diag("yze/errconst")}, ran: &secondRan},
	})

	assert.True(t, secondRan, "later runners must still run after an earlier error")
	require.Len(t, result.Errors, 1)
	assert.True(t, errors.Is(result.Errors[0], constants.ErrRunner))
	assert.Len(t, result.Diagnostics, 1)
	assert.True(t, result.Failed(suite.Policy{}))
}

func TestResultFailedIsFalseOnlyWhenCleanAndErrorFree(t *testing.T) {
	clean := suite.Orchestrate(context.Background(), ".", []suite.Runner{
		fakeRunner{name: "yze"},
	})

	assert.False(t, clean.Failed(suite.Policy{}))
	assert.Empty(t, clean.Diagnostics)
	assert.Empty(t, clean.Errors)
}

func TestResultSoftFailDoesNotGateButHardStillDoes(t *testing.T) {
	want := assert.New(t)
	result := suite.Result{Diagnostics: []goyze.Diagnostic{
		{Tool: "yze", Rule: "yze/ptrrecv"},
		{Tool: "golangci-lint", Rule: "errcheck"},
	}}
	// a baseline that permits the soft finding present, so this test isolates
	// softening from the ratchet (which has its own test below).
	within := suite.Baseline{"yze/ptrrecv": 1}

	// soft-failing the whole yze tool still leaves the golangci finding hard.
	want.True(
		result.Failed(suite.Policy{Soft: suite.Soft{"yze"}, Baseline: within}),
		"a hard golangci finding still gates",
	)

	// soft-failing every present tool makes the run pass (findings reported, not gating).
	want.False(result.Failed(suite.Policy{Soft: suite.Soft{"yze", "golangci-lint"}, Baseline: suite.Baseline{
		"yze/ptrrecv": 1, "errcheck": 1,
	}}))

	// per-analyzer soft: only the named rule is soft, the golangci rule still gates.
	want.True(
		result.Failed(suite.Policy{Soft: suite.Soft{"yze/ptrrecv"}, Baseline: within}),
		"the golangci rule is not softened",
	)
	want.False(
		suite.Result{
			Diagnostics: []goyze.Diagnostic{{Tool: "yze", Rule: "yze/ptrrecv"}},
		}.Failed(suite.Policy{Soft: suite.Soft{"yze/ptrrecv"}, Baseline: within}),
	)
}

func TestSoftFindingsGateOnceTheyGrowPastTheBaseline(t *testing.T) {
	want := assert.New(t)
	soft := suite.Soft{"yze/invariant"}
	two := suite.Result{Diagnostics: []goyze.Diagnostic{
		{Tool: "yze", Rule: "yze/invariant"},
		{Tool: "yze", Rule: "yze/invariant"},
	}}
	atOne := suite.Policy{Soft: soft, Baseline: suite.Baseline{"yze/invariant": 1}}
	atTwo := suite.Policy{Soft: soft, Baseline: suite.Baseline{"yze/invariant": 2}}

	// at or under the committed ceiling, softening holds.
	want.False(two.Failed(atTwo), "at the baseline is permitted")
	want.False(
		two.Failed(suite.Policy{Soft: soft, Baseline: suite.Baseline{"yze/invariant": 5}}),
		"under the baseline is permitted",
	)

	// past it, the run gates: a soft rule that nobody counts is invisible, not
	// non-blocking, so growth has to hurt.
	want.True(two.Failed(atOne), "growth past the baseline gates")

	// a rule absent from the baseline is ceilinged at zero, so a repository that
	// has never carried a finding cannot silently acquire one.
	want.True(two.Failed(suite.Policy{Soft: soft}), "an unrecorded rule is permitted nothing")

	over := two.OverBaseline(atOne)
	want.Equal([]suite.Overage{{Rule: "yze/invariant", Count: 2, Baseline: 1}}, over)
	want.Empty(two.OverBaseline(atTwo))

	// a HARD rule is not counted by the ratchet at all; it gates on its own.
	hard := suite.Result{Diagnostics: []goyze.Diagnostic{{Tool: "yze", Rule: "yze/gotostmt"}}}
	want.Empty(hard.OverBaseline(suite.Policy{Soft: soft}), "only soft findings are counted against a baseline")
}

func TestPermanentProbeNeverGates(t *testing.T) {
	want := assert.New(t)
	// The shape that turned builds red in the field: one finding from a rule the
	// global configuration declares a PERMANENT PROBE — soft everywhere, forever,
	// because its precision is bounded by judgment and a human adjudicates each
	// finding. A probe reports; it never gates.
	one := suite.Result{Diagnostics: []goyze.Diagnostic{{Tool: "yze", Rule: "yze/invariant"}}}
	policy := suite.Policy{Soft: suite.Soft{"yze/invariant"}, Probe: suite.Probe{"yze/invariant"}}

	want.False(one.Failed(policy), "a probe with no recorded baseline must not gate")
	want.Empty(one.OverBaseline(policy), "a probe finding is not an overage")

	// An explicitly recorded zero is not a ceiling for a probe either: there is no
	// number a probe's count is worked down to, so no number gates it.
	policy.Baseline = suite.Baseline{"yze/invariant": 0}
	want.False(one.Failed(policy), "an explicit zero baseline does not make a probe gate")

	three := suite.Result{Diagnostics: []goyze.Diagnostic{
		{Tool: "yze", Rule: "yze/invariant"},
		{Tool: "yze", Rule: "yze/invariant"},
		{Tool: "yze", Rule: "yze/invariant"},
	}}
	want.False(three.Failed(policy), "a probe count that grows past a recorded baseline still does not gate")

	// Declaring a probe is sufficient on its own; it does not have to be
	// soft-listed as well for the run to stay green.
	want.False(one.Failed(suite.Policy{Probe: suite.Probe{"yze/invariant"}}), "a probe alone does not gate")

	// Never gating is only half the contract — the findings must still be
	// visible, counted per rule, or a probe nobody counts manufactures the
	// appearance of coverage.
	want.Equal([]suite.ProbeCount{{Rule: "yze/invariant", Count: 3}}, three.ProbeCounts(policy))
	want.Empty(suite.Result{}.ProbeCounts(policy), "no findings, nothing to report")
}

// TestProbeCountsTalliesEveryProbeRuleInStableOrder names ProbeCounts' claim.
// A probe never gates, so its count is the only signal it produces, and a
// report whose order depends on map iteration is a report nobody can diff
// between two runs of the same tree.
func TestProbeCountsTalliesEveryProbeRuleInStableOrder(t *testing.T) {
	t.Parallel()

	result := suite.Result{Diagnostics: []goyze.Diagnostic{
		{Tool: "yze", Rule: "yze/invariant"},
		{Tool: "yze", Rule: "yze/filesize"},
		{Tool: "yze", Rule: "yze/invariant"},
		{Tool: "yze", Rule: "yze/gotostmt"},
	}}
	policy := suite.Policy{Probe: suite.Probe{"yze/invariant", "yze/filesize"}}

	assert.Equal(t, []suite.ProbeCount{
		{Rule: "yze/filesize", Count: 1},
		{Rule: "yze/invariant", Count: 2},
	}, result.ProbeCounts(policy), "sorted by rule, whatever order the findings arrived in")
}

func TestProbeUnGatesOnlyTheRuleItNames(t *testing.T) {
	want := assert.New(t)
	probe := suite.Policy{Probe: suite.Probe{"yze/invariant"}}

	// A hard rule standing beside a probe finding still fails the build.
	beside := suite.Result{Diagnostics: []goyze.Diagnostic{
		{Tool: "yze", Rule: "yze/invariant"},
		{Tool: "yze", Rule: "yze/ptrrecv"},
	}}
	want.True(beside.Failed(probe), "a hard rule beside a probe still gates")

	// A ROLLOUT rule — soft-listed, not probe-listed — keeps the zero default: it
	// gates by right and is only temporarily quiet, so an unrecorded baseline
	// still permits it nothing.
	one := suite.Result{Diagnostics: []goyze.Diagnostic{{Tool: "yze", Rule: "yze/errtest"}}}
	rollout := suite.Policy{Soft: suite.Soft{"yze/errtest"}}
	want.True(one.Failed(rollout), "a rollout rule with no recorded baseline is still permitted nothing")
	want.Equal(
		[]suite.Overage{{Rule: "yze/errtest", Count: 1, Baseline: 0}},
		one.OverBaseline(rollout),
		"and the growth is named",
	)

	// A probe entry naming a TOOL covers nothing: one line must not be able to
	// stop a whole suite from gating.
	want.True(one.Failed(suite.Policy{Probe: suite.Probe{"yze"}}), "a probe entry naming a tool covers no rule")
	want.Empty(one.ProbeCounts(suite.Policy{Probe: suite.Probe{"yze"}}), "and counts nothing")
}

func TestProbeDoesNotMaskRunnerErrors(t *testing.T) {
	// A runner ERROR (the tool could not run) gates whatever the policy says;
	// probing suppresses findings, never infrastructure failures.
	result := suite.Result{Errors: []error{errs.Const("yze crashed")}}
	assert.True(t, result.Failed(suite.Policy{Probe: suite.Probe{"yze/invariant"}}))
}

func TestResultSoftDoesNotMaskRunnerErrors(t *testing.T) {
	// a runner ERROR (the tool could not run) gates regardless of soft — soft only
	// suppresses findings, not infrastructure failures.
	result := suite.Result{Errors: []error{errs.Const("yze crashed")}}
	assert.True(t, result.Failed(suite.Policy{Soft: suite.Soft{"yze"}}))
}
