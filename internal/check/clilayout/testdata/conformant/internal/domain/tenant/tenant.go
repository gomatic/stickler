// Package tenant is a grouping package: helpers only, no contract, no
// counterpart requirement. Its declarations deliberately span every
// non-contract shape — a const block, a non-contract type with a method, and
// a helper func — none of which makes it a verb.
package tenant

const maxLen = 64

type name string

func (n name) trimmed() name {
	if len(n) > maxLen {
		return n[:maxLen]
	}
	return n
}

func Normalize(raw string) string { return string(name(raw).trimmed()) }
