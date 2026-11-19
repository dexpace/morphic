// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// nonScalarKeys writes a key that is no scalar at a path map, a responses map
// and a components map: the three places a rule judging a key's spelling reads
// one.
const nonScalarKeys = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  ? [p]
  : {get: {responses: {"200": {description: ok}}}}
  /a:
    get:
      responses:
        ? {code: 1}
        : {description: weird}
        "200": {description: ok}
components:
  schemas:
    ? [s]
    : {type: object}
`

// TestCompile_AKeyThatIsNoScalarIsReportedOnceAndLeftOut holds a key that is
// no scalar to the Format error alone: it is not read as the key "", so no rule
// that judges a key's spelling reports it again, and its entry is left out.
func TestCompile_AKeyThatIsNoScalarIsReportedOnceAndLeftOut(t *testing.T) {
	t.Parallel()

	doc, diags := compileText(t, "cascade.yaml", nonScalarKeys, openapi.Options{})

	require.NotNil(t, doc)
	require.Len(t, diags, 3, "three errors, no warning: %+v", diags)
	type site struct {
		pointer string
		at      ir.Position
	}
	got := make([]site, 0, len(diags))
	for _, diagnostic := range diags {
		assert.Equal(t, typeMismatch, diagnostic.Code)
		assert.Equal(t, ir.SeverityError, diagnostic.Severity)
		assert.Contains(t, diagnostic.Message, "a mapping key must be a string in OpenAPI 3.1 (Format)")
		assert.True(t, strings.HasSuffix(diagnostic.Message, "; the entry is read as left out"), diagnostic.Message)
		got = append(got, site{string(diagnostic.Provenance.Pointer), diagnostic.Provenance.Position})
	}
	assert.ElementsMatch(t, []site{
		{"/paths", ir.Position{Line: 4, Column: 5}},
		{"/paths/~1a/get/responses", ir.Position{Line: 9, Column: 11}},
		{"/components/schemas", ir.Position{Line: 14, Column: 7}},
	}, got, "at the mapping's pointer, with the key's own line and column")
	assert.Len(t, allOperations(doc), 1, "the path whose key is a list lowers no operation")
	for id := range doc.Types {
		assert.False(t, strings.HasPrefix(string(id), "type/openapi/components/schemas/"), "no schema is named: %s", id)
	}
}

// TestCompile_AKeyWrittenAsTheEmptyStringStillDrawsEachRule holds the twin of
// the document above, whose three keys are written "": each rule that judges a
// key's spelling still reports it.
func TestCompile_AKeyWrittenAsTheEmptyStringStillDrawsEachRule(t *testing.T) {
	t.Parallel()
	spec := strings.NewReplacer("? [p]\n  :", `"":`, "? {code: 1}\n        :", `"":`, "? [s]\n    :", `"":`).Replace(nonScalarKeys)
	require.NotEqual(t, nonScalarKeys, spec)

	_, diags := compileText(t, "empty.yaml", spec, openapi.Options{})

	for _, code := range []string{"openapi/invalid-path-key", "openapi/invalid-status-key", "openapi/validation/validation-invalid-syntax"} {
		assert.Len(t, byCode(diags, code), 1, "%s: %+v", code, diags)
	}
	assert.Empty(t, byCode(diags, typeMismatch), "a key written \"\" is a string: %+v", diags)
}

// TestCompile_TwoKeysThatAreNoScalarAreEachReportedInEitherOrder holds two such
// keys of one mapping to one answer in both declaration orders: neither is read
// as a key "", so no entry stands for which order would decide, each is
// reported at its own line, and nothing is said of a key written twice.
func TestCompile_TwoKeysThatAreNoScalarAreEachReportedInEitherOrder(t *testing.T) {
	t.Parallel()
	const first = "    ? [a]\n    : {type: object}\n"
	const second = "    ? [b]\n    : {type: string}\n"
	head := head31 + "paths: {}\ncomponents:\n  schemas:\n"

	docAB, diagsAB := compileText(t, "ab.yaml", head+first+second, openapi.Options{})
	docBA, diagsBA := compileText(t, "ba.yaml", head+second+first, openapi.Options{})

	require.NotNil(t, docAB)
	require.NotNil(t, docBA)
	assert.Empty(t, cmp.Diff(jsonOf(t, docAB), jsonOf(t, docBA)), "one document in both orders")
	assert.Empty(t, cmp.Diff(lines(diagsAB), lines(diagsBA)), "and the same findings")
	require.Len(t, diagsAB, 2, "%+v", diagsAB)
	assert.Equal(t, []int{6, 8}, []int{diagsAB[0].Provenance.Position.Line, diagsAB[1].Provenance.Position.Line})
	for _, diagnostic := range diagsAB {
		assert.Equal(t, typeMismatch, diagnostic.Code)
		assert.Equal(t, "/components/schemas", string(diagnostic.Provenance.Pointer))
	}
	for id := range docAB.Types {
		assert.False(t, strings.HasPrefix(string(id), "type/openapi/components/schemas/"), "no type under components/schemas: %s", id)
	}
}

// TestCompile_AKeyThatIsNoScalarInAnotherDocumentIsReportedWhereItIsWritten
// holds a path item a reference reads from another file to the Format error a
// key that is no scalar draws, at the key's own line and column in that file.
// The entry is set aside while the model is built and put back before what the
// reference reaches is checked, so the key is still there to report.
func TestCompile_AKeyThatIsNoScalarInAnotherDocumentIsReportedWhereItIsWritten(t *testing.T) {
	t.Parallel()
	const ext = "p:\n  get: {responses: {\"200\": {description: ok}}}\n  ? [a]\n  : 1\n"
	const root = "openapi: 3.2.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /x: {$ref: \"other.yaml#/p\"}\n"

	diags := compileBesideExpectingErrors(t, ext, root)

	require.Len(t, diags, 1, "%+v", diags)
	diagnostic := diags[0]
	assert.Equal(t, typeMismatch, diagnostic.Code)
	assert.Equal(t, ir.SeverityError, diagnostic.Severity)
	assert.Contains(t, diagnostic.Message, "a mapping key must be a string in OpenAPI 3.2 (Format)")
	assert.Equal(t, 1, diagnostic.Provenance.Source, "in the file the reference reads")
	assert.Equal(t, "/p", string(diagnostic.Provenance.Pointer))
	assert.Equal(t, ir.Position{Line: 3, Column: 5}, diagnostic.Provenance.Position)
}
