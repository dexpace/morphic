// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/report"
	"github.com/dexpace/morphic/internal/testspec"
)

// twoErrorSpec draws two error findings, a contact address that is no address
// and a path parameter that is not required, and lowers to a document.
const twoErrorSpec = `openapi: 3.1.0
info:
  title: T
  version: "1"
  contact: {email: not-an-email}
paths:
  /pets/{id}:
    get:
      operationId: getPet
      parameters:
        - {name: id, in: path, required: false, schema: {type: string}}
      responses: {"200": {description: ok}}
`

// The codes twoErrorSpec draws, and where the first is.
const (
	formatCode   = "openapi/validation/validation-invalid-format"
	valuesCode   = "openapi/validation/validation-allowed-values"
	emailPointer = "/info/contact/email"
)

// triageFile writes a triage file holding text and returns its path.
func triageFile(t *testing.T, text string) string {
	t.Helper()
	return writeFile(t, "triage.json", text)
}

// TestRun_TriageRatesTheFindingsTheVerdictReads pins --triage on both
// commands: with no file the spec's errors exit 1; a rule accepting one at its
// place and another rating the other a warning exit 0, the warning written at
// its rated severity, the accepted one counted, and the rule no finding met
// named.
func TestRun_TriageRatesTheFindingsTheVerdictReads(t *testing.T) {
	t.Parallel()
	rules := triageFile(t, `{"rules": [
		{"code": "`+formatCode+`", "pointer": "`+emailPointer+`", "accept": true, "reason": "the vendor's address"},
		{"code": "`+valuesCode+`", "severity": "warning", "reason": "lowered as required"},
		{"code": "openapi/unmet", "severity": "info", "reason": "stale"}
	]}`)
	for _, command := range []string{"compile", "validate"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			spec := writeFile(t, "spec.yaml", twoErrorSpec)
			code, _, stderr := runCommand(t, command, spec)
			require.Equal(t, exitFindings, code, stderr)

			code, stdout, stderr := runCommand(t, command, spec, "--triage", rules)
			require.Equal(t, 0, code, stderr)
			assert.Equal(t, command == "compile", stdout != "", "compile writes its document")
			lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
			require.Len(t, lines, 3, stderr)
			assert.True(t, strings.HasPrefix(lines[0], "warning "+valuesCode+" "), lines[0])
			assert.Equal(t, "  triage rule for openapi/unmet matched no finding", lines[1])
			assert.Equal(t, "  1 of 2 findings shown: 0 error, 1 warning, 0 info, 1 accepted", lines[2])
		})
	}
}

// TestRun_TriageThatCannotBeReadIsAFailure pins a triage file that cannot be
// read, or holds what no run can apply, as exit 2 before anything compiles:
// no finding is written, and no document.
func TestRun_TriageThatCannotBeReadIsAFailure(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, want string
		path       func(t *testing.T) string
	}{
		{"a missing file", "read triage file", func(t *testing.T) string {
			return filepath.Join(t.TempDir(), "absent.json")
		}},
		{"a misspelt member", "severty", func(t *testing.T) string {
			return triageFile(t, `{"rules": [{"code": "c", "severty": "info", "reason": "r"}]}`)
		}},
		{"an acceptance naming no place", "accepts without naming a place", func(t *testing.T) string {
			return triageFile(t, `{"rules": [{"code": "c", "accept": true, "reason": "r"}]}`)
		}},
	} {
		for _, command := range []string{"compile", "validate"} {
			t.Run(command+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				path := tc.path(t)
				code, stdout, stderr := runCommand(t, command, writeFile(t, "spec.yaml", twoErrorSpec), "--triage", path)
				assert.Equal(t, exitFailure, code)
				assert.Empty(t, stdout)
				assert.True(t, strings.HasPrefix(stderr, "morphic: engine: "), stderr)
				assert.Contains(t, stderr, tc.want)
				assert.Contains(t, stderr, path)
				assert.Equal(t, 1, strings.Count(stderr, "\n"), "one line, and no finding: %s", stderr)
			})
		}
	}
}

// decodeReport decodes stderr as the JSON report, failing the test unless it
// is one object and nothing else.
func decodeReport(t *testing.T, stderr string) (int, report.Report) {
	t.Helper()
	var got struct {
		Version int `json:"version"`
		report.Report
	}
	require.NoError(t, json.Unmarshal([]byte(stderr), &got, json.RejectUnknownMembers(true)), "stderr: %s", stderr)
	return got.Version, got.Report
}

