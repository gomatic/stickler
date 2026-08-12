package config

// The generic config document: parse to a tree, deep-merge layers, marshal
// back. Kept apart from the merger that decides WHICH layers apply, because
// this half knows nothing about runners or repositories — it is document
// arithmetic, and it is where "an overlay must never mutate the base" lives.

import (
	"maps"

	"gopkg.in/yaml.v3"

	"github.com/gomatic/stickler/internal/constants"
)

// Overlay is one configuration layer's overlay for a single tool, taken from that
// tool's entry under the `config:` block of a .stickler.yaml. It is deep-merged
// onto the tool's own base config file at run time, so per-repo tool-config deltas
// live in .stickler.yaml instead of in a divergent, unmanaged base config. A
// mapping value deep-merges; a scalar or sequence replaces; a mapping written with
// only add/remove/replace keys mutates the base list (the StringList polymorphism).
type Overlay map[string]any

// parseTree decodes a base config document into a generic tree, or an empty tree
// when data is empty or null. A malformed document is a configuration error.
func parseTree(data []byte) (map[string]any, error) {
	var tree map[string]any
	if err := yaml.Unmarshal(data, &tree); err != nil {
		return nil, constants.ErrConfig.With(err)
	}
	if tree == nil {
		return map[string]any{}, nil
	}
	return tree, nil
}

// marshalTree renders an effective configuration tree as a YAML document.
func marshalTree(tree map[string]any) ([]byte, error) {
	data, err := yaml.Marshal(tree)
	if err != nil {
		return nil, constants.ErrConfig.With(err)
	}
	return data, nil
}

// mergeTree folds each overlay, in layer order, onto base and returns the effective
// configuration tree. base is never mutated.
func mergeTree(base map[string]any, overlays []Overlay) map[string]any {
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

// cloneMap returns a shallow copy of m (empty for a nil map), so a merge never
// mutates a caller's map; nested maps are copied as merges recurse into them.
func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	maps.Copy(out, m)
	return out
}

// asMap coerces a decoded mapping to map[string]any, accepting both the plain type
// (as parseTree yields) and the named Overlay type (as yaml.v3 yields when decoding
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
