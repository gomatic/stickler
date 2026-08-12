// Package config is stickler's layered configuration: the documents that
// declare which checks run, how each tool is invoked, and which findings are
// permitted to be soft.
//
// It is a leaf. A tool is DATA here (see [RunnerSpec]) — the package knows how
// a tool's configuration is spelled and merged, never how one is executed.
package config

import (
	"maps"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/gomatic/stickler/internal/constants"
)

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

// Path is the path of one stickler config layer (global, then repo).
type Path string

// LoadLayers reads and parses each existing config path into a layer. A path the
// reader cannot open is treated as an absent layer and skipped; a path that
// parses badly is an error.
func LoadLayers(read FileReader, paths ...Path) ([]Config, error) {
	var layers []Config
	for _, path := range paths {
		data, err := read(string(path))
		if err != nil {
			continue
		}
		var layer Config
		if err := yaml.Unmarshal(data, &layer); err != nil {
			return nil, constants.ErrConfig.With(err, "path", path)
		}
		layers = append(layers, layer)
	}
	return layers, nil
}

// HomeDir is the current user's home directory, the base for the default global
// config location.
type HomeDir string

// RepoRoot is the directory whose .stickler.yaml supplies the repository
// configuration layer.
type RepoRoot string

// Getenv reads one environment variable; injected so the config-path resolution
// is testable without mutating the process environment.
type Getenv func(key string) string

// Paths returns the ordered config layer paths: the global config, then the
// repository's .stickler.yaml.
func Paths(getenv Getenv, home HomeDir, repoRoot RepoRoot) []Path {
	return []Path{globalConfigPath(getenv, home), Path(filepath.Join(string(repoRoot), ".stickler.yaml"))}
}

// globalConfigPath returns the XDG global config path. Per the XDG Base Directory
// specification a relative $XDG_CONFIG_HOME is invalid and must be ignored, so the
// default ~/.config location is used unless the value is an absolute path.
func globalConfigPath(getenv Getenv, home HomeDir) Path {
	if xdg := getenv("XDG_CONFIG_HOME"); filepath.IsAbs(xdg) {
		return Path(filepath.Join(xdg, "stickler", "config.yaml"))
	}
	return Path(filepath.Join(string(home), ".config", "stickler", "config.yaml"))
}
