package lint

import "time"

// Timeout bounds an entire lint pass, so a wedged linter cannot hang the run
// forever.
type Timeout time.Duration

// DefaultTimeout applies when a caller names no timeout.
const DefaultTimeout Timeout = Timeout(5 * time.Minute)

// duration converts the bound flag back to the standard duration a context
// deadline is spelled with, defaulting a zero timeout so a caller that builds a
// Config by hand does not get an already-expired deadline.
func (t Timeout) duration() time.Duration {
	if t <= 0 {
		return time.Duration(DefaultTimeout)
	}
	return time.Duration(t)
}