// TestRun_DiagFormatJSONWritesOneReport pins --diag-format json on both
// commands: stderr holds one JSON report, every finding in it as rated with the
// rule it met, the verdict beside them, and the run's exit status and stdout
// are what text gives.
func TestRun_DiagFormatJSONWritesOneReport(t *testing.T) {
	t.Parallel()
	rules := triageFile(t, `{"rules": [
		{"code": "`+valuesCode+`", "severity": "warning", "reason": "lowered as required"}
	]}`)
	for _, command := range []string{"compile", "validate"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			spec := writeFile(t, "spec.yaml", twoErrorSpec)
			textCode, textOut, _ := runCommand(t, command, spec, "--triage", rules)

			code, stdout, stderr := runCommand(t, command, spec, "--triage", rules, "--diag-format", "json")
			assert.Equal(t, textCode, code)
			assert.Equal(t, textOut, stdout)
			version, got := decodeReport(t, stderr)
			assert.Equal(t, report.SchemaVersion, version)
			assert.True(t, got.Failed)
			require.Len(t, got.Findings, 2)
			assert.Equal(t, formatCode, got.Findings[0].Diagnostic.Code)
			assert.EqualValues(t, "error", got.Findings[0].Severity)
			assert.Nil(t, got.Findings[0].Rule)
			assert.EqualValues(t, "error", got.Findings[1].Diagnostic.Severity)
			assert.EqualValues(t, "warning", got.Findings[1].Severity)
			require.NotNil(t, got.Findings[1].Rule)
			assert.Equal(t, "lowered as required", got.Findings[1].Rule.Reason)
			require.Len(t, got.Sources, 1)
			assert.Equal(t, spec, got.Sources[0].Path)
			assert.Empty(t, got.UnusedRules)
			assert.Empty(t, got.Warnings)
		})
	}
}

// TestRun_DiagFormatJSONWritesAReportForEveryRun pins a report for a run with
// no finding, and for one whose spec lowered to no document, so a consumer
// always has one to read.
func TestRun_DiagFormatJSONWritesAReportForEveryRun(t *testing.T) {
	t.Parallel()
	code, _, stderr := runCommand(t, "validate", writeFile(t, "spec.yaml", testspec.Tiny), "--diag-format", "json")
	require.Equal(t, 0, code, stderr)
	_, clean := decodeReport(t, stderr)
	assert.False(t, clean.Failed)
	assert.Empty(t, clean.Findings)

	code, _, stderr = runCommand(t, "compile", writeFile(t, "spec.yaml", "hello: world\n"), "--diag-format", "json")
	require.Equal(t, exitFindings, code, stderr)
	_, refused := decodeReport(t, stderr)
	assert.True(t, refused.Failed)
	require.Len(t, refused.Findings, 1)
	assert.Equal(t, "engine/unrecognized-format", refused.Findings[0].Diagnostic.Code)
}

// TestRun_DiagFormatJSONCarriesTheCacheWarnings pins the warnings a cache
// gives, before the run and after it, as members of the report rather than
// lines beside it, so stderr stays one JSON object.
func TestRun_DiagFormatJSONCarriesTheCacheWarnings(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocked, nil, 0o600))
	spec := specFile(t)

	code, _, stderr := runCommand(t, "compile", spec, "--cache-dir", blocked, "--diag-format", "json")
	require.Equal(t, 0, code, stderr)
	_, after := decodeReport(t, stderr)
	require.Len(t, after.Warnings, 1)
	assert.Regexp(t, `^engine: cache: load: [^\n]+; engine: cache: store: [^\n]+$`, after.Warnings[0])

	failBuildID(t, os.ErrPermission)
	code, _, stderr = runCommand(t, "validate", spec, "--cache-dir", t.TempDir(), "--diag-format", "json")
	require.Equal(t, 0, code, stderr)
	_, before := decodeReport(t, stderr)
	assert.Equal(t, []string{os.ErrPermission.Error()}, before.Warnings)
}

// TestRun_DiagFormatJSONRefusesTheTextOptions pins --show and --diag-limit as
// misuses beside --diag-format json, which writes every finding: accepted,
// either would say a report was narrowed that was not.
func TestRun_DiagFormatJSONRefusesTheTextOptions(t *testing.T) {
	t.Parallel()
	for _, option := range [][]string{{"--show", "error"}, {"--diag-limit", "1"}} {
		for _, command := range []string{"compile", "validate"} {
			t.Run(command+"/"+option[0], func(t *testing.T) {
				t.Parallel()
				args := append([]string{command, specFile(t), "--diag-format", "json"}, option...)
				code, stdout, stderr := runCommand(t, args...)
				assert.Equal(t, exitFailure, code)
				assert.Empty(t, stdout)
				assert.Equal(t, "morphic: --diag-format json writes every finding, so "+option[0]+
					" has nothing to withhold\nTry \"morphic "+command+" --help\" for usage.\n", stderr)
			})
		}
	}
}

