// Package tenant is the pre-fix kilroy shape: one command package holding
// three verbs as unexported constructors, with three domain verb packages
// beneath it that no command package matches.
package tenant

const name = "tenant"

func Command() string { return name }
