// A flat CLI that imports urfave under an alias: the check must follow the
// alias, or renaming the import would silently exempt a repository.
package main

import urf "github.com/urfave/cli/v3"

func main() { _ = &urf.Command{Name: "app"} }
