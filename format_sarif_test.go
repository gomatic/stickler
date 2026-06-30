package stickler_test

import (
	"bytes"
	"encoding/json"
	"testing"

	goyze "github.com/gomatic/go-yze"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gomatic/stickler"
)

func TestFormatSARIFEmitsResults(t *testing.T) {
	diags := []goyze.Diagnostic{
		{Rule: "yze/gotostmt", Path: "a.go", Line: 3, Col: 2, Severity: goyze.SeverityError, Message: "goto"},
		{Rule: "gosec", Path: "b.go", Line: 7, Col: 1, Severity: goyze.SeverityWarning, Message: "weak"},
		{Rule: "yze/boolname", Path: "c.go", Line: 1, Col: 1, Severity: goyze.SeverityInfo, Message: "name"},
	}
	var buf bytes.Buffer
	require.NoError(t, stickler.Format(&buf, stickler.OutputSARIF, resultWith(diags, nil)))

	var log struct {
		Schema  string `json:"$schema"`
		Version string `json:"version"`
		Runs    []struct {
			Tool struct {
				Driver struct {
					Name string `json:"name"`
				} `json:"driver"`
			} `json:"tool"`
			Results []struct {
				RuleID  string `json:"ruleId"`
				Level   string `json:"level"`
				Message struct {
					Text string `json:"text"`
				} `json:"message"`
				Locations []struct {
					PhysicalLocation struct {
						ArtifactLocation struct {
							URI string `json:"uri"`
						} `json:"artifactLocation"`
						Region struct {
							StartLine   int `json:"startLine"`
							StartColumn int `json:"startColumn"`
						} `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &log))
	assert.Equal(t, "2.1.0", log.Version)
	assert.Contains(t, log.Schema, "sarif-2.1.0")
	require.Len(t, log.Runs, 1)
	assert.Equal(t, "stickler", log.Runs[0].Tool.Driver.Name)
	results := log.Runs[0].Results
	require.Len(t, results, 3)
	assert.Equal(t, "yze/gotostmt", results[0].RuleID)
	assert.Equal(t, "error", results[0].Level)
	assert.Equal(t, "goto", results[0].Message.Text)
	loc := results[0].Locations[0].PhysicalLocation
	assert.Equal(t, "a.go", loc.ArtifactLocation.URI)
	assert.Equal(t, 3, loc.Region.StartLine)
	assert.Equal(t, 2, loc.Region.StartColumn)
	assert.Equal(t, "warning", results[1].Level)
	assert.Equal(t, "note", results[2].Level)
}

func TestFormatSARIFSurfacesWriteError(t *testing.T) {
	res := resultWith([]goyze.Diagnostic{{Rule: "yze/x", Path: "a.go", Message: "m"}}, nil)
	require.Error(t, stickler.Format(failWriter{}, stickler.OutputSARIF, res))
}
