//go:build ignore

// A build-excluded sibling: skipping it must not hide the package's real,
// untagged declaration in solo.go.
package solo

type Config struct{ Kept bool }
