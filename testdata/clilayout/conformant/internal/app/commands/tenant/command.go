// Package tenant is a mount-only parent: it declares Command and has a
// self-declaring command beneath it, so it needs no domain verb of its own.
package tenant

const name = "tenant"

func Command() string { return name }
