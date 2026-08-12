// A FIXTURE inside a repository's own testdata: a flat CLI that exists to be
// judged by somebody else's tests. Judging it as this repository's layout
// would report a finding nobody can fix.
package main

import "github.com/urfave/cli/v3"

func main() { _ = &cli.Command{Name: "fixture"} }
