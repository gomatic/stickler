package lint

import (
	"context"
	"log/slog"
	"os"

	goyze "github.com/gomatic/go-yze"

	"github.com/gomatic/stickler/internal/config"
	"github.com/gomatic/stickler/internal/constants"
	"github.com/gomatic/stickler/internal/domain"
	"github.com/gomatic/stickler/internal/report"
	"github.com/gomatic/stickler/internal/runner"
	"github.com/gomatic/stickler/internal/suite"
)

// Result is the outcome of one lint pass, as data. The rendered report has
// already gone to the Config's writer; this is the same pass in a shape a
// second caller can act on.
type Result struct {
	Diagnostics []goyze.Diagnostic `json:"diagnostics"`
	Errors      []string           `json:"errors,omitempty"`
	Overages    []suite.Overage    `json:"overages,omitempty"`
	// Probes counts the findings of each rule declared a permanent probe. They
	// gate nothing, so the count is the whole of their signal: a second caller
	// that only reads HasFailures would otherwise never learn they exist.
	Probes      []suite.ProbeCount `json:"probes,omitempty"`
	HasFailures bool               `json:"failed"`
}

// The seams tests substitute, so a pass can be exercised without a real home
// directory, real config files, or real subprocesses.
var (
	getenv       config.Getenv     = os.Getenv
	readFile     config.FileReader = os.ReadFile
	userHomeDir                    = os.UserHomeDir
	execCommand                    = runner.ExecCommand
	buildRunners                   = build
)

// Run executes the configured checks over the root named in args, renders the
// report to the Config's writer, and reports failure with ErrLintFailed.
func Run(ctx context.Context, logger *slog.Logger, cfg Config, args ...domain.Argument) (Result, error) {
	root := rootOf(args)
	repoRoot := configRoot(cfg.Root, root)
	resolved, err := configure(repoRoot)
	if err != nil {
		return Result{}, err
	}
	runners, err := buildRunners(resolved, repoRoot)
	if err != nil {
		return Result{}, err
	}
	pass := orchestrate(ctx, cfg.Timeout, root, runners)
	result := resultOf(pass, resolved)
	if err := report.Write(writer(cfg.Out), format(cfg.Format, report.OutputFormat(resolved.Format)), pass); err != nil {
		return Result{}, err
	}
	reportOverages(logger, result.Overages)
	reportProbes(logger, result.Probes)
	if result.HasFailures {
		return result, constants.ErrLintFailed
	}
	return result, nil
}

// orchestrate runs every check under one overall deadline.
func orchestrate(ctx context.Context, timeout Timeout, root suite.Root, runners []suite.Runner) suite.Result {
	ctx, cancel := context.WithTimeout(ctx, timeout.duration())
	defer cancel()
	return suite.Orchestrate(ctx, root, runners)
}

// resultOf renders one pass as the domain result, applying the soft-fail
// ratchet the resolved configuration declares.
func resultOf(pass suite.Result, resolved config.Resolved) Result {
	policy := policyOf(resolved)
	return Result{
		Diagnostics: pass.Diagnostics,
		Errors:      messages(pass.Errors),
		Overages:    pass.OverBaseline(policy),
		Probes:      pass.ProbeCounts(policy),
		HasFailures: pass.Failed(policy),
	}
}

// messages renders each runner error as its message, so the result carries no
// live error values across a process or protocol boundary.
func messages(errs []error) []string {
	out := make([]string, 0, len(errs))
	for _, err := range errs {
		out = append(out, err.Error())
	}
	return out
}

// reportOverages names each soft rule that has grown past its committed
// baseline. A bare "failed" would leave the reader to diff two counts by hand,
// and the whole point of the ratchet is that growth is visible.
func reportOverages(logger *slog.Logger, overages []suite.Overage) {
	for _, over := range overages {
		logger.Warn("Soft findings grew past the committed baseline; fix one or raise the baseline deliberately.",
			"rule", over.Rule, "findings", over.Count, "baseline", over.Baseline)
	}
}
