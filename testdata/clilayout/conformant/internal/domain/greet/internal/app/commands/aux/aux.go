// Package aux is the symmetric shape: the COMMAND marker nested beneath the
// domain tree. It belongs to the domain tier, declares no contract, and binds
// nothing.
package aux

// Command is helper vocabulary that must not create a phantom command tier.
func Command() string { return "aux" }
