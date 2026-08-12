// Package instructions orchestrates "stickler instructions": it asks every
// configured check to state the rules it enforces, and renders the answers as
// one document an agent can write conforming code from.
//
// It AUTHORS nothing about a tool it does not own. Rule R1 makes a tool data,
// not code, so what yze's rules mean is yze's to say — stickler asks the tool
// (yze already exports its whole catalog) rather than keeping prose about it in
// this repository, where it would be a second source of truth that drifts from
// the analyzer that actually decides. The native checks are the exception, and
// only because stickler owns them: for those, this IS the source.
package instructions
