// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

//go:build pending

package e2e_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// Pending: an allOf entry writing a $ref beside other keys is listed as the
// reference and the rest, and where the rest is the conjunct the position
// lowers from it is lowered as a position of its own, whose statement lists
// the reference again. The reference is then read twice: kept and reported by
// the entry's own election and again by the composition's.

// TestRefConjunction_AValueSetBesideAnEntrysRefIsReportedOnce holds that a
// value set written beside the $ref of an allOf entry keeps the $ref once and
// reports it once, as beside a $ref at the position.
func TestRefConjunction_AValueSetBesideAnEntrysRefIsReportedOnce(t *testing.T) {
	t.Parallel()
	for name, schema := range map[string]string{
		"beside the $ref":       "{$ref: '#/components/schemas/Str', enum: [a]}",
		"beside an entry's ref": "{allOf: [{$ref: '#/components/schemas/Str', enum: [a]}]}",
	} {
		doc, diags := compileUnrefused(t, admissionSpec("3.1.0", "", "Str: {type: string}", "Use: "+schema))
		enum, ok := kindNode(t, doc, componentID("Use")).(*ir.Enum)
		require.True(t, ok, name)
		assert.Contains(t, enum.Unmodeled, "openapi:$ref", name)
		assert.Equal(t, 1, degradedInfosUnder(diags, "/components/schemas/Use"), "%s: one report for one $ref kept", name)
	}
}

// TestRefConjunction_AnEntrysRefIsKeptOnOneNode holds that a $ref an
// election passes over is kept on one node of the document, whether a union
// is written beside it or beside its entry.
func TestRefConjunction_AnEntrysRefIsKeptOnOneNode(t *testing.T) {
	t.Parallel()
	union := "oneOf: [{type: string}, {type: integer}]"
	for name, schema := range map[string]string{
		"beside the $ref":       "{$ref: '#/components/schemas/Top', " + union + "}",
		"beside an entry's ref": "{allOf: [{$ref: '#/components/schemas/Top', " + union + "}]}",
	} {
		doc, diags := compileUnrefused(t, admissionSpec("3.1.0", "", "Top: {}", "Use: "+schema))
		holders := 0
		for id, td := range doc.Types {
			if _, kept := td.Common().Unmodeled["openapi:$ref"]; kept && strings.Contains(string(id), "/Use") {
				holders++
			}
		}
		assert.Equal(t, 1, holders, "%s: the $ref is kept on one node", name)
		assert.Equal(t, 1, degradedInfosUnder(diags, "/components/schemas/Use"), "%s: and reported once", name)
	}
}

// TestRefConjunction_ASplitEntryKeptWholeHoldsItsWordsAlone holds that an
// allOf entry writing a $ref beside a keyword the conjuncts disagree on keeps
// the keyword whole without the $ref, which the composition already holds as a
// mixin, as the same keyword on an entry of its own is kept.
func TestRefConjunction_ASplitEntryKeptWholeHoldsItsWordsAlone(t *testing.T) {
	t.Parallel()
	a := "{type: object, properties: {a: {type: string}}}"
	b := "{type: object, properties: {b: {type: string}}}"
	for name, schema := range map[string]string{
		"beside the $ref": "{allOf: [{$ref: '#/components/schemas/A', description: d}, " +
			"{$ref: '#/components/schemas/B', description: e}]}",
		"on an entry": "{allOf: [{$ref: '#/components/schemas/A'}, {description: d}, " +
			"{$ref: '#/components/schemas/B'}, {description: e}]}",
	} {
		doc, _ := compileUnrefused(t, admissionSpec("3.1.0", "", "A: "+a, "B: "+b, "Use: "+schema))
		kept := kindNode(t, doc, componentID("Use")).Common().Unmodeled
		require.Len(t, kept, 2, name)
		for key, entry := range kept {
			assert.NotContains(t, string(entry.Value), "$ref", "%s: %s holds the keyword alone", name, key)
		}
	}
}

// TestRefConjunction_AnAllOfOfOneRefToFalseIsTheReference holds that an allOf
// of one $ref saying nothing else is the reference where its target is the
// false schema too, as the bare $ref is, at a property and at a component.
func TestRefConjunction_AnAllOfOfOneRefToFalseIsTheReference(t *testing.T) {
	t.Parallel()
	spec := admissionSpec("3.1.0", "", "F: false",
		"Bare: {$ref: '#/components/schemas/F'}",
		"Wrapped: {allOf: [{$ref: '#/components/schemas/F'}]}",
		"H: {type: object, properties: {bare: {$ref: '#/components/schemas/F'}, "+
			"wrapped: {allOf: [{$ref: '#/components/schemas/F'}]}}}")
	doc, _ := compileUnrefused(t, spec)
	h, ok := kindNode(t, doc, componentID("H")).(*ir.Model)
	require.True(t, ok)
	require.Len(t, h.Properties, 2)
	assert.Equal(t, h.Properties[0].Type.Target, h.Properties[1].Type.Target, "the wrapped use names what the bare one does")
	assert.Equal(t, kindNode(t, doc, componentID("Bare")).Kind(), kindNode(t, doc, componentID("Wrapped")).Kind())
}
