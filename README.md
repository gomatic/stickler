# stickler

The gomatic **lint runner** — a stickler for the rules. It executes a set of analyzer tools (the [`yze`](https://github.com/gomatic/yze) suite, `golangci-lint`, and others) to completion, normalizes their findings into one diagnostic schema, writes the report to stdout in the chosen format, prints a pass/fail status to stderr, and **reports pass/fail via the process exit code**. Any finding or any tool error is a stickler failure; every tool still runs to completion first.

```
stickler [--format human|json|github] [root]
```

- **Runner model** — each tool is an executable plus an output adapter (`Runner`). `yze` is read via its native stickler-json (no adapter); `golangci-lint`'s JSON is adapted. New tools are added as more `Runner`s.
- **`--format`** — `human` (default, one line per finding), `json` (the normalized result), `github` (Actions annotations). SARIF output is planned.
- **Exit code** — `0` only when every tool ran cleanly with zero findings; non-zero on any finding or tool error.

## Configuration (layered merge)

stickler is a runner, so its configuration is about configuring the tools it runs. Two layers merge, global first then repo:

- **Global:** `$XDG_CONFIG_HOME/stickler/config.yaml` (or `~/.config/stickler/config.yaml`).
- **Repo:** `.stickler.yaml` at the target (or `--root`).

```yaml
runners: [yze, golangci-lint]   # which tools to run
format: human                   # default output (overridable by --format)
analyzers:                      # per-analyzer settings (forwarded to yze)
  ptrrecv:
    allow: [mypkg.MyMutexType]
```

A repo layer can **replace**, **add**, or **remove** relative to the global layer. A list written as a sequence replaces; a list written as a mapping adds/removes:

```yaml
runners:
  add: [revive]
  remove: [golangci-lint]
```

Maps (the `analyzers` tree) deep-merge; each setting's list follows the same replace/add/remove rules. Precedence for output format is `--format` flag > config > `human`.

### Per-tool config overlays (`config:`)

A `config:` block carries a **generic, per-tool configuration overlay** keyed by runner name. Each overlay is deep-merged onto that tool's *own* base config file at run time, so per-repo lint deltas live in `.stickler.yaml` instead of in a divergent, unmanaged base config. The merge is generic — golangci-lint is just one configured tool — and follows the same polymorphism as everything else: a mapping deep-merges, a scalar or sequence replaces, and a mapping written with only `add`/`remove`/`replace` keys mutates the base list.

```yaml
config:
  golangci-lint:                  # merged onto the repo's managed .golangci.yaml
    linters:
      settings:
        gosec:
          excludes: { add: [G204] }   # add a per-repo exclude on top of the central config
```

At lint time stickler reads the tool's base config (for golangci-lint, the repo's `.golangci.yaml`/`.golangci.yml`), folds the overlays onto it, writes the effective config to a temp file, and runs the tool with `--config` pointing at it. With no overlay the tool is run as before (its own config discovery). This is how a repo keeps the **uniform, managed** base config while still expressing the handful of rules it genuinely needs to differ on.

Built on the [`go-yze`](https://github.com/gomatic/go-yze) diagnostic schema. Forwarding the merged `analyzers` settings to `yze --config`, and `--fix` (delegating to each tool's fixer), are the next steps.
