// Package runonly declares only the Run element of the contract — still a
// verb, still requiring a command package.
package runonly

func Run(args ...string) (int, error) { return len(args), nil }
