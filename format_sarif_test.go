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

func TestFormatSARIFOmitsFixesWhenAbsent(t *testing.T) {
	var buf bytes.Buffer
	diags := []goyze.Diagnostic{
		{Rule: "yze/gotostmt", Path: "a.go", Line: 3, Col: 2, Severity: goyze.SeverityError, Message: "goto"},
	}

	require.NoError(t, stickler.Format(&buf, stickler.OutputSARIF, resultWith(diags, nil)))

	assert.NotContains(t, buf.String(), `"fixes"`)
}

func TestFormatSARIFEmitsFixes(t *testing.T) {
	diags := []goyze.Diagnostic{{
		Rule:     "yze/gotostmt",
		Path:     "a.go",
		Line:     3,
		Col:      2,
		Severity: goyze.SeverityError,
		Message:  "goto",
		Fixes: []goyze.Fix{{
			Description: "replace goto with loop",
			Files: []goyze.FileEdit{{
				Path:  "a.go",
				Edits: []goyze.TextEdit{{NewText: "for {", Start: 10, End: 14}},
			}},
		}},
	}}
	var buf bytes.Buffer

	require.NoError(t, stickler.Format(&buf, stickler.OutputSARIF, resultWith(diags, nil)))

	assert.JSONEq(t, `{
		"$schema": "https://json.schemastore.org/sarif-2.1.0.json",
		"version": "2.1.0",
		"runs": [{
			"tool": {"driver": {"name": "stickler"}},
			"results": [{
				"ruleId": "yze/gotostmt",
				"level": "error",
				"message": {"text": "goto"},
				"locations": [{"physicalLocation": {
					"artifactLocation": {"uri": "a.go"},
					"region": {"startLine": 3, "startColumn": 2}
				}}],
				"fixes": [{
					"description": {"text": "replace goto with loop"},
					"artifactChanges": [{
						"artifactLocation": {"uri": "a.go"},
						"replacements": [{
							"deletedRegion": {"byteOffset": 10, "byteLength": 4},
							"insertedContent": {"text": "for {"}
						}]
					}]
				}]
			}]
		}]
	}`, buf.String())
}

func TestFormatSARIFOmitsInsertedContentForPureDeletion(t *testing.T) {
	diags := []goyze.Diagnostic{{
		Rule:     "yze/gotostmt",
		Path:     "a.go",
		Severity: goyze.SeverityError,
		Message:  "goto",
		Fixes: []goyze.Fix{{
			Description: "delete the goto",
			Files:       []goyze.FileEdit{{Path: "a.go", Edits: []goyze.TextEdit{{NewText: "", Start: 5, End: 9}}}},
		}},
	}}
	var buf bytes.Buffer

	require.NoError(t, stickler.Format(&buf, stickler.OutputSARIF, resultWith(diags, nil)))

	var log struct {
		Runs []struct {
			Results []struct {
				Fixes []struct {
					ArtifactChanges []struct {
						Replacements []struct {
							InsertedContent *struct {
								Text string `json:"text"`
							} `json:"insertedContent"`
							DeletedRegion struct {
								ByteOffset int `json:"byteOffset"`
								ByteLength int `json:"byteLength"`
							} `json:"deletedRegion"`
						} `json:"replacements"`
					} `json:"artifactChanges"`
				} `json:"fixes"`
			} `json:"results"`
		} `json:"runs"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &log))
	replacements := log.Runs[0].Results[0].Fixes[0].ArtifactChanges[0].Replacements
	require.Len(t, replacements, 1)
	assert.Equal(t, 5, replacements[0].DeletedRegion.ByteOffset)
	assert.Equal(t, 4, replacements[0].DeletedRegion.ByteLength)
	assert.Nil(t, replacements[0].InsertedContent)
	assert.NotContains(t, buf.String(), `"insertedContent"`)
}

func TestFormatSARIFSurfacesWriteError(t *testing.T) {
	res := resultWith([]goyze.Diagnostic{{Rule: "yze/x", Path: "a.go", Message: "m"}}, nil)
	require.Error(t, stickler.Format(failWriter{}, stickler.OutputSARIF, res))
}
