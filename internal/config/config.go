// Package config is stickler's layered configuration: the documents that
// declare which checks run, how each tool is invoked, and which findings are
// permitted to be soft.
//
// It is a leaf. A tool is DATA here (see [RunnerSpec]) — the package knows how
// a tool's configuration is spelled and merged, never how one is executed.
package config

import "maps"

// Config is one configuration layer (global or repo). Config holds per-tool config
// overlays keyed by runner name (the `config:` block); each is deep-merged onto
// that tool's own base config file at run time. Soft lists runner names and/or rule
// identifiers whose findings are reported but do NOT fail the run (a soft-fail
// ratchet: a whole tool like `yze`, or a single analyzer like `yze/ptrrecv`).
type Config struct {
	Analyzers map[string]map[string]StringList `yaml:"analyzers"`
	Config    map[string]Overlay               `yaml:"config"`
	Define    map[string]RunnerSpec            `yaml:"define"`
	// SoftBaseline is the per-rule ceiling on soft findings. A later layer
	// overrides an earlier one entry by entry, so a repository records its own
	// numbers without restating the global set.
	SoftBaseline map[string]int `yaml:"soft-baseline"`
	Format       string         `yaml:"format"`
	Runners      StringList     `yaml:"runners"`
	Soft         StringList     `yaml:"soft"`
	// Probe names the rules that never gate: analyzers whose precision is bounded
	// by judgment, which report for a human to adjudicate. It is declared ONLY by
	// the global layer — see [LoadLayers] — because whether an analyzer is
	// judgment-bound is a property of the analyzer, not of a repository, and a
	// repo-declared probe would be an unbounded uncounted escape from a rule that
	// gates by right.
	Probe StringList `yaml:"probe"`
}

// Resolved is the concrete configuration after all layers are folded. Config maps
// each runner name to the ordered list of its per-layer overlays (global first,
// repo last); a config-file runner folds them onto its base config in the repo at
// run time, since that base lives in the repo, not in any stickler layer.
type Resolved struct {
	Analyzers    map[string]map[string][]string
	Config       map[string][]Overlay
	Define       map[string]RunnerSpec
	SoftBaseline map[string]int
	Format       string
	Runners      []string
	Soft         []string
	Probe        []string
}

// Resolve folds the layers in order (global first, repo last), applying each
// layer's add/remove/replace directives onto the accumulated result.
func Resolve(layers ...Config) Resolved {
	resolved := Resolved{
		Analyzers:    map[string]map[string][]string{},
		Config:       map[string][]Overlay{},
		Define:       map[string]RunnerSpec{},
		SoftBaseline: map[string]int{},
	}
	for _, layer := range layers {
		resolved.Runners = layer.Runners.applyTo(resolved.Runners)
		resolved.Soft = layer.Soft.applyTo(resolved.Soft)
		resolved.Probe = layer.Probe.applyTo(resolved.Probe)
		maps.Copy(resolved.SoftBaseline, layer.SoftBaseline)
		if layer.Format != "" {
			resolved.Format = layer.Format
		}
		mergeAnalyzers(resolved.Analyzers, layer.Analyzers)
		appendConfigOverlays(resolved.Config, layer.Config)
		maps.Copy(resolved.Define, layer.Define)
	}
	appendAnalyzerOverlay(resolved.Config, resolved.Analyzers)
	return resolved
}

// appendAnalyzerOverlay delivers the resolved `analyzers:` settings to yze as
// the last entry of its overlay list, so they reach the analyzer suite through
// the same generic config-merge path every other tool uses — and, being last,
// win over anything the `config: yze:` block set for the same key.
//
// Nothing is appended when no settings were configured: an empty overlay list
// is what tells the merger to pass no --config at all, leaving yze to discover
// its own .yze.yaml exactly as before.
func appendAnalyzerOverlay(config map[string][]Overlay, analyzers map[string]map[string][]string) {
	if len(analyzers) == 0 {
		return
	}
	config[ToolYze] = append(config[ToolYze], Overlay{analyzersKey: analyzerTree(analyzers)})
}

// analyzersKey is the top-level key yze reads its per-analyzer settings from.
const analyzersKey = "analyzers"

// analyzerTree renders the resolved settings as the generic YAML tree the
// merger and yze both expect: analyzer name -> setting name -> list.
func analyzerTree(analyzers map[string]map[string][]string) map[string]any {
	tree := make(map[string]any, len(analyzers))
	for analyzer, settings := range analyzers {
		values := make(map[string]any, len(settings))
		for setting, list := range settings {
			values[setting] = toAnyList(list)
		}
		tree[analyzer] = values
	}
	return tree
}

// toAnyList widens a resolved string list to the []any a decoded YAML
// sequence carries, so a later merge treats it like any other sequence.
func toAnyList(list []string) []any {
	out := make([]any, len(list))
	for i, item := range list {
		out[i] = item
	}
	return out
}

// MergeSpecs overlays config-defined runner specs onto the built-in defaults,
// returning a new map; a defined spec replaces the default of the same name. This
// is what lets a .stickler.yaml `define:` block add or override a tool without a
// recompile.
func MergeSpecs(defaults, defined map[string]RunnerSpec) map[string]RunnerSpec {
	merged := make(map[string]RunnerSpec, len(defaults)+len(defined))
	maps.Copy(merged, defaults)
	maps.Copy(merged, defined)
	return merged
}

// appendConfigOverlays appends each tool's overlay from one layer onto that tool's
// ordered overlay list, preserving layer order (global first, repo last).
func appendConfigOverlays(acc map[string][]Overlay, layer map[string]Overlay) {
	for tool, overlay := range layer {
		acc[tool] = append(acc[tool], overlay)
	}
}

// mergeAnalyzers folds a layer's per-analyzer settings onto the accumulator.
func mergeAnalyzers(acc map[string]map[string][]string, layer map[string]map[string]StringList) {
	for analyzer, settings := range layer {
		if acc[analyzer] == nil {
			acc[analyzer] = map[string][]string{}
		}
		for setting, list := range settings {
			acc[analyzer][setting] = list.applyTo(acc[analyzer][setting])
		}
	}
}
