// Package stickler is the gomatic lint runner: it executes a set of analyzer
// tools (the yze suite, golangci-lint, and others) to completion, normalizes
// their findings into one diagnostic schema, and reports pass/fail via the
// process exit code. Any finding or any tool error is a stickler failure.
package stickler

import (
	"cmp"
	"context"
	"slices"
	"sync"

	errs "github.com/gomatic/go-error"
	goyze "github.com/gomatic/go-yze"
)

// ErrRunner wraps a failure from one tool runner, tagged with the runner name.
const ErrRunner errs.Const = "lint runner failed"

// Root is the directory or package pattern a runner analyzes (e.g. "./..." or a
// path); it is the target every Runner operates over.
type Root string

// Runner executes one analyzer tool over a root directory and returns its
// findings as normalized diagnostics.
type Runner interface {
	Name() string
	Run(ctx context.Context, root Root) ([]goyze.Diagnostic, error)
}

// Result aggregates every runner's findings and failures from one stickler pass.
type Result struct {
	Diagnostics []goyze.Diagnostic
	Errors      []error
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

// Failed reports whether the pass should fail the build: any runner error, or any
// HARD (not soft-listed) diagnostic. Soft diagnostics are still reported by the
// formatter; they just do not gate.
func (r Result) Failed(soft Soft) bool {
	if len(r.Errors) > 0 {
		return true
	}
	return slices.ContainsFunc(r.Diagnostics, func(diag goyze.Diagnostic) bool {
		return !soft.covers(diag)
	})
}

// runnerResult is one runner's outcome, stored at its index so the merged output
// is ordered by runner regardless of completion order.
type runnerResult struct {
	err   error
	diags []goyze.Diagnostic
}

// Orchestrate runs every runner concurrently to completion over root, collecting
// all diagnostics and wrapping any runner error with its name. One runner's failure
// never prevents the others from running, and the merged result is deterministic:
// diagnostics are grouped by runner order then sorted within the pass.
func Orchestrate(ctx context.Context, root Root, runners []Runner) Result {
	results := make([]runnerResult, len(runners))
	var wg sync.WaitGroup
	for i, runner := range runners {
		wg.Go(func() {
			diags, err := runner.Run(ctx, root)
			if err != nil {
				err = ErrRunner.With(err, "runner", runner.Name())
			}
			results[i] = runnerResult{diags: diags, err: err}
		})
	}
	wg.Wait()
	return collect(results)
}

// collect folds the per-runner results into one Result in runner order, then sorts
// the diagnostics so the report does not depend on goroutine scheduling.
func collect(results []runnerResult) Result {
	result := Result{}
	for _, r := range results {
		result.Diagnostics = append(result.Diagnostics, r.diags...)
		if r.err != nil {
			result.Errors = append(result.Errors, r.err)
		}
	}
	slices.SortStableFunc(result.Diagnostics, compareDiagnostics)
	return result
}

// compareDiagnostics orders diagnostics by path, line, column, then rule, giving a
// stable, tool-independent report order.
func compareDiagnostics(a, b goyze.Diagnostic) int {
	return cmp.Or(
		cmp.Compare(a.Path, b.Path),
		cmp.Compare(a.Line, b.Line),
		cmp.Compare(a.Col, b.Col),
		cmp.Compare(a.Rule, b.Rule),
	)
}
