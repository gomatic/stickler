package runner

import (
	"os"
	"strings"

	"github.com/gomatic/stickler/internal/config"
	"github.com/gomatic/stickler/internal/constants"
)

// cacheDir is the per-invocation hermetic cache directory substituted for the
// `{cache}` placeholder; empty when a template carries no placeholder.
type cacheDir string

// resolveEnv expands a spec's Env template into concrete entries. When any
// entry carries the `{cache}` placeholder, one fresh temporary directory is
// created for the invocation, substituted into every entry, and removed by the
// returned cleanup; with no placeholder the cleanup is a no-op.
func resolveEnv(template []string) ([]EnvVar, func(), error) {
	nothing := func() {}
	if !mentionsCache(template) {
		return toEnv(template, ""), nothing, nil
	}
	dir, err := osMkdirTemp("", "stickler-cache-")
	if err != nil {
		return nil, nothing, constants.ErrRunnerFailed.With(err, "creating", "hermetic cache directory")
	}
	return toEnv(template, cacheDir(dir)), func() { _ = osRemoveAll(dir) }, nil
}

// mentionsCache reports whether any env entry carries the cache placeholder.
func mentionsCache(template []string) bool {
	for _, entry := range template {
		if strings.Contains(entry, config.PlaceholderCache) {
			return true
		}
	}
	return false
}

// toEnv expands the env template, substituting the cache placeholder with dir.
func toEnv(template []string, dir cacheDir) []EnvVar {
	if len(template) == 0 {
		return nil
	}
	out := make([]EnvVar, len(template))
	for i, entry := range template {
		out[i] = EnvVar(strings.ReplaceAll(entry, config.PlaceholderCache, string(dir)))
	}
	return out
}

// osMkdirTemp and osRemoveAll are indirection seams so the hermetic-cache
// failure path is testable without an unwritable real filesystem.
var (
	osMkdirTemp = os.MkdirTemp
	osRemoveAll = os.RemoveAll
)
