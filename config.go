package stickler

import (
	"path/filepath"
	"slices"

	errs "github.com/gomatic/go-error"
	"gopkg.in/yaml.v3"
)

// Configuration errors.
const (
	// ErrConfig reports a stickler config file that cannot be parsed.
	ErrConfig errs.Const = "cannot load stickler config"
	// ErrBadListSetting reports a list setting that is neither a sequence nor an
	// add/remove/replace mapping.
	ErrBadListSetting errs.Const = "list setting must be a sequence or an add/remove/replace mapping"
)

// StringList is a list-valued setting in one configuration layer. It merges onto
// the value accumulated from lower layers either by replacing it (when written as
// a YAML sequence) or by adding and removing entries (when written as a mapping
// with add/remove/replace keys). An absent setting leaves the lower value intact.
type StringList struct {
	replace []string
	add     []string
	remove  []string
}

// UnmarshalYAML accepts a sequence (replace) or an add/remove/replace mapping.
func (l *StringList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.SequenceNode:
		return node.Decode(&l.replace)
	case yaml.MappingNode:
		var directives struct {
			Add     []string `yaml:"add"`
			Remove  []string `yaml:"remove"`
			Replace []string `yaml:"replace"`
		}
		if err := node.Decode(&directives); err != nil {
			return err
		}
		l.add, l.remove, l.replace = directives.Add, directives.Remove, directives.Replace
		return nil
	default:
		return ErrBadListSetting
	}
}

// applyTo folds this layer's directives onto the accumulated base value.
func (l StringList) applyTo(base []string) []string {
	out := base
	if l.replace != nil {
		out = slices.Clone(l.replace)
	}
	out = append(out, l.add...)
	return slices.DeleteFunc(out, func(s string) bool { return slices.Contains(l.remove, s) })
}

// Config is one configuration layer (global or repo).
type Config struct {
	Analyzers map[string]map[string]StringList `yaml:"analyzers"`
	Format    string                           `yaml:"format"`
	Runners   StringList                       `yaml:"runners"`
}

// Resolved is the concrete configuration after all layers are folded.
type Resolved struct {
	Analyzers map[string]map[string][]string
	Format    string
	Runners   []string
}

// Resolve folds the layers in order (global first, repo last), applying each
// layer's add/remove/replace directives onto the accumulated result.
func Resolve(layers ...Config) Resolved {
	resolved := Resolved{Analyzers: map[string]map[string][]string{}}
	for _, layer := range layers {
		resolved.Runners = layer.Runners.applyTo(resolved.Runners)
		if layer.Format != "" {
			resolved.Format = layer.Format
		}
		mergeAnalyzers(resolved.Analyzers, layer.Analyzers)
	}
	return resolved
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

// LoadLayers reads and parses each existing config path into a layer. A path the
// reader cannot open is treated as an absent layer and skipped; a path that
// parses badly is an error.
func LoadLayers(read func(path string) ([]byte, error), paths ...string) ([]Config, error) {
	var layers []Config
	for _, path := range paths {
		data, err := read(path)
		if err != nil {
			continue
		}
		var layer Config
		if err := yaml.Unmarshal(data, &layer); err != nil {
			return nil, ErrConfig.With(err, "path", path)
		}
		layers = append(layers, layer)
	}
	return layers, nil
}

// ConfigPaths returns the ordered config layer paths: the global config, then the
// repository's .stickler.yaml.
func ConfigPaths(getenv func(string) string, home, repoRoot string) []string {
	return []string{globalConfigPath(getenv, home), filepath.Join(repoRoot, ".stickler.yaml")}
}

// globalConfigPath returns the XDG global config path.
func globalConfigPath(getenv func(string) string, home string) string {
	if xdg := getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "stickler", "config.yaml")
	}
	return filepath.Join(home, ".config", "stickler", "config.yaml")
}
