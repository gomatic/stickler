// The shape of every yupsh/yup-* repository: a single-verb wrapper that
// declares a spec and hands it to a framework driver. It imports urfave only to
// spell its flag TYPES and to take a *cli.Command parameter — it constructs no
// command, owns no verbs, and must never be asked for a command tier.
package main

import (
	clix "example.com/clix"
	urf "github.com/urfave/cli/v3"
)

var spec = clix.Spec{
	Name:  "cat",
	Flags: []urf.Flag{&urf.BoolFlag{Name: "number"}},
}

func options(c *urf.Command) []any { return []any{c.Bool("number")} }

func main() { clix.Main(spec, options) }
