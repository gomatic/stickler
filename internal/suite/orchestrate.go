package suite

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"sync"

	goyze "github.com/gomatic/go-yze"

	"github.com/gomatic/stickler/internal/constants"
)

// Result aggregates every runner's findings and failures from one stickler pass.
type Result struct {
	Diagnostics []goyze.Diagnostic
	Errors      []error
}

// Failed reports whether the pass should fail the build: any runner error, any
// HARD (not soft-listed) diagnostic, or any soft rule that has grown past its
// committed baseline. Soft diagnostics within their baseline are still reported
// by the formatter; they just do not gate.
func (r Result) Failed(soft Soft, baseline Baseline) bool {
	if len(r.Errors) > 0 || len(r.OverBaseline(soft, baseline)) > 0 {
		return true
	}
	return slices.ContainsFunc(r.Diagnostics, func(diag goyze.Diagnostic) bool {
		return !soft.covers(diag)
	})
}

// OverBaseline returns every soft rule whose finding count exceeds its
// committed baseline, in stable rule order so output is deterministic.
func (r Result) OverBaseline(soft Soft, baseline Baseline) []Overage {
	counts := r.softCounts(soft)
	over := make([]Overage, 0, len(counts))
	for _, rule := range slices.Sorted(maps.Keys(counts)) {
		if counts[rule] > baseline[rule] {
			over = append(over, Overage{Rule: rule, Count: counts[rule], Baseline: baseline[rule]})
		}
	}
	return over
}

// softCounts tallies the soft-listed diagnostics by rule.
func (r Result) softCounts(soft Soft) map[string]int {
	counts := map[string]int{}
	for _, diag := range r.Diagnostics {
		if soft.covers(diag) {
			counts[diag.Rule]++
		}
	}
	return counts
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
				err = constants.ErrRunner.With(err, "runner", runner.Name())
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
