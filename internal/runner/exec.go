// Package runner turns a declared tool (a config.RunnerSpec) into something
// that runs: it merges the tool's effective config, expands its argument and
// environment templates, executes the subprocess, and reads the output through
// the spec's parser.
//
// The native checks stickler implements itself are NOT registered here — they
// are injected (see [Registry]), which is what keeps this package from
// depending on the checks that depend on it.
package runner

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/gomatic/stickler/internal/constants"
)

// Name identifies a runner's binary (the first word of its command).
type Name string

// Arg is one command-line argument passed to a runner's binary.
type Arg string

// EnvVar is one KEY=value environment entry a runner's subprocess receives on
// top of the inherited environment. A key set here shadows the ambient value:
// the entries are appended after os.Environ, and the last occurrence of a key
// wins for the started process.
type EnvVar string

// Command runs an external tool and returns its stdout. A non-nil error includes
// a non-zero exit; callers that can still parse the stdout (linters exit non-zero
// when they report findings) treat the output as authoritative.
type Command func(ctx context.Context, name Name, env []EnvVar, args ...Arg) ([]byte, error)

// ExecCommand backs Command, and the assertion below is the compile-checked
// evidence: composition roots bind it, tests substitute a failing one.
var _ Command = ExecCommand

// ExecCommand is the default Command, executing a real subprocess. On failure the
// returned error wraps ErrExec with the captured stderr so the underlying reason
// (config error, panic, load failure) reaches the caller's message.
func ExecCommand(ctx context.Context, name Name, env []EnvVar, args ...Arg) ([]byte, error) {
	bin, list := string(name), stringArgs(args)
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, list...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), stringEnv(env)...)
	}
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, constants.ErrExec.With(err, "stderr", strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// stringEnv converts typed environment entries to the plain strings exec expects.
func stringEnv(env []EnvVar) []string {
	out := make([]string, len(env))
	for i, e := range env {
		out[i] = string(e)
	}
	return out
}

// stringArgs converts typed arguments to the plain strings exec expects.
func stringArgs(args []Arg) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = string(a)
	}
	return out
}

// toArgs converts a string slice to typed Args.
func toArgs(in []string) []Arg {
	out := make([]Arg, len(in))
	for i, s := range in {
		out[i] = Arg(s)
	}
	return out
}
