package lint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestTimeoutDefaultsRatherThanExpiringImmediately pins the zero case: a Config
// built by hand carries no timeout, and a zero deadline would cancel the pass
// before any check ran.
func TestTimeoutDefaultsRatherThanExpiringImmediately(t *testing.T) {
	t.Parallel()
	assert.Equal(t, time.Duration(DefaultTimeout), Timeout(0).duration())
	assert.Equal(t, time.Duration(DefaultTimeout), Timeout(-1).duration())
	assert.Equal(t, 3*time.Second, Timeout(3*time.Second).duration())
	assert.Positive(t, DefaultTimeout, "an unbounded pass can hang a job indefinitely")
}
