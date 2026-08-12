package config

// The one resolution test that observes its result through the unexported
// merge: what Resolve delivers to yze is only visible once the overlays are
// folded, and folding them is not public API.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// parseConfig decodes one configuration layer from YAML.
func parseConfig(t *testing.T, text string) Config {
	t.Helper()
	var cfg Config
	require.NoError(t, yaml.Unmarshal([]byte(text), &cfg))
	return cfg
}

// TestResolveAppendsAnalyzerOverlayAfterConfigOverlays pins precedence: the
// typed `analyzers:` surface is delivered last, so it wins over a raw
// `config: yze:` entry for the same key.
func TestResolveAppendsAnalyzerOverlayAfterConfigOverlays(t *testing.T) {
	resolved := Resolve(parseConfig(t, `
config:
  yze:
    analyzers:
      ptrparam:
        allow: [from.config.block]
analyzers:
  ptrparam:
    allow: [from.analyzers.block]
`))

	overlays := resolved.Config["yze"]
	require.Len(t, overlays, 2)
	effective := mergeTree(map[string]any{}, overlays)
	analyzers, ok := effective["analyzers"].(map[string]any)
	require.True(t, ok)
	settings, ok := analyzers["ptrparam"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{"from.analyzers.block"}, settings["allow"],
		"the typed analyzers: surface is authoritative")
}
