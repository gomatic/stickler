package main

import "github.com/urfave/cli/v3"

func main() { _ = &cli.Command{Name: "app", Commands: subcommands()} }
