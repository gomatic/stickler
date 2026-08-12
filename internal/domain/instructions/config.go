package instructions

import (
	"io"

	"github.com/gomatic/stickler/internal/config"
)

// Config holds the flags for the instructions command. Its fields are bound by
// the CLI tier and read by Run; it carries no behavior.
type Config struct {
	Out  io.Writer
	Root config.RepoRoot
}