// manyFindingsSpec draws five info findings of one code, a validation-only
// keyword kept on each of five schemas, and one warning, a reserved header.
const manyFindingsSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: getX
      parameters:
        - {name: Accept, in: header, schema: {type: string}}
      responses: {"200": {description: ok}}
components:
  schemas:
    A: {type: object, not: {required: [x]}}
    B: {type: object, not: {required: [x]}}
    C: {type: object, not: {required: [x]}}
    D: {type: object, not: {required: [x]}}
    E: {type: object, not: {required: [x]}}
`

// TestRun_TextWithholdsWhatItsOptionsSay pins the text format's defaults and
// both options on a spec drawing more findings of one code than the default
// writes: three of them and a count of the rest, then every one under
// --diag-limit 0, then none below --show warning.
func TestRun_TextWithholdsWhatItsOptionsSay(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", manyFindingsSpec)
	const code = "openapi/validation-only-keyword"
	for _, tc := range []struct {
		name   string
		args   []string
		lines  int
		footer []string
	}{
		{"the defaults", nil, 4, []string{"  2 more info " + code + " not shown",
			"  4 of 6 findings shown: 0 error, 1 warning, 5 info, 0 accepted"}},
		{"every finding", []string{"--diag-limit", "0"}, 6, []string{}},
		{"one of a code", []string{"--diag-limit", "1"}, 2, []string{"  4 more info " + code + " not shown",
			"  2 of 6 findings shown: 0 error, 1 warning, 5 info, 0 accepted"}},
		{"warnings and errors", []string{"--show", "warning"}, 1, []string{"  5 info " + code + " not shown",
			"  1 of 6 findings shown: 0 error, 1 warning, 5 info, 0 accepted"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			exit, _, stderr := runCommand(t, append([]string{"validate", spec}, tc.args...)...)
			require.Equal(t, 0, exit, stderr)
			lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
			require.Len(t, lines, tc.lines+len(tc.footer), stderr)
			for _, line := range lines[:tc.lines] {
				assert.Regexp(t, `^(info|warning) openapi/[a-z-]+ `, line)
			}
			assert.Equal(t, tc.footer, lines[tc.lines:])
		})
	}
}

// escapeSpec names a schema with a key holding ESC and a newline, which the
// key's validation finding places at a pointer naming it.
const escapeSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    "S\e[31m\nerror openapi-forged spec.yaml: forged": {type: object, not: {required: [x]}}
`

// TestRun_FindingsEscapeWhatTheSpecWrites holds both commands' text, and
// --explain's report, to writing no control character a spec wrote: the key's
// ESC and newline are spelled as escapes, so no line is forged and no
// terminal sequence reaches the stream.
func TestRun_FindingsEscapeWhatTheSpecWrites(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", escapeSpec)
	noControl := func(t *testing.T, stream string) {
		t.Helper()
		for i, line := range strings.Split(strings.TrimSuffix(stream, "\n"), "\n") {
			assert.Equal(t, -1, strings.IndexFunc(line, func(r rune) bool { return r < 0x20 || r == 0x7f }), "line %d: %q", i, line)
			assert.NotContains(t, line, "\x1b")
		}
	}
	for _, command := range []string{"compile", "validate"} {
		_, _, stderr := runCommand(t, command, spec)
		require.Contains(t, stderr, `S\x1b[31m\nerror openapi-forged spec.yaml: forged`, "the key is written, escaped")
		noControl(t, stderr)
		assert.NotContains(t, "\n"+stderr, "\nerror openapi-forged", "no line is forged")
	}
	_, stdout, stderr := runCommand(t, "compile", spec, "--explain", "")
	noControl(t, stderr)
	noControl(t, stdout)
	assert.Contains(t, stdout, `S\x1b[31m\nerror openapi-forged`)
}

// TestRun_DiagFormatJSONWriteFailureEndsTheRun pins a stderr refusing the JSON
// report as the text format's refusal is: the verdict's code, or 2 for a run
// that earned 0, the reason after it, and no document. A report is written for
// every run, so even one with no finding asks the stream.
func TestRun_DiagFormatJSONWriteFailureEndsTheRun(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, spec string
		want       int
	}{
		{"a finding at the threshold", errSpec, exitFindings},
		{"no findings", testspec.Tiny, exitFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stdout bytes.Buffer
			stderr := &firstWriteFails{}
			code := run(t.Context(), []string{"compile", writeFile(t, "spec.yaml", tc.spec), "--diag-format", "json"},
				&stdout, stderr)
			assert.Equal(t, tc.want, code)
			assert.Equal(t, "morphic: write diagnostics: pipe closed\n", stderr.text.String())
			assert.Empty(t, stdout.String())
		})
	}
}
