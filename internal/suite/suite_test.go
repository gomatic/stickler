package suite_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gomatic/stickler/internal/suite"
)

// TestRootDirNormalizesPackagePatterns pins the root translation: package
// patterns walk their directory, and an empty root walks the working
// directory.
func TestRootDirNormalizesPackagePatterns(t *testing.T) {
	t.Parallel()
	want := assert.New(t)

	want.Equal(suite.Dir("."), suite.Root("./...").Dir())
	want.Equal(suite.Dir("."), suite.Root("").Dir())
	want.Equal(suite.Dir("."), suite.Root("...").Dir())
	want.Equal(suite.Dir("sub/dir"), suite.Root("sub/dir/...").Dir())
	want.Equal(suite.Dir("sub/dir"), suite.Root("sub/dir").Dir())
}
