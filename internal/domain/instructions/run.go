package instructions

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"

	"github.com/gomatic/stickler/internal/checks"
	"github.com/gomatic/stickler/internal/config"
	"github.com/gomatic/stickler/internal/domain"
	"github.com/gomatic/stickler/internal/runner"
	"github.com/gomatic/stickler/internal/suite"
)

// The seams tests substitute, so the document can be assembled without a real
// home directory, real config files, or real subprocesses.
var (
	getenv      config.Getenv     = os.Getenv
	readFile    config.FileReader = os.ReadFile
	userHomeDir                   = os.UserHomeDir
	execCommand                   = runner.ExecCommand
)

// assemble builds the checks this repository's configuration selects. It is a
// seam so the document can be rendered from stand-in checks.
var assemble = assembleFromConfig

// assembleFromConfig resolves the configuration and builds what it selects.
func assembleFromConfig(repoRoot config.RepoRoot) ([]suite.Runner, error) {
	home, _ := userHomeDir()
	resolved, err := checks.Resolve(readFile, getenv, config.HomeDir(home), repoRoot)
	if err != nil {
		return nil, err
	}
	return checks.Build(execCommand, resolved, repoRoot)
}

// Result is the outcome of one instructions pass: what each check said about
// itself, and which checks could not say anything.
type Result struct {
	Sections []Section `json:"sections"`
	Silent   []string  `json:"silent,omitempty"`
}

// Section is one check's own statement of the rules it enforces.
type Section struct {
	Runner string `json:"runner"`
	Text   string `json:"text"`
}

// Run asks every configured check to explain itself and writes the assembled
// document to the Config's writer.
//
// A check that cannot explain itself is NAMED in the output rather than
// omitted. An agent reading this needs to know which rules it has NOT been
// told about; silently rendering a shorter document would present a partial
// standard as the whole one.
func Run(ctx context.Context, logger *slog.Logger, cfg Config, _ ...domain.Argument) (Result, error) {
	runners, err := assemble(rootOrDefault(cfg.Root))
	if err != nil {
		return Result{}, err
	}
	result := explain(ctx, runners)
	logger.Info("Collected instructions.", "checks", len(result.Sections), "silent", len(result.Silent))
	if err := render(writer(cfg.Out), result); err != nil {
		return Result{}, err
	}
	return result, nil
}

// rootOrDefault falls back to the working directory, whose .stickler.yaml is
// the one a developer standing in a repository means.
func rootOrDefault(root config.RepoRoot) config.RepoRoot {
	if root == "" {
		return "."
	}
	return root
}

// explain asks each runner in turn, collecting what it said and noting the
// ones that said nothing.
func explain(ctx context.Context, runners []suite.Runner) Result {
	result := Result{Sections: make([]Section, 0, len(runners))}
	for _, runner := range runners {
		text, err := ask(ctx, runner)
		if err != nil {
			result.Silent = append(result.Silent, runner.Name())
			continue
		}
		result.Sections = append(result.Sections, Section{Runner: runner.Name(), Text: string(text)})
	}
	sort.Slice(result.Sections, func(i, j int) bool { return result.Sections[i].Runner < result.Sections[j].Runner })
	sort.Strings(result.Silent)
	return result
}

// ask returns what one runner has to say, or a matchable refusal when the
// runner carries no way to say it.
func ask(ctx context.Context, runner suite.Runner) (suite.Instructions, error) {
	explainer, ok := runner.(suite.Explainer)
	if !ok {
		return "", errNotAnExplainer
	}
	return explainer.Explain(ctx)
}

// render writes the assembled document.
func render(out io.Writer, result Result) error {
	if _, err := io.WriteString(out, preamble); err != nil {
		return err
	}
	for _, section := range result.Sections {
		if _, err := fmt.Fprintf(out, "\n%s\n", section.Text); err != nil {
			return err
		}
	}
	return renderSilent(out, result.Silent)
}

// renderSilent names the checks that could not explain themselves, so a reader
// knows the document is not the whole standard.
func renderSilent(out io.Writer, silent []string) error {
	if len(silent) == 0 {
		return nil
	}
	_, err := fmt.Fprintf(out, "\n## Checks that did not explain themselves\n\n"+
		"These run and can fail your build, but state no rules here — consult the tool directly:\n\n%s\n",
		bullets(silent))
	return err
}

// bullets renders one markdown list item per name.
func bullets(names []string) string {
	out := ""
	for _, name := range names {
		out += "- `" + name + "`\n"
	}
	return out
}

// writer defaults an unset destination to standard output.
func writer(out io.Writer) io.Writer {
	if out == nil {
		return os.Stdout
	}
	return out
}
