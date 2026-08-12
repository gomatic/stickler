// Package helper is the symmetric shape: the COMMAND marker nested beneath the
// domain tree. It belongs to the domain tier, declares no contract, and binds
// nothing. (Named helper, not aux — aux is a Windows-reserved name that a Go
// module zip refuses, which is how the first spelling broke the v0.9.2 tag.)
package helper

// Command is helper vocabulary that must not create a phantom command tier.
func Command() string { return "helper" }
