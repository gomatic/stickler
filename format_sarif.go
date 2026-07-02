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
	Fixes     []sarifFix      `json:"fixes,omitempty"`
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

// sarifFix is a SARIF fix: a suggested change viewers and code scanning can apply.
type sarifFix struct {
	Description     sarifMessage          `json:"description"`
	ArtifactChanges []sarifArtifactChange `json:"artifactChanges"`
}

type sarifArtifactChange struct {
	ArtifactLocation sarifArtifact      `json:"artifactLocation"`
	Replacements     []sarifReplacement `json:"replacements"`
}

// sarifReplacement deletes deletedRegion and, unless the edit is a pure deletion,
// inserts insertedContent in its place.
type sarifReplacement struct {
	InsertedContent *sarifContent   `json:"insertedContent,omitempty"`
	DeletedRegion   sarifByteRegion `json:"deletedRegion"`
}

// sarifByteRegion addresses a region by byte extent. Both properties are always
// emitted: byteOffset 0 is a valid start and byteLength 0 marks a pure insertion.
type sarifByteRegion struct {
	ByteOffset int `json:"byteOffset"`
	ByteLength int `json:"byteLength"`
}

type sarifContent struct {
	Text string `json:"text"`
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
		Fixes: sarifFixesOf(d.Fixes),
	}
}

// sarifFixesOf maps a diagnostic's fixes onto SARIF fixes, or nil when there are
// none so the fixes key is omitted entirely.
func sarifFixesOf(fixes []goyze.Fix) []sarifFix {
	if len(fixes) == 0 {
		return nil
	}
	out := make([]sarifFix, 0, len(fixes))
	for _, f := range fixes {
		out = append(out, sarifFix{
			Description:     sarifMessage{Text: f.Description},
			ArtifactChanges: sarifChangesOf(f.Files),
		})
	}
	return out
}

// sarifChangesOf maps one fix's per-file edit groups onto SARIF artifactChanges.
func sarifChangesOf(files []goyze.FileEdit) []sarifArtifactChange {
	out := make([]sarifArtifactChange, 0, len(files))
	for _, file := range files {
		out = append(out, sarifArtifactChange{
			ArtifactLocation: sarifArtifact{URI: file.Path},
			Replacements:     sarifReplacementsOf(file.Edits),
		})
	}
	return out
}

// sarifReplacementsOf maps byte-offset TextEdits onto SARIF replacements: the
// deleted region is [Start, End) and NewText, when present, is the insertion.
func sarifReplacementsOf(edits []goyze.TextEdit) []sarifReplacement {
	out := make([]sarifReplacement, 0, len(edits))
	for _, e := range edits {
		out = append(out, sarifReplacement{
			DeletedRegion:   sarifByteRegion{ByteOffset: e.Start, ByteLength: e.End - e.Start},
			InsertedContent: sarifContentOf(textParam(e.NewText)),
		})
	}
	return out
}

// textParam names the text parameter of sarifContentOf; rename it to the real domain concept.
type textParam string

// sarifContentOf wraps inserted text, or nil for a pure deletion so the
// insertedContent key is omitted.
func sarifContentOf(text textParam) *sarifContent {
	if string(text) == "" {
		return nil
	}
	return &sarifContent{Text: string(text)}
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
