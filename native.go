package stickler

// The native-check registry: checks implemented inside stickler itself rather
// than as external tools. A native check earns its place here only when its
// question is a whole-repo fact no single-package analyzer pass can see —
// everything else belongs in an external tool wired through a RunnerSpec.

import "slices"

// nativeRunners is the registry of native checks, keyed like the spec registry.
func nativeRunners() map[specName]Runner {
	return map[specName]Runner{
		clilayoutName: clilayoutRunner{},
		binariesName:  binariesRunner{},
	}
}

// defaultNames is every defined spec plus every native check, in one
// deterministic order.
func defaultNames(specs map[string]RunnerSpec, native map[specName]Runner) []string {
	names := sortedKeys(specs)
	for name := range native {
		if _, defined := specs[string(name)]; !defined {
			names = append(names, string(name))
		}
	}
	slices.Sort(names)
	return names
}

// resolveRunner resolves one name: a defined spec wins — so a repository can
// rewire even a native check from configuration — then a native check.
func resolveRunner(
	command Command, specs map[string]RunnerSpec, native map[specName]Runner, name specName, ctx RunnerContext,
) (Runner, bool) {
	if runner, ok := newSpecRunner(command, specs[string(name)], name, ctx); ok {
		return runner, true
	}
	runner, ok := native[name]
	return runner, ok
}
