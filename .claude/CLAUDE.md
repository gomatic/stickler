# stickler

The gomatic **lint runner**: it executes the configured checks to completion, normalizes their findings into one diagnostic schema, and reports pass or fail through the process exit code. It is a CLI, not a library — nothing outside this module imports it.

## Soft is two different things, and they are declared apart

A rule can be non-gating for two unrelated reasons, and conflating them is what made a rule documented as advisory block every push in the fleet.

- **A ROLLOUT ratchet** — a rule that gates BY RIGHT, softened while a backlog is worked down. It lives in `soft:`, its count is capped by `soft-baseline:`, an absent entry caps it at ZERO, and exceeding the cap fails the build. It returns to hard at zero.
- **A PROBE** — a rule whose precision is bounded by judgment rather than syntax. It lives in `probe:` in the GLOBAL configuration only, it never gates at any count, and no baseline applies to it. Its findings are printed and counted so a reader still sees them; the remedy is to adjudicate the finding or fix the analyzer — never to record a number, and never to reword the prose the probe read.

A repository may not declare a probe of its own: the load is refused with `ErrProbeNotGlobal`, for the KEY in any spelling — a list, a directive, an empty mapping, a bare `probe:` — because refusing only the spellings that fold to something would leave the rest silently ignored. Whether an analyzer is judgment-bound is a property of the analyzer, and a repo-declared probe would be an unbounded, uncounted escape from a rule that gates by right — strictly weaker than the baseline it would replace.

That rule is only as good as knowing which layer is which, so the global config path must be ABSOLUTE and an unresolvable one yields NO global layer rather than a guessed one. Joining an empty home gives the relative `.config/stickler/config.yaml`, which resolves against the working directory — inside the tree being linted — and a repository that committed that path would be handed the global scope over its own lint. A container with no `HOME` reaches that state on its own.

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
