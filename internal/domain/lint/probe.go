package lint

// Which findings gate, and which only report. A rule softened for a ROLLOUT
// gates by right and is quiet only until its committed count is exceeded; a
// rule declared a PROBE is judgment-bound and never gates at any count. The two
// are indistinguishable from a count alone — an absent baseline reads as zero —
// so the probe list is what tells them apart, and it is read here.

import (
	"log/slog"

	"github.com/gomatic/stickler/internal/config"
	"github.com/gomatic/stickler/internal/suite"
)

// policyOf reads the gating decision out of the resolved configuration.
func policyOf(resolved config.Resolved) suite.Policy {
	return suite.Policy{
		Soft:     suite.Soft(resolved.Soft),
		Probe:    suite.Probe(resolved.Probe),
		Baseline: suite.Baseline(resolved.SoftBaseline),
	}
}

// reportProbes names each probe rule and how many findings it reported. A probe
// gates nothing, so this line is the only thing that carries it to a reader —
// and an ungated rule nobody counts manufactures the appearance of coverage.
// There is no baseline in the message because a probe has no ceiling: the
// remedy is to adjudicate the finding, not to record a number.
func reportProbes(logger *slog.Logger, probes []suite.ProbeCount) {
	for _, probe := range probes {
		logger.Info("Probe findings reported; a probe never gates, so adjudicate each one.",
			"rule", probe.Rule, "findings", probe.Count)
	}
}
