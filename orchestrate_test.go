package stickler_test

import (
	"context"
	"errors"
	"testing"

	errs "github.com/gomatic/go-error"
	goyze "github.com/gomatic/go-yze"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gomatic/stickler"
)

type fakeRunner struct {
	err   error
	ran   *bool
	name  string
	diags []goyze.Diagnostic
}

func (f fakeRunner) Name() string { return f.name }

func (f fakeRunner) Run(context.Context, stickler.Root) ([]goyze.Diagnostic, error) {
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
	result := stickler.Orchestrate(context.Background(), ".", []stickler.Runner{
		fakeRunner{name: "yze", diags: []goyze.Diagnostic{diag("yze/gotostmt")}},
		fakeRunner{name: "golangci", diags: []goyze.Diagnostic{diag("staticcheck/SA1000")}},
	})

	assert.Len(t, result.Diagnostics, 2)
	assert.Empty(t, result.Errors)
	assert.True(t, result.Failed(nil))
}

func TestOrchestrateRunsAllToCompletionDespiteAnError(t *testing.T) {
	secondRan := false
	result := stickler.Orchestrate(context.Background(), ".", []stickler.Runner{
		fakeRunner{name: "broken", err: errs.Const("tool crashed")},
		fakeRunner{name: "yze", diags: []goyze.Diagnostic{diag("yze/errconst")}, ran: &secondRan},
	})

	assert.True(t, secondRan, "later runners must still run after an earlier error")
	require.Len(t, result.Errors, 1)
	assert.True(t, errors.Is(result.Errors[0], stickler.ErrRunner))
	assert.Len(t, result.Diagnostics, 1)
	assert.True(t, result.Failed(nil))
}

func TestResultFailedIsFalseOnlyWhenCleanAndErrorFree(t *testing.T) {
	clean := stickler.Orchestrate(context.Background(), ".", []stickler.Runner{
		fakeRunner{name: "yze"},
	})

	assert.False(t, clean.Failed(nil))
	assert.Empty(t, clean.Diagnostics)
	assert.Empty(t, clean.Errors)
}

func TestResultSoftFailDoesNotGateButHardStillDoes(t *testing.T) {
	want := assert.New(t)
	result := stickler.Result{Diagnostics: []goyze.Diagnostic{
		{Tool: "yze", Rule: "yze/ptrrecv"},
		{Tool: "golangci-lint", Rule: "errcheck"},
	}}

	// soft-failing the whole yze tool still leaves the golangci finding hard.
	want.True(result.Failed(stickler.Soft{"yze"}), "a hard golangci finding still gates")

	// soft-failing every present tool makes the run pass (findings reported, not gating).
	want.False(result.Failed(stickler.Soft{"yze", "golangci-lint"}))

	// per-analyzer soft: only the named rule is soft, the golangci rule still gates.
	want.True(result.Failed(stickler.Soft{"yze/ptrrecv"}), "the golangci rule is not softened")
	want.False(
		stickler.Result{
			Diagnostics: []goyze.Diagnostic{{Tool: "yze", Rule: "yze/ptrrecv"}},
		}.Failed(
			stickler.Soft{"yze/ptrrecv"},
		),
	)
}

func TestResultSoftDoesNotMaskRunnerErrors(t *testing.T) {
	// a runner ERROR (the tool could not run) gates regardless of soft — soft only
	// suppresses findings, not infrastructure failures.
	result := stickler.Result{Errors: []error{errs.Const("yze crashed")}}
	assert.True(t, result.Failed(stickler.Soft{"yze"}))
}
