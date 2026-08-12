// Package solo declares the contract in an untagged file, so it IS a domain
// verb — and with no command package in front of it, it must be reported,
// anchored here rather than at the build-excluded gen.go.
package solo

type Config struct{ Name string }

type Result struct{ Out string }

func Run(cfg Config) (Result, error) { return Result{Out: cfg.Name}, nil }
