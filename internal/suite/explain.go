package suite

import "context"

// Explainer is a runner that can state the rules it enforces, in prose an agent
// can write conforming code from.
//
// It is OPTIONAL, and deliberately separate from Runner: a tool that reports
// findings need not be able to explain itself, and pretending otherwise would
// mean inventing prose on its behalf. A runner that does not implement this is
// reported as unable to explain itself — which is a fact worth showing, not one
// worth papering over with a guess.
type Explainer interface {
	Explain(ctx context.Context) (Instructions, error)
}

// Instructions is what one runner has to say about the rules it enforces.
type Instructions string
