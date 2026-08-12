# stickler

The gomatic **lint runner**: it executes the configured checks to completion, normalizes their findings into one diagnostic schema, and reports pass or fail through the process exit code. It is a CLI, not a library — nothing outside this module imports it.

## The rule that shapes everything here

**A tool is DATA, not code** (standing rule R1). Which checkers run, how each is invoked, how its configuration is merged, and how its output is parsed are declared in `.stickler.yaml` layered over the global configuration — never in Go. The only per-tool Go code stickler carries is an output parser. Adding a tool is configuration; if a change requires a new Go branch per tool, it is the wrong change.

The exception is a **native check**: one whose question is a whole-repo fact no single-package analyzer pass can see (`stickler/clilayout`, `stickler/binaries`). Everything else belongs in an external tool wired through a `RunnerSpec`.

## Layout

Strict three-tier, uniform with [`gomatic/template.cli`](https://github.com/gomatic/template.cli):

| Tier | Package |
| --- | --- |
| app — flags only | [internal/app/commands/lint](internal/app/commands/lint) |
| domain — orchestration | [internal/domain/lint](internal/domain/lint) (`Run`), [internal/domain](internal/domain) (the shared `Argument` alias) |
| implementation | [internal/suite](internal/suite), [internal/config](internal/config), [internal/runner](internal/runner), [internal/report](internal/report), [internal/check](internal/check) |
| sentinels | [internal/constants](internal/constants) |

Dependencies run one way and must stay acyclic: `suite` and `config` are leaves; `runner` reads both; `report` and `check/*` read `suite`; `domain/lint` wires them together.

**The native checks are injected, never registered.** `internal/domain/lint` is the composition root that hands them to `runner.Registry`. A registry of native checks living inside `internal/runner` would make it depend on the check packages that depend on it — that cycle is why the injection exists, so do not "simplify" it back.

`lint` owns stdout: its `github` and `sarif` formats are wire formats a CI runner or code scanner parses, so it binds `app.Interactive` and takes its writer on the `Config`. `Run` still returns a structured `Result`, which is what lets a second caller (a server, a composing workflow) reuse the pass without a second implementation of it.

`cmd/stickler` is wiring only, and sets `DefaultCommand` so the bare `stickler [root]` every CI job runs still means a lint pass.

## Shared libraries it consumes

- [`gomatic/go-app`](https://github.com/gomatic/go-app) — the urfave/cli v3 framework (`Interactive`, `GetLogger`, the logger-in-metadata convention).
- [`gomatic/go-log`](https://github.com/gomatic/go-log) — slog setup (`LoggerConfig`, `NewLogger`).
- [`gomatic/go-error`](https://github.com/gomatic/go-error) — the sentinel `errs.Const` type; this repo declares only its own values, in `internal/constants`.
- [`gomatic/go-yze`](https://github.com/gomatic/go-yze) — the `Diagnostic` schema every runner normalizes into.

## Working on it

- Run the gate with `GOWORK=off make check`; an active `go.work` that excludes this module makes every runner fail to load packages.
- **Run the tool on itself before claiming anything.** `GOWORK=off go run ./cmd/stickler` must print nothing and exit zero. A lint runner that reports its own violations and calls it a pass is the failure this repo exists to prevent.
- `.stickler.yaml` carries **no** `soft-baseline` block, and adding one is a decision, not a fix. A finding is removed at its root cause; a baseline that absorbs it makes the violation permanent and invisible.
- Specs live in [`gomatic/_projects/specs/stickler/`](https://github.com/gomatic/.projects), never in this repo.
