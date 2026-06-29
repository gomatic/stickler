// Package stickler is the gomatic lint runner: it executes a set of analyzer
// tools (the yze suite, golangci-lint, and others) to completion, normalizes
// their findings into one diagnostic schema, and reports pass/fail via the
// process exit code. Any finding or any tool error is a stickler failure.
package stickler

import (
	"context"
	"slices"

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

// Orchestrate runs every runner to completion over root, collecting all
// diagnostics and wrapping any runner error with its name. One runner's failure
// never prevents the others from running.
func Orchestrate(ctx context.Context, root Root, runners []Runner) Result {
	result := Result{}
	for _, runner := range runners {
		diags, err := runner.Run(ctx, root)
		result.Diagnostics = append(result.Diagnostics, diags...)
		if err != nil {
			result.Errors = append(result.Errors, ErrRunner.With(err, "runner", runner.Name()))
		}
	}
	return result
}
