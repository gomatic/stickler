package stickler_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	errs "github.com/gomatic/go-error"
	goyze "github.com/gomatic/go-yze"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gomatic/stickler"
)

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errs.Const("io fail") }

func resultWith(diags []goyze.Diagnostic, errs []error) stickler.Result {
	return stickler.Result{Diagnostics: diags, Errors: errs}
}

func TestFormatHumanWritesDiagnosticsAndErrors(t *testing.T) {
	var buf bytes.Buffer
	res := resultWith(
		[]goyze.Diagnostic{{Path: "a.go", Line: 3, Col: 2, Severity: goyze.SeverityError, Message: "boom", Rule: "yze/gotostmt"}},
		[]error{errors.New("tool x failed")},
	)

	require.NoError(t, stickler.Format(&buf, stickler.OutputHuman, res))

	assert.Contains(t, buf.String(), "a.go:3:2: boom [error] (yze/gotostmt)")
	assert.Contains(t, buf.String(), "runner error: tool x failed")
}

func TestFormatHumanReportsDiagnosticWriteError(t *testing.T) {
	res := resultWith([]goyze.Diagnostic{{Path: "a.go"}}, nil)
	require.Error(t, stickler.Format(failWriter{}, stickler.OutputHuman, res))
}

func TestFormatHumanReportsErrorLineWriteError(t *testing.T) {
	res := resultWith(nil, []error{errors.New("boom")})
	require.Error(t, stickler.Format(failWriter{}, stickler.OutputHuman, res))
}

func TestFormatJSONEncodesDiagnosticsAndErrors(t *testing.T) {
	var buf bytes.Buffer
	res := resultWith(
		[]goyze.Diagnostic{{Rule: "r", Path: "a.go"}},
		[]error{errors.New("oops")},
	)

	require.NoError(t, stickler.Format(&buf, stickler.OutputJSON, res))

	assert.Contains(t, buf.String(), `"diagnostics"`)
	assert.Contains(t, buf.String(), `"oops"`)
}

func TestFormatJSONReportsWriteError(t *testing.T) {
	require.Error(t, stickler.Format(failWriter{}, stickler.OutputJSON, resultWith(nil, nil)))
}

func TestFormatGitHubEmitsAnnotationsByLevel(t *testing.T) {
	var buf bytes.Buffer
	res := resultWith([]goyze.Diagnostic{
		{Path: "a.go", Line: 1, Col: 1, Severity: goyze.SeverityError, Message: "e"},
		{Path: "b.go", Line: 2, Col: 2, Severity: goyze.SeverityWarning, Message: "w"},
		{Path: "c.go", Line: 3, Col: 3, Severity: goyze.SeverityInfo, Message: "i"},
	}, nil)

	require.NoError(t, stickler.Format(&buf, stickler.OutputGitHub, res))

	assert.Contains(t, buf.String(), "::error file=a.go,line=1,col=1::e")
	assert.Contains(t, buf.String(), "::warning file=b.go,line=2,col=2::w")
	assert.Contains(t, buf.String(), "::notice file=c.go,line=3,col=3::i")
}

func TestFormatGitHubEscapesDataAndPropertiesAndCarriesRule(t *testing.T) {
	var buf bytes.Buffer
	res := resultWith([]goyze.Diagnostic{{
		Path:     "a:b,c.go",
		Line:     5,
		Col:      7,
		Severity: goyze.SeverityError,
		Rule:     "yze/gotostmt",
		Message:  "100%\r\nsecond, line: x",
	}}, nil)

	require.NoError(t, stickler.Format(&buf, stickler.OutputGitHub, res))

	// Property values escape %,CR,LF plus comma and colon; the message escapes only
	// %,CR,LF (its commas and colon stay literal); % is escaped first so %0D/%0A are
	// not re-escaped; the rule rides in title=.
	assert.Equal(t,
		"::error title=yze/gotostmt,file=a%3Ab%2Cc.go,line=5,col=7::100%25%0D%0Asecond, line: x\n",
		buf.String(),
	)
}

func TestFormatGitHubOmitsTitleWhenRuleAbsent(t *testing.T) {
	var buf bytes.Buffer
	res := resultWith([]goyze.Diagnostic{{Path: "a.go", Line: 1, Col: 2, Severity: goyze.SeverityWarning, Message: "m"}}, nil)

	require.NoError(t, stickler.Format(&buf, stickler.OutputGitHub, res))

	assert.Equal(t, "::warning file=a.go,line=1,col=2::m\n", buf.String())
}

func TestFormatJSONRoundTripsIntoDiagnosticSchema(t *testing.T) {
	var buf bytes.Buffer
	want := goyze.Diagnostic{Tool: "yze", Rule: "yze/gotostmt", Path: "a.go", Line: 3, Col: 2, Severity: goyze.SeverityError, Message: "boom"}
	res := resultWith([]goyze.Diagnostic{want}, []error{errors.New("oops")})

	require.NoError(t, stickler.Format(&buf, stickler.OutputJSON, res))

	var decoded struct {
		Diagnostics []goyze.Diagnostic `json:"diagnostics"`
		Errors      []string           `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &decoded))
	require.Len(t, decoded.Diagnostics, 1)
	assert.Equal(t, want, decoded.Diagnostics[0])
	assert.Equal(t, []string{"oops"}, decoded.Errors)
}

func TestFormatGitHubReportsWriteError(t *testing.T) {
	res := resultWith([]goyze.Diagnostic{{Path: "a.go"}}, nil)
	require.Error(t, stickler.Format(failWriter{}, stickler.OutputGitHub, res))
}

func TestFormatRejectsUnknownFormat(t *testing.T) {
	err := stickler.Format(&bytes.Buffer{}, "nope", resultWith(nil, nil))
	require.Error(t, err)
	assert.True(t, errors.Is(err, stickler.ErrUnknownOutput))
}
