// Package fixture is a test fixture beneath a testdata directory: the walk
// prunes it exactly as the go tool would, so its unmatched Command binds
// nothing.
package fixture

func Command() string { return "fixture" }
