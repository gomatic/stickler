package main

import (
	"github.com/urfave/cli/v3"

	"example.com/tiered/internal/app/commands/greet"
)

func main() { _ = &cli.Command{Commands: []*cli.Command{greet.Command()}} }
