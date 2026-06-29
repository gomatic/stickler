package stickler

import (
	"path/filepath"

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

// HomeDir is the current user's home directory, the base for the default global
// config location.
type HomeDir string

// RepoRoot is the directory whose .stickler.yaml supplies the repository
// configuration layer.
type RepoRoot string

// ConfigPaths returns the ordered config layer paths: the global config, then the
// repository's .stickler.yaml.
func ConfigPaths(getenv func(string) string, home HomeDir, repoRoot RepoRoot) []string {
	return []string{globalConfigPath(getenv, home), filepath.Join(string(repoRoot), ".stickler.yaml")}
}

// globalConfigPath returns the XDG global config path. Per the XDG Base Directory
// specification a relative $XDG_CONFIG_HOME is invalid and must be ignored, so the
// default ~/.config location is used unless the value is an absolute path.
func globalConfigPath(getenv func(string) string, home HomeDir) string {
	if xdg := getenv("XDG_CONFIG_HOME"); filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "stickler", "config.yaml")
	}
	return filepath.Join(string(home), ".config", "stickler", "config.yaml")
}
