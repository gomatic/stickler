// Package lint orchestrates a whole lint pass.
//
// It defines the command's Config (the flags the CLI binds) and Run (the
// orchestration entry point the CLI invokes). Run resolves the configuration
// layers, assembles the runners — the tools declared as data, plus the native
// checks stickler implements itself — executes them under one timeout, renders
// the report, and decides pass or fail. It contains no CLI or flag logic, and
// no knowledge of how any single check works. This is the domain tier: the
// seam between the app tier (internal/app/commands/lint) and the
// implementation tier (internal/config, internal/runner, internal/report,
// internal/check).
//
// Run BOTH renders and returns. The rendering is the CLI's product — `github`
// and `sarif` are wire formats a runner or a scanner parses, not encodings a
// generic result encoder could supply — and the returned Result is what makes
// a second caller possible: a server or a workflow passes a discarding writer
// and reads the structured findings, without a second implementation of the
// pass.
package lint
