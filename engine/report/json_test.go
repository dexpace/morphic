// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package report_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/report"
	"github.com/dexpace/morphic/engine/triage"
	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/internal/jsonshape"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// schema is one published shape of the JSON report: a SchemaVersion and the
// hex SHA-256 of its description, schemaGolden.
type schema struct {
	version int
	shape   string
}

// schemas is every shape the report has been published with, oldest first. A
// change to the shape appends a row for the next SchemaVersion; a row is never
// edited.
var schemas = []schema{
	{version: 1, shape: "1de2889715ac89ffec00b148cadd3ec6970c254c3d846018328ce5a50166903a"},
}

// schemaGolden holds the description of the newest row's shape, so a change
// reviews as a diff of it.
const schemaGolden = "testdata/schema.golden.txt"

// TestSchemaVersion_PinsTheEncodedShape holds SchemaVersion to the shape of
// what WriteJSON encodes, every type it reaches described, a finding's
// diagnostic among them: a shape change at an unmoved version fails here, and
// so does a version with no row. -update rewrites schemaGolden, never the
// table, so it cannot settle either.
func TestSchemaVersion_PinsTheEncodedShape(t *testing.T) {
	t.Parallel()
	p, err := decls.Load("github.com/dexpace/morphic/engine/report")
	require.NoError(t, err)
	desc, err := jsonshape.Describe(p, "versioned")
	require.NoError(t, err)
	if irtest.Update() {
		require.NoError(t, os.WriteFile(schemaGolden, []byte(desc), 0o644))
	}
	want, err := os.ReadFile(schemaGolden)
	require.NoError(t, err, "go test ./engine/report -run TestSchemaVersion -update writes it")
	assert.Empty(t, cmp.Diff(string(want), desc),
		"%s is stale: go test ./engine/report -run TestSchemaVersion -update rewrites it, then review its diff", schemaGolden)

	sum := sha256.Sum256([]byte(desc))
	last := schemas[len(schemas)-1]
	require.Equal(t, report.SchemaVersion, last.version,
		"SchemaVersion has no row: append {version: %d, shape: %q}", report.SchemaVersion, hex.EncodeToString(sum[:]))
	assert.Equal(t, last.shape, hex.EncodeToString(sum[:]),
		"the report's shape changed at SchemaVersion %d, published with another: move it to %d and append its row",
		last.version, last.version+1)
}

// TestSchemas_AppendInOrder holds the table to its form: versions counting up
// by one from 1, each a shape its predecessor did not have.
func TestSchemas_AppendInOrder(t *testing.T) {
	t.Parallel()
	for i, row := range schemas {
		assert.Equal(t, i+1, row.version, "row %d", i)
		if i > 0 {
			assert.NotEqual(t, schemas[i-1].shape, row.shape, "version %d moved with no change of shape", row.version)
		}
	}
}

// sampleReport is a report holding a member of every kind: a source, a finding
// a rule rated, one a rule accepted, one no rule met, an unused rule and a
// warning.
func sampleReport(t *testing.T) report.Report {
	t.Helper()
	rules := []triage.Rule{
		{Code: "openapi/x", Severity: ir.SeverityWarning, Reason: "rated"},
		{Code: "openapi/y", Pointer: "/a", Accept: true, Reason: "accepted"},
		{Code: "openapi/z", Node: "s/x", Severity: ir.SeverityInfo, Reason: "unused"},
	}
	s, err := triage.NewRuleset(rules)
	require.NoError(t, err)
	findings, unused := s.Rate([]ir.Diagnostic{
		{Severity: ir.SeverityError, Code: "openapi/x", Message: "esc \x1b[31m", Provenance: ir.Provenance{Pointer: "/b"}},
		{Severity: ir.SeverityInfo, Code: "openapi/y", Message: "m", Provenance: ir.Provenance{Pointer: "/a"}},
		{Severity: ir.SeverityInfo, Code: "pass/w", Message: "m", Provenance: ir.Provenance{Source: ir.NoSource, Node: "g/x"}},
	})
	return report.Report{
		Failed:      true,
		Sources:     []ir.SourceInfo{{Format: "openapi@3.1", Path: "spec.yaml", Hash: "h"}},
		Findings:    findings,
		UnusedRules: unused,
		Warnings:    []string{"engine: cache: store: disk full"},
	}
}

// TestWriteJSON_WritesTheReportAsData decodes what WriteJSON wrote back into
// the report, so each member is what a program reads, and pins the version and
// the text of a rated finding as literal JSON a consumer's query names.
func TestWriteJSON_WritesTheReportAsData(t *testing.T) {
	t.Parallel()
	r := sampleReport(t)
	var w strings.Builder
	require.NoError(t, report.WriteJSON(&w, r))

	out := w.String()
	require.True(t, strings.HasSuffix(out, "}\n"), "one object, a newline after it")
	assert.Equal(t, 1, strings.Count(out, "\n"), "one line")
	var back struct {
		Version int `json:"version"`
		report.Report
	}
	require.NoError(t, json.Unmarshal([]byte(out), &back, json.RejectUnknownMembers(true)))
	assert.Equal(t, report.SchemaVersion, back.Version)
	assert.Empty(t, cmp.Diff(r, back.Report))

	assert.Contains(t, out, `{"diagnostic":{"severity":"error","code":"openapi/x","message":"esc \u001b[31m",`+
		`"provenance":{"source":0,"pointer":"/b"}},"severity":"warning","rule":{"code":"openapi/x","severity":"warning","reason":"rated"}}`)
	assert.Contains(t, out, `"accepted":true`)
	assert.True(t, strings.HasPrefix(out, `{"version":1,"failed":true,`), "%s", out)
}

// TestWriteJSON_WritesEveryListWhenEmpty pins an empty report's lists as empty
// arrays, never null or absent, so a query iterating one needs no guard.
func TestWriteJSON_WritesEveryListWhenEmpty(t *testing.T) {
	t.Parallel()
	var w strings.Builder
	require.NoError(t, report.WriteJSON(&w, report.Report{}))
	assert.Equal(t, `{"version":1,"failed":false,"sources":[],"findings":[],"unusedRules":[],"warnings":[]}`+"\n", w.String())
}

// TestWriteJSON_RefusesWhatJSONCannotCarry pins a string that is not UTF-8 as
// refused before anything is written, so a reader never meets half a report.
func TestWriteJSON_RefusesWhatJSONCannotCarry(t *testing.T) {
	t.Parallel()
	r := report.Report{Findings: rated([]ir.Diagnostic{diag("c", ir.Provenance{Pointer: "/\xff"})})}
	w := &failingWriter{failAt: 1}
	err := report.WriteJSON(w, r)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "report: encode:")
	assert.Zero(t, w.attempts)
}

// TestWriteJSON_ReturnsTheWritersError pins a destination refusing the report
// as the error WriteJSON returns.
func TestWriteJSON_ReturnsTheWritersError(t *testing.T) {
	t.Parallel()
	w := &failingWriter{failAt: 1}
	require.ErrorIs(t, report.WriteJSON(w, sampleReport(t)), errDisk)
	assert.Equal(t, 1, w.attempts, "the report is one write")
}
