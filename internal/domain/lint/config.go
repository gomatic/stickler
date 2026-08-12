package lint

import (
	"io"

	"github.com/gomatic/stickler/internal/config"
	"github.com/gomatic/stickler/internal/report"
)

// Config holds the flags and arguments for the lint command. Its fields are
// bound by the CLI tier and read by Run; it carries no behavior.
//
// Out is the report's destination rather than a hardcoded stdout, so a caller
// that wants the findings as data — a server, a composing workflow — passes a
// discarding writer and reads the returned Result instead.
type Config struct {
	Out     io.Writer
	Format  report.OutputFormat
	Root    config.RepoRoot
	Timeout Timeout
}
