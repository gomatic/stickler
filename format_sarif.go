package stickler

import (
	"encoding/json"
	"io"

	goyze "github.com/gomatic/go-yze"
)

// sarifSchemaURL is the SARIF 2.1.0 JSON-schema URL stamped into the log.
const sarifSchemaURL = "https://json.schemastore.org/sarif-2.1.0.json"

// sarifLog is the minimal SARIF 2.1.0 envelope carrying one run of results.
type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name string `json:"name"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId,omitempty"`
	Level     string          `json:"level"`
	Message   sarifMessage    `json:"message"`
	Locations []sarifLocation `json:"locations,omitempty"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysical `json:"physicalLocation"`
}

type sarifPhysical struct {
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
	Region           sarifRegion   `json:"region"`
}

type sarifArtifact struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine,omitempty"`
	StartColumn int `json:"startColumn,omitempty"`
}

// formatSARIF writes the result as a SARIF 2.1.0 log whose single run carries one
// result per diagnostic, for GitHub code scanning and SARIF viewers. Runner errors
// are not SARIF results; they still gate the run via Result.Failed.
func formatSARIF(w io.Writer, result Result) error {
	results := make([]sarifResult, 0, len(result.Diagnostics))
	for _, d := range result.Diagnostics {
		results = append(results, sarifResultOf(d))
	}
	log := sarifLog{
		Schema:  sarifSchemaURL,
		Version: "2.1.0",
		Runs:    []sarifRun{{Tool: sarifTool{Driver: sarifDriver{Name: "stickler"}}, Results: results}},
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(log)
}

// sarifResultOf maps one diagnostic onto a SARIF result with a physical location.
func sarifResultOf(d goyze.Diagnostic) sarifResult {
	return sarifResult{
		RuleID:  d.Rule,
		Level:   sarifLevel(d.Severity),
		Message: sarifMessage{Text: d.Message},
		Locations: []sarifLocation{{PhysicalLocation: sarifPhysical{
			ArtifactLocation: sarifArtifact{URI: d.Path},
			Region:           sarifRegion{StartLine: d.Line, StartColumn: d.Col},
		}}},
	}
}

// sarifLevel maps a normalized severity to a SARIF result level.
func sarifLevel(severity goyze.Severity) string {
	switch severity {
	case goyze.SeverityWarning:
		return levelWarning
	case goyze.SeverityInfo:
		return "note"
	default:
		return "error"
	}
}
