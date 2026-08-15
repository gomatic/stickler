package lint

// The probe half of the ratchet: a rule declared judgment-bound reports and
// never gates, while a rule softened only for a rollout keeps its ceiling. The
// two are one mechanism read two ways, so they are exercised side by side.

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	errs "github.com/gomatic/go-error"
	goyze "github.com/gomatic/go-yze"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gomatic/stickler/internal/config"
	"github.com/gomatic/stickler/internal/constants"
	"github.com/gomatic/stickler/internal/suite"
)

// swapLayeredReadFile substitutes the config reader with a different document
// per layer, keyed the way the layers themselves are: a repository's document is
// its .stickler.yaml, everything else is the global one. A setting only one
// layer may declare cannot be exercised through a reader that answers both with
// the same bytes.
func swapLayeredReadFile(t *testing.T, global, repo string) {
	t.Helper()
	original := readFile
	t.Cleanup(func() { readFile = original })
	readFile = func(path string) ([]byte, error) {
		if strings.HasSuffix(path, ".stickler.yaml") {
			return []byte(repo), nil
		}
		return []byte(global), nil
	}
}

// TestRunPermanentProbeReportsWithoutGating pins the whole point of a probe,
// end to end through the real config layers: the global configuration declares
// yze/invariant judgment-bound, the repository records no baseline for it, and
// the finding is REPORTED while the pass SUCCEEDS.
//
// Before this, an absent baseline read as a ceiling of zero, so a rule the
// global config declared "never to block a push" blocked every push in every
// repository — and the only remedy the tooling counted was raising a baseline,
// which is itself a disablement. That is what made rewording the prose the
// probe had read the cheapest way to a green build.
func TestRunPermanentProbeReportsWithoutGating(t *testing.T) {
	probeFinding := goyze.Diagnostic{Path: "a.go", Rule: "yze/invariant", Message: "claims a property"}
	swapRunners(t, fakeRunner{diags: []goyze.Diagnostic{probeFinding, probeFinding}})
	swapLayeredReadFile(t, "soft: [yze/invariant]\nprobe: [yze/invariant]\n", "")

	out, result, err := runPass(t, Config{})

	require.NoError(t, err, "a probe must not gate at any count, with no baseline recorded")
	assert.Contains(t, out, "claims a property", "and it is still reported")
	assert.False(t, result.HasFailures)
	assert.Empty(t, result.Overages, "a probe finding is not an overage")
	assert.Equal(t, []suite.ProbeCount{{Rule: "yze/invariant", Count: 2}}, result.Probes,
		"the count is what keeps an ungated rule visible")
}

// TestRunRollOutRuleStillGatesWithNoBaseline is the other half of the same
// contract: softening a rule that gates BY RIGHT still permits it nothing until
// a number is committed, so making probes stop gating cannot be used to make a
// rollout rule stop gating too.
func TestRunRollOutRuleStillGatesWithNoBaseline(t *testing.T) {
	swapRunners(t, fakeRunner{diags: []goyze.Diagnostic{
		{Path: "a.go", Rule: "yze/errtest", Message: "an error nobody asserts"},
	}})
	swapLayeredReadFile(t, "soft: [yze/errtest]\nprobe: [yze/invariant]\n", "")

	_, result, err := runPass(t, Config{})

	require.ErrorIs(t, err, constants.ErrLintFailed)
	assert.Equal(t, []suite.Overage{{Rule: "yze/errtest", Count: 1, Baseline: 0}}, result.Overages)
	assert.Empty(t, result.Probes)
}

// TestRunRefusesARepositoryDeclaredProbe pins the refusal end to end: a repo
// that declares its own probe does not lint at all, rather than quietly linting
// with a rule it has permanently disabled.
func TestRunRefusesARepositoryDeclaredProbe(t *testing.T) {
	swapRunners(t, fakeRunner{})
	swapLayeredReadFile(t, "probe: [yze/invariant]\n", "probe:\n  add: [yze/ptrrecv]\n")

	_, _, err := runPass(t, Config{})

	assert.ErrorIs(t, err, constants.ErrProbeNotGlobal)
}

// TestRunWithNoHomeWillNotReadAProbeOutOfTheTreeItIsLinting is the attack the
// scope rule exists to stop, driven end to end. A repository commits
// .config/stickler/config.yaml — the path an unresolvable home would join to —
// declaring a ROLLOUT rule a probe. If the pass reads it as the global layer,
// that rule stops gating for this repository, permanently and uncounted, from a
// file the repository itself ships. A container with no HOME needs no attacker
// to reach that state.
func TestRunWithNoHomeWillNotReadAProbeOutOfTheTreeItIsLinting(t *testing.T) {
	originalHome, originalEnv := userHomeDir, getenv
	t.Cleanup(func() { userHomeDir, getenv = originalHome, originalEnv })
	userHomeDir = func() (string, error) { return "", errs.Const("no home") }
	getenv = func(string) string { return "" }

	original := buildRunners
	t.Cleanup(func() { buildRunners = original })
	buildRunners = func(config.Resolved, config.RepoRoot) ([]suite.Runner, error) {
		return []suite.Runner{fakeRunner{diags: []goyze.Diagnostic{
			{Path: "a.go", Rule: "yze/errtest", Message: "an error nobody asserts"},
		}}}, nil
	}

	var asked []string
	originalRead := readFile
	t.Cleanup(func() { readFile = originalRead })
	readFile = func(path string) ([]byte, error) {
		asked = append(asked, path)
		if strings.HasSuffix(path, ".stickler.yaml") {
			return []byte("soft: [yze/errtest]\n"), nil
		}
		return []byte("probe: [yze/errtest]\n"), nil
	}

	_, result, err := runPass(t, Config{})

	require.ErrorIs(t, err, constants.ErrLintFailed, "the rollout rule must still gate")
	assert.Equal(t, []string{".stickler.yaml"}, asked, "no path inside the tree may be read as the global layer")
	assert.Empty(t, result.Probes)
	assert.Equal(t, []suite.Overage{{Rule: "yze/errtest", Count: 1, Baseline: 0}}, result.Overages)
}

// TestReportProbesNamesEachProbeAndItsCount pins that an ungated rule still
// reaches a reader: a probe nobody counts manufactures the appearance of
// coverage, which is the failure mode softening was invented to avoid.
func TestReportProbesNamesEachProbeAndItsCount(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	reportProbes(slog.New(slog.NewTextHandler(&out, nil)), []suite.ProbeCount{
		{Rule: "yze/invariant", Count: 5},
		{Rule: "yze/filesize", Count: 2},
	})

	got := out.String()
	assert.Contains(t, got, `rule=yze/invariant findings=5`)
	assert.Contains(t, got, `rule=yze/filesize findings=2`)
}

// TestReportProbesIsSilentWithNoProbeFindings pins that the notice only appears
// when there is something to adjudicate.
func TestReportProbesIsSilentWithNoProbeFindings(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	reportProbes(slog.New(slog.NewTextHandler(&out, nil)), nil)

	assert.Empty(t, out.String())
}
