// The shape of every yze-go-* analyzer repository: a main that hands one
// analyzer to a framework driver. It has no verbs and no flags of its own, so
// it must never be asked for a command tier.
package main

import "golang.org/x/tools/go/analysis/singlechecker"

func main() { singlechecker.Main(nil) }
