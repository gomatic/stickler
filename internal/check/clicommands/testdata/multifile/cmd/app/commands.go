package main

import "github.com/urfave/cli/v3"

// A second file of the SAME program that also builds commands: one program is
// one finding, however many files it is spread across.
func subcommands() []*cli.Command { return []*cli.Command{{Name: "sub"}} }
