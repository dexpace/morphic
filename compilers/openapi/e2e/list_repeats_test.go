// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

const (
	duplicateParameter  = "openapi/duplicate-parameter"
	duplicateServerName = "openapi/duplicate-server-name"
)

// sharedItem is a path item whose parameters and operation's parameters each
// repeat an entry, written at at.
func sharedItem(indent string) string {
	lines := []string{
		"parameters:",
		"  - {name: a, in: query, schema: {type: string}}",
		"  - {name: a, in: query, schema: {type: string}}",
		"get:",
		"  parameters:",
		"    - {name: b, in: query, schema: {type: string}}",
		"    - {name: b, in: query, schema: {type: string}}",
		"  responses: {\"200\": {description: ok}}",
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(indent + l + "\n")
	}
	return b.String()
}

// TestCompile_AParameterListRepeatingAnEntryIsReportedWhereverItIsWritten holds a
// path item a $ref reaches outside paths to the findings the same lists draw
// under paths: each repeat once, at the entry, with the text it has there.
func TestCompile_AParameterListRepeatingAnEntryIsReportedWhereverItIsWritten(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n"
	_, direct := compileText(t, "direct.yaml", head+"paths:\n  /x:\n"+sharedItem("    "), openapi.Options{})
	_, shared := compileText(t, "shared.yaml", head+"x-shared:\n"+sharedItem("  ")+"paths:\n  /x: {$ref: \"#/x-shared\"}\n", openapi.Options{})

	want := map[string]string{}
	for _, d := range byCode(direct, duplicateParameter) {
		want[strings.Replace(string(d.Provenance.Pointer), "/paths/~1x", "/x-shared", 1)] =
			strings.ReplaceAll(d.Message, "/paths/~1x", "/x-shared")
	}
	got := map[string]string{}
	for _, d := range byCode(shared, duplicateParameter) {
		assert.Equal(t, ir.SeverityError, d.Severity)
		_, dup := got[string(d.Provenance.Pointer)]
		assert.False(t, dup, "each repeat is reported once: %+v", shared)
		got[string(d.Provenance.Pointer)] = d.Message
	}

	require.Len(t, want, 2)
	assert.Equal(t, want, got)
	assert.Contains(t, got, "/x-shared/parameters/1")
	assert.Contains(t, got, "/x-shared/get/parameters/1")
}

// TestCompile_ARepeatInAListTwoReferencesReachIsReportedOnce holds a list
// reached by two path items to one finding for each repeat, not one for each
// reference to it.
func TestCompile_ARepeatInAListTwoReferencesReachIsReportedOnce(t *testing.T) {
	t.Parallel()
	spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\nx-shared:\n" + sharedItem("  ") +
		"paths:\n  /x: {$ref: \"#/x-shared\"}\n  /y: {$ref: \"#/x-shared\"}\n"

	_, diags := compileText(t, "spec.yaml", spec, openapi.Options{})

	assert.ElementsMatch(t, []string{"/x-shared/parameters/1", "/x-shared/get/parameters/1"},
		codedPointers(diags, duplicateParameter), "%+v", diags)
}

// TestCompile_AListInAnotherDocumentIsCheckedWhereItIsWritten holds the lists of
// a path item another document holds, and a servers list there, to one finding
// for each repeat, in that document's own entry, at the entry that repeats.
func TestCompile_AListInAnotherDocumentIsCheckedWhereItIsWritten(t *testing.T) {
	t.Parallel()
	const other = `item:
  parameters:
    - {name: a, in: query, schema: {type: string}}
    - {name: a, in: query, schema: {type: string}}
  servers:
    - {url: 'https://a.example', name: s}
    - {url: 'https://b.example', name: s}
  get:
    parameters:
      - {name: b, in: query, schema: {type: string}}
      - {name: b, in: query, schema: {type: string}}
    responses: {"200": {description: ok}}
`
	const root = "openapi: 3.2.0\ninfo: {title: t, version: \"1\"}\npaths:\n  /x: {$ref: \"other.yaml#/item\"}\n"

	diags := compileBesideExpectingErrors(t, other, root)

	got := map[string][]int{}
	for _, d := range diags {
		switch d.Code {
		case duplicateParameter, duplicateServerName:
			got[d.Code+" "+string(d.Provenance.Pointer)] = append(got[d.Code+" "+string(d.Provenance.Pointer)], d.Provenance.Source)
		}
	}
	require.Len(t, got, 3, "%+v", diags)
	for key, sources := range got {
		require.Len(t, sources, 1, "%s once: %+v", key, diags)
		assert.NotZero(t, sources[0], "%s is in the other document's entry", key)
	}
	assert.Contains(t, got, duplicateParameter+" /item/parameters/1")
	assert.Contains(t, got, duplicateParameter+" /item/get/parameters/1")
	// A servers list's repeat is placed at the name it repeats, as the
	// source's own are.
	assert.Contains(t, got, duplicateServerName+" /item/servers/1/name")
}
