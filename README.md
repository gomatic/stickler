# stickler

The gomatic **lint runner** — a stickler for the rules. It executes a set of analyzer tools (the [`yze`](https://github.com/gomatic/yze) suite, `golangci-lint`, and others) to completion, normalizes their findings into one diagnostic schema, writes them to stderr in the chosen format, and **reports pass/fail via the process exit code**. Any finding or any tool error is a stickler failure; every tool still runs to completion first.

```
stickler [--format human|json|github] [root]
```

- **Runner model** — each tool is an executable plus an output adapter (`Runner`). `yze` is read via its native stickler-json (no adapter); `golangci-lint`'s JSON is adapted. New tools are added as more `Runner`s.
- **`--format`** — `human` (default, one line per finding), `json` (the normalized result), `github` (Actions annotations). SARIF output is planned.
- **Exit code** — `0` only when every tool ran cleanly with zero findings; non-zero on any finding or tool error.

Built on the [`go-yze`](https://github.com/gomatic/go-yze) diagnostic schema. `--fix` (delegating to each tool's fixer) and a `stickler.yaml` tool configuration are planned; v1 runs `yze` + `golangci-lint` zero-config.
