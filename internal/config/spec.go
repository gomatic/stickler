package config

// A tool is DATA. Everything about how a checker is invoked — its binary, its
// argument template, its config-file wiring, the parser its stdout is read
// with — is declared here and in a repository's .stickler.yaml, never in Go.
// The only per-tool code stickler carries is an output parser.

// Argument placeholders substituted in a RunnerSpec's Args at run time.
const (
	PlaceholderRoot   = "{root}"
	PlaceholderConfig = "{config}"
	// PlaceholderCache, in a RunnerSpec's Env entries, expands to a fresh
	// per-invocation temporary directory removed when the run finishes. It
	// exists so a tool's result cache can be made hermetic: a shared mutable
	// cache is a channel through which a stale entry silently suppresses a
	// real finding (observed in the field — a poisoned global golangci-lint
	// cache hid a genuine exhaustive violation across many runs until the
	// file's content changed), and a gate that can be greened by ambient
	// state is not a gate.
	PlaceholderCache = "{cache}"
)

// ParserName selects the output parser a runner's stdout is read with.
type ParserName string

// Built-in parser names.
const (
	ParserSticklerJSON ParserName = "stickler-json"
	ParserGolangciJSON ParserName = "golangci-json"
)

// Built-in tool literals (named so the spec table and adapters share one source).
const (
	ToolYze      = "yze"
	ToolGolangci = "golangci-lint"

	golangciRunVerb  = "run"
	golangciJSONFlag = "--output.json.path=stdout"
	golangciCfgFlag  = "--config={path}"
	golangciBaseYAML = ".golangci.yaml"
	golangciBaseYML  = ".golangci.yml"
	yzeCfgFlag       = "--config={path}"
	yzeBaseYAML      = ".yze.yaml"
	yzeBaseYML       = ".yze.yml"
	// golangciParallelFlag disables golangci-lint's start-up file lock. That lock
	// is global (a fixed path, not under GOLANGCI_LINT_CACHE), so N concurrent
	// runs — a `git repo list | xargs -P<N> make check` fleet sweep — otherwise
	// have N-1 die with "parallel golangci-lint is running" (exit 3). The cache
	// is content-addressed and safe for concurrent runners, which is why
	// golangci ships this opt-out.
	golangciParallelFlag = "--allow-parallel-runners"
	// golangciCacheEnv is golangci-lint's cache-directory selector, bound to
	// the `{cache}` placeholder in the default spec so each run gets a fresh
	// hermetic cache instead of the shared global one.
	golangciCacheEnv = "GOLANGCI_LINT_CACHE"
)

// Spec declares, as data, how a tool takes its configuration file: the base
// config filename candidates (first found wins) the stickler overlays are merged
// onto, and the flag template (`{path}` substituted) that passes the effective
// config. It carries no tool-specific behavior — the merge is generic.
type Spec struct {
	Flag string   `yaml:"flag"`
	Base []string `yaml:"base"`
}

// RunnerSpec is the declarative definition of a tool stickler runs: its command,
// its argument template (with `{root}`/`{config}` placeholders), the parser its
// stdout is read with, and optionally how its config file is wired. A tool is data
// here, not code — adding one is configuration, not a recompile.
// Fields are ordered for struct-field alignment (slices last); the YAML schema is
// unaffected since decoding is by tag.
type RunnerSpec struct {
	Name    string     `yaml:"name"`
	Format  ParserName `yaml:"format"`
	Config  *Spec      `yaml:"config"`
	Command []string   `yaml:"command"`
	Args    []string   `yaml:"args"`
	// Env entries are appended to the subprocess environment, shadowing
	// same-keyed ambient values. An entry may carry the `{cache}` placeholder,
	// which expands to a fresh per-invocation temporary directory (removed
	// after the run) — the hermetic-cache seam.
	Env []string `yaml:"env"`
}

// DefaultRunnerSpecs is the built-in tool set as pure data: yze (native
// stickler-json, config merged from the repo's .yze.yaml plus the resolved
// `analyzers:` settings) and golangci-lint (adapted JSON, config merged from
// the repo's .golangci.yaml). A .stickler.yaml `define:` block overrides or
// extends this map without touching Go.
func DefaultRunnerSpecs() map[string]RunnerSpec {
	return map[string]RunnerSpec{
		ToolYze: {
			Name:    ToolYze,
			Command: []string{ToolYze},
			Args: []string{
				"--format", string(ParserSticklerJSON), PlaceholderConfig, "--", PlaceholderRoot,
			},
			Format: ParserSticklerJSON,
			Config: &Spec{Base: []string{yzeBaseYAML, yzeBaseYML}, Flag: yzeCfgFlag},
		},
		ToolGolangci: {
			Name:    ToolGolangci,
			Command: []string{ToolGolangci, golangciRunVerb},
			Args:    []string{golangciJSONFlag, golangciParallelFlag, PlaceholderConfig, "--", PlaceholderRoot},
			Format:  ParserGolangciJSON,
			Config:  &Spec{Base: []string{golangciBaseYAML, golangciBaseYML}, Flag: golangciCfgFlag},
			// The issues cache is hermetic per invocation: golangci-lint's
			// shared global cache is a channel through which a stale entry
			// can silently green the gate (see PlaceholderCache), so every
			// run analyzes from scratch in its own throwaway cache.
			Env: []string{golangciCacheEnv + "=" + PlaceholderCache},
		},
	}
}
