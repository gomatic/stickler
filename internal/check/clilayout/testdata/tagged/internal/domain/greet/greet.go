//go:build ignore

// Package greet declares the full domain contract, but this file is excluded
// from the default build by its constraint — the layout check must not see it
// as a domain verb demanding a command package.
package greet

type Config struct{ Name string }

type Result struct{ Out string }

func Run(cfg Config) (Result, error) { return Result{Out: cfg.Name}, nil }
