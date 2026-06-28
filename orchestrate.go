// Package stickler is the gomatic lint runner: it executes a set of analyzer
// tools (the yze suite, golangci-lint, and others) to completion, normalizes
// their findings into one diagnostic schema, and reports pass/fail via the
// process exit code. Any finding or any tool error is a stickler failure.
package stickler

import (
	"context"

	errs "github.com/gomatic/go-error"
	goyze "github.com/gomatic/go-yze"
)

// ErrRunner wraps a failure from one tool runner, tagged with the runner name.
const ErrRunner errs.Const = "lint runner failed"

// Runner executes one analyzer tool over a root directory and returns its
// findings as normalized diagnostics.
type Runner interface {
	Name() string
	Run(ctx context.Context, root string) ([]goyze.Diagnostic, error)
}

// Result aggregates every runner's findings and failures from one stickler pass.
type Result struct {
	Diagnostics []goyze.Diagnostic
	Errors      []error
}

// Failed reports whether the pass should fail the build: any diagnostic or any
// runner error.
func (r Result) Failed() bool {
	return len(r.Diagnostics) > 0 || len(r.Errors) > 0
}

// Orchestrate runs every runner to completion over root, collecting all
// diagnostics and wrapping any runner error with its name. One runner's failure
// never prevents the others from running.
func Orchestrate(ctx context.Context, root string, runners []Runner) Result {
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
