package stickler

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Overlay is one configuration layer's overlay for a single tool, taken from that
// tool's entry under the `config:` block of a .stickler.yaml. It is deep-merged
// onto the tool's own base config file at run time, so per-repo tool-config deltas
// live in .stickler.yaml instead of in a divergent, unmanaged base config. A
// mapping value deep-merges; a scalar or sequence replaces; a mapping written with
// only add/remove/replace keys mutates the base list (the StringList polymorphism).
type Overlay map[string]any

// MergeTree folds each overlay, in layer order, onto base and returns the effective
// configuration tree. base is never mutated.
func MergeTree(base map[string]any, overlays []Overlay) map[string]any {
	effective := cloneMap(base)
	for _, overlay := range overlays {
		effective = mergeMap(effective, map[string]any(overlay))
	}
	return effective
}

// mergeMap returns a new map with overlay folded onto base, recursing per key.
func mergeMap(base, overlay map[string]any) map[string]any {
	out := cloneMap(base)
	for key, value := range overlay {
		out[key] = mergeValue(out[key], value)
	}
	return out
}

// asMap coerces a decoded mapping to map[string]any, accepting both the plain type
// (as ParseTree yields) and the named Overlay type (as yaml.v3 yields when decoding
// the nested config: tree into map[string]Overlay) — a named map type does not
// satisfy a plain-type assertion, so both must be handled or deep merges silently
// degrade into wholesale replacements.
func asMap(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case Overlay:
		return typed, true
	default:
		return nil, false
	}
}

// mergeValue folds one overlay value onto its base value: list directives mutate
// the base list, a sub-map deep-merges, and anything else (scalar, sequence, or a
// map replacing a non-map) replaces.
func mergeValue(base, overlay any) any {
	if directives, ok := asDirectives(overlay); ok {
		return directives.applyTo(toStringList(base))
	}
	overlayMap, isMap := asMap(overlay)
	if !isMap {
		return overlay
	}
	baseMap, ok := asMap(base)
	if !ok {
		return cloneMap(overlayMap)
	}
	return mergeMap(baseMap, overlayMap)
}

// asDirectives recognizes an overlay value written as a non-empty mapping whose
// keys are all add/remove/replace, returning the list directives it encodes. Any
// other shape (including a map with a non-directive key) is not a directive set.
func asDirectives(overlay any) (StringList, bool) {
	overlayMap, ok := asMap(overlay)
	if !ok || len(overlayMap) == 0 {
		return StringList{}, false
	}
	for key := range overlayMap {
		if !listDirectiveKeys[key] {
			return StringList{}, false
		}
	}
	return StringList{
		add:     toStringList(overlayMap[directiveAdd]),
		remove:  toStringList(overlayMap[directiveRemove]),
		replace: replaceDirective(overlayMap),
	}, true
}

// replaceDirective returns the replace list as a sequence, or nil when the key is
// absent, so applyTo distinguishes "replace with empty" from "no replace".
func replaceDirective(overlayMap map[string]any) []string {
	if _, ok := overlayMap[directiveReplace]; !ok {
		return nil
	}
	replace := toStringList(overlayMap[directiveReplace])
	if replace == nil {
		return []string{}
	}
	return replace
}

// toStringList coerces a decoded YAML value to a string slice, dropping non-string
// and non-sequence values (a nil or scalar yields nil, i.e. an empty base list).
func toStringList(value any) []string {
	seq, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(seq))
	for _, item := range seq {
		if str, ok := item.(string); ok {
			out = append(out, str)
		}
	}
	return out
}

// cloneMap returns a shallow copy of m (empty for a nil map), so a merge never
// mutates a caller's map; nested maps are copied as merges recurse into them.
func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for key, value := range m {
		out[key] = value
	}
	return out
}

// ParseTree decodes a base config document into a generic tree, or an empty tree
// when data is empty or null. A malformed document is a configuration error.
func ParseTree(data []byte) (map[string]any, error) {
	var tree map[string]any
	if err := yaml.Unmarshal(data, &tree); err != nil {
		return nil, ErrConfig.With(err)
	}
	if tree == nil {
		return map[string]any{}, nil
	}
	return tree, nil
}

// MarshalTree renders an effective configuration tree as a YAML document.
func MarshalTree(tree map[string]any) ([]byte, error) {
	data, err := yaml.Marshal(tree)
	if err != nil {
		return nil, ErrConfig.With(err)
	}
	return data, nil
}

// FileReader reads a file's bytes; injected so config merging is testable without
// a real base config on disk.
type FileReader func(path string) ([]byte, error)

// TempWriter writes data to a fresh temporary file and returns its path plus a
// cleanup that removes it; injected so the effective-config write is testable.
type TempWriter func(data []byte) (path string, cleanup func(), err error)

// ConfigMerger is the generic, per-tool config-merge capability: given a tool's
// base config filenames, its config-flag prefix, the per-repo overlays, and the
// repo directory, it produces the tool's --config argument pointing at an effective
// config (base + overlays). It is reused by every config-file tool — golangci-lint
// is just one configured instance — so no tool is special-cased in the merge logic.
type ConfigMerger struct {
	Flag      string
	BaseDir   string
	Read      FileReader
	Temp      TempWriter
	BaseNames []string
	Overlays  []Overlay
}

// readBase returns the first existing base config under BaseDir, or empty bytes
// when none is present (the overlays then define the whole config).
func (m ConfigMerger) readBase() []byte {
	for _, name := range m.BaseNames {
		if data, err := m.Read(filepath.Join(m.BaseDir, name)); err == nil {
			return data
		}
	}
	return nil
}

// Args builds the tool's config argument: with overlays it merges them onto the
// base and writes the effective config to a temp file, returning the config flag
// and a cleanup; with no overlays it returns no extra args (and a no-op cleanup),
// leaving config discovery to the tool itself — the pre-merge behavior.
func (m ConfigMerger) Args() ([]Arg, func(), error) {
	if len(m.Overlays) == 0 {
		return nil, func() {}, nil
	}
	base, err := ParseTree(m.readBase())
	if err != nil {
		return nil, func() {}, err
	}
	data, err := MarshalTree(MergeTree(base, m.Overlays))
	if err != nil {
		return nil, func() {}, err
	}
	path, cleanup, err := m.Temp(data)
	if err != nil {
		return nil, func() {}, err
	}
	return []Arg{Arg(m.Flag + path)}, cleanup, nil
}

// osTempCreate opens a fresh temp file; indirected so the write-failure path is
// testable (a test substitutes a create that returns an already-closed file).
var osTempCreate = func() (*os.File, error) { return os.CreateTemp("", "stickler-config-*.yaml") }

// OSTempWriter is the production TempWriter, writing the effective config to a
// uniquely-named temp file and returning a cleanup that removes it.
func OSTempWriter(data []byte) (string, func(), error) {
	file, err := osTempCreate()
	if err != nil {
		return "", func() {}, err
	}
	if writeErr := writeAndClose(file, data); writeErr != nil {
		_ = os.Remove(file.Name())
		return "", func() {}, writeErr
	}
	return file.Name(), func() { _ = os.Remove(file.Name()) }, nil
}

// writeAndClose writes data to file and closes it, returning the first error.
func writeAndClose(file *os.File, data []byte) error {
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
