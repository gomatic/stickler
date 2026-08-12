// Package help is a command-local helper whose path nests the DOMAIN marker
// beneath the command tree. It belongs to the command tier (where it declares
// no entry point and binds nothing) — not to a phantom domain tier demanding a
// nonsense counterpart path.
package help

// Config is ordinary helper vocabulary here, not the domain contract.
type Config struct{ Name string }
