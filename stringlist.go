package stickler

import (
	"slices"

	"gopkg.in/yaml.v3"
)

// listDirectives is the mapping form of a StringList: explicit add/remove/replace
// instructions a configuration layer applies to the accumulated value.
type listDirectives struct {
	Add     []string `yaml:"add"`
	Remove  []string `yaml:"remove"`
	Replace []string `yaml:"replace"`
}

// listDirectiveKeys is the set of mapping keys a StringList accepts. Any other key
// is a configuration typo and is rejected rather than silently ignored.
var listDirectiveKeys = map[string]bool{"add": true, "remove": true, "replace": true}

// StringList is a list-valued setting in one configuration layer. It merges onto
// the value accumulated from lower layers either by replacing it (when written as
// a YAML sequence) or by adding and removing entries (when written as a mapping
// with add/remove/replace keys). An absent setting leaves the lower value intact.
type StringList struct {
	replace []string
	add     []string
	remove  []string
}

// UnmarshalYAML accepts a sequence (replace) or an add/remove/replace mapping,
// rejecting any unknown mapping key (a config typo such as `addd`). The pointer
// receiver and *yaml.Node parameter are dictated by the yaml.Unmarshaler interface,
// which a polymorphic (sequence-or-mapping) setting must implement.
func (l *StringList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.SequenceNode:
		return node.Decode(&l.replace)
	case yaml.MappingNode:
		if key, ok := unknownDirectiveKey(node.Content); ok {
			return ErrBadListSetting.With(nil, "key", key)
		}
		var directives listDirectives
		if err := node.Decode(&directives); err != nil {
			return err
		}
		l.add, l.remove, l.replace = directives.Add, directives.Remove, directives.Replace
		return nil
	default:
		return ErrBadListSetting
	}
}

// unknownDirectiveKey returns the first mapping key that is not a recognized list
// directive, scanning the key nodes (the even indices of a mapping's content).
func unknownDirectiveKey(content []*yaml.Node) (string, bool) {
	for i := 0; i < len(content); i += 2 {
		if key := content[i].Value; !listDirectiveKeys[key] {
			return key, true
		}
	}
	return "", false
}

// applyTo folds this layer's directives onto the accumulated base value. It is
// purely functional: base (and any layer's replace slice) is never mutated, so a
// slice shared across layers cannot be corrupted by a later layer's edits.
func (l StringList) applyTo(base []string) []string {
	out := slices.Clone(base)
	if l.replace != nil {
		out = slices.Clone(l.replace)
	}
	out = append(out, l.add...)
	return slices.DeleteFunc(out, func(s string) bool { return slices.Contains(l.remove, s) })
}
