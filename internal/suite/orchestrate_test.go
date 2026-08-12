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
	assert.True(t, result.Failed(nil, nil))
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
	assert.True(t, result.Failed(nil, nil))
}

func TestResultFailedIsFalseOnlyWhenCleanAndErrorFree(t *testing.T) {
	clean := suite.Orchestrate(context.Background(), ".", []suite.Runner{
		fakeRunner{name: "yze"},
	})

	assert.False(t, clean.Failed(nil, nil))
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
	want.True(result.Failed(suite.Soft{"yze"}, within), "a hard golangci finding still gates")

	// soft-failing every present tool makes the run pass (findings reported, not gating).
	want.False(result.Failed(suite.Soft{"yze", "golangci-lint"}, suite.Baseline{
		"yze/ptrrecv": 1, "errcheck": 1,
	}))

	// per-analyzer soft: only the named rule is soft, the golangci rule still gates.
	want.True(result.Failed(suite.Soft{"yze/ptrrecv"}, within), "the golangci rule is not softened")
	want.False(
		suite.Result{
			Diagnostics: []goyze.Diagnostic{{Tool: "yze", Rule: "yze/ptrrecv"}},
		}.Failed(suite.Soft{"yze/ptrrecv"}, within),
	)
}

func TestSoftFindingsGateOnceTheyGrowPastTheBaseline(t *testing.T) {
	want := assert.New(t)
	soft := suite.Soft{"yze/invariant"}
	two := suite.Result{Diagnostics: []goyze.Diagnostic{
		{Tool: "yze", Rule: "yze/invariant"},
		{Tool: "yze", Rule: "yze/invariant"},
	}}

	// at or under the committed ceiling, softening holds.
	want.False(two.Failed(soft, suite.Baseline{"yze/invariant": 2}), "at the baseline is permitted")
	want.False(two.Failed(soft, suite.Baseline{"yze/invariant": 5}), "under the baseline is permitted")

	// past it, the run gates: a soft rule that nobody counts is invisible, not
	// non-blocking, so growth has to hurt.
	want.True(two.Failed(soft, suite.Baseline{"yze/invariant": 1}), "growth past the baseline gates")

	// a rule absent from the baseline is ceilinged at zero, so a repository that
	// has never carried a finding cannot silently acquire one.
	want.True(two.Failed(soft, nil), "an unrecorded rule is permitted nothing")

	over := two.OverBaseline(soft, suite.Baseline{"yze/invariant": 1})
	want.Equal([]suite.Overage{{Rule: "yze/invariant", Count: 2, Baseline: 1}}, over)
	want.Empty(two.OverBaseline(soft, suite.Baseline{"yze/invariant": 2}))

	// a HARD rule is not counted by the ratchet at all; it gates on its own.
	hard := suite.Result{Diagnostics: []goyze.Diagnostic{{Tool: "yze", Rule: "yze/gotostmt"}}}
	want.Empty(hard.OverBaseline(soft, nil), "only soft findings are counted against a baseline")
}

func TestResultSoftDoesNotMaskRunnerErrors(t *testing.T) {
	// a runner ERROR (the tool could not run) gates regardless of soft — soft only
	// suppresses findings, not infrastructure failures.
	result := suite.Result{Errors: []error{errs.Const("yze crashed")}}
	assert.True(t, result.Failed(suite.Soft{"yze"}, nil))
}
