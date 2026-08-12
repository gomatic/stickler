package main

import "github.com/urfave/cli/v3"

// The shape stickler itself had: a real urfave command tree with its whole
// implementation somewhere other than internal/app/commands.
func main() { _ = &cli.Command{Name: "app", Action: nil} }
