// Where a configuration layer comes from, and what its scope permits. The
// layers are the only place a repository and the machine meet, so the rule that
// keeps them apart lives here rather than beside the settings they carry.

package config

import (
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/gomatic/stickler/internal/constants"
)

// Path is the path of one stickler config layer (global, then repo).
type Path string

// Scope is which configuration layer a path is: the GLOBAL layer every
// repository inherits, or one REPOSITORY's own .stickler.yaml.
type Scope string

// The configuration scopes.
const (
	ScopeGlobal Scope = "global"
	ScopeRepo   Scope = "repo"
)

// Layer is one configuration source: where it is read from and what scope it
// carries. The scope travels WITH the path rather than being inferred from
// position, because an absent global config is skipped — so "the first layer
// loaded" is not reliably the global one, and a setting only the global layer
// may declare must never be honoured by accident.
type Layer struct {
	Path  Path
	Scope Scope
}

// LoadLayers reads and parses each existing config path into a layer. A path the
// reader cannot open is treated as an absent layer and skipped; a path that
// parses badly is an error.
//
// A `probe:` declaration outside the global layer is refused rather than
// ignored: a probe never gates at any count, so a repository able to declare one
// could permanently and uncountably disable a rule that gates by right — which
// is strictly weaker than the baseline ratchet it would replace.
func LoadLayers(read FileReader, sources ...Layer) ([]Config, error) {
	var layers []Config
	for _, source := range sources {
		data, err := read(string(source.Path))
		if err != nil {
			continue
		}
		layer, err := parseLayer(data, source)
		if err != nil {
			return nil, err
		}
		layers = append(layers, layer)
	}
	return layers, nil
}

// probeKey is the one setting a repository layer may not write.
const probeKey = "probe"

// parseLayer decodes one layer's document and enforces what its scope permits.
//
// The bytes are read twice on purpose. The first pass reads only the TOP-LEVEL
// KEYS, because the question a scope rule asks is whether the layer WROTE the
// key — never whether what it wrote would change the folded value. `probe:`
// spelled as a null, an empty mapping, or an empty directive folds to nothing
// today; refusing only the spellings that bite would leave the rest silently
// ignored, which is how a refusal decays into a shrug. A null does not even
// reach [StringList.UnmarshalYAML], so nothing downstream can see it.
func parseLayer(data []byte, source Layer) (Config, error) {
	var written map[string]yaml.Node
	if err := yaml.Unmarshal(data, &written); err != nil {
		return Config{}, constants.ErrConfig.With(err, "path", source.Path)
	}
	if _, probed := written[probeKey]; probed && source.Scope != ScopeGlobal {
		return Config{}, constants.ErrProbeNotGlobal.With(nil, "path", source.Path)
	}
	var layer Config
	if err := yaml.Unmarshal(data, &layer); err != nil {
		return Config{}, constants.ErrConfig.With(err, "path", source.Path)
	}
	return layer, nil
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

// Layers returns the ordered config layers: the global config, then the
// repository's .stickler.yaml, each carrying its own scope. When no global
// config location can be resolved the repository is the only layer — an
// unresolvable global config is an ABSENT one, never a guessed one.
func Layers(getenv Getenv, home HomeDir, repoRoot RepoRoot) []Layer {
	repo := Layer{Path: Path(filepath.Join(string(repoRoot), ".stickler.yaml")), Scope: ScopeRepo}
	global := globalConfigPath(getenv, home)
	if global == "" {
		return []Layer{repo}
	}
	return []Layer{{Path: global, Scope: ScopeGlobal}, repo}
}

// globalConfigPath returns the XDG global config path, or "" when there is none
// to resolve.
//
// The path must be ABSOLUTE. Per the XDG Base Directory specification a relative
// $XDG_CONFIG_HOME is invalid and must be ignored, and a home directory that did
// not resolve is not a location either: joining an empty home would yield the
// RELATIVE `.config/stickler/config.yaml`, which resolves against the working
// directory — inside the tree being linted. A repository that committed that
// path would then be supplying its own global layer, and the settings only the
// global layer may declare would be the repository's to declare. A container
// with no HOME reaches that state on its own, with nobody attacking anything.
func globalConfigPath(getenv Getenv, home HomeDir) Path {
	if xdg := getenv("XDG_CONFIG_HOME"); filepath.IsAbs(xdg) {
		return Path(filepath.Join(xdg, "stickler", "config.yaml"))
	}
	if !filepath.IsAbs(string(home)) {
		return ""
	}
	return Path(filepath.Join(string(home), ".config", "stickler", "config.yaml"))
}
