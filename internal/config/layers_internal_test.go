package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/gomatic/stickler/internal/constants"
)

// TestParseLayerSeesAKeyTheSettingItselfNeverSees names parseLayer's claim, and
// is the reason it reads the bytes twice. `probe:` with a null value is a key
// the layer WROTE, but yaml resolves the null before any Unmarshaler runs, so
// the setting never sees it and folds to nothing. A refusal built on the folded
// value would let that spelling through silently; one built on the key does not.
func TestParseLayerSeesAKeyTheSettingItselfNeverSees(t *testing.T) {
	t.Parallel()

	_, err := parseLayer([]byte("probe:\n"), Layer{Path: "repo/.stickler.yaml", Scope: ScopeRepo})

	require.ErrorIs(t, err, constants.ErrProbeNotGlobal)
	assert.Contains(t, err.Error(), "repo/.stickler.yaml", "the refusal names the file to edit")

	var direct Config
	require.NoError(t, yaml.Unmarshal([]byte("probe:\n"), &direct),
		"the setting itself decodes without complaint")
	assert.Empty(t, direct.Probe.applyTo(nil),
		"and folds to nothing, which is why the KEY is what has to be refused")
}

// TestParseLayerStillRefusesAMistypedSettingAfterTheKeyScan pins that the key
// scan is an ADDITION to the decode, not a replacement for it: a document whose
// keys are all permitted must still be decoded, and a setting written as the
// wrong shape is still a configuration error naming the file.
func TestParseLayerStillRefusesAMistypedSettingAfterTheKeyScan(t *testing.T) {
	t.Parallel()

	_, err := parseLayer([]byte("runners: nope\n"), Layer{Path: "repo/.stickler.yaml", Scope: ScopeRepo})

	require.ErrorIs(t, err, constants.ErrConfig)
	assert.ErrorIs(t, err, constants.ErrBadListSetting, "the cause survives the wrap")
	assert.Contains(t, err.Error(), "repo/.stickler.yaml")
}
