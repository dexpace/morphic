// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// nothingTargets are targets an allOf of one $ref to them says nothing more
// than: the false schema, the true one, the empty one, and a component the
// document does not declare, whose reference fails.
var nothingTargets = map[string]string{
	"false":   "T: false",
	"true":    "T: true",
	"empty":   "T: {}",
	"missing": "Other: {type: string}",
}

// writtenAnyway is writtenAt for a document the compiler may refuse: the
// reference to a missing target is an error in every spelling.
func writtenAnyway(t *testing.T, site conjSite, target, schema string) entryWritten {
	t.Helper()
	doc, diags := compileAnnotationSpec(t, "conjunction", site.write("3.1.0", schema, target))
	require.NotNil(t, doc)
	relocate := relocater(site.pointer)
	got := entryWritten{doc: relocate(withoutVersion(t, doc))}
	for _, d := range diags {
		got.diags = append(got.diags, fmt.Sprintf("%s %s %s", d.Severity, d.Code, relocate(string(d.Provenance.Pointer))))
	}
	slices.Sort(got.diags)
	return got
}

// TestRefConjunction_AUseSayingNothingMoreIsTheBareRefOverAnyTarget holds an
// allOf of one $ref saying nothing else to what the bare $ref compiles to at
// each of the eight sites, whatever its target admits: nothing, everything,
// or, for a reference naming nothing, the top type with one error, up to the
// entry's pointer. An entry `true` beside it says no more than `{}` does.
func TestRefConjunction_AUseSayingNothingMoreIsTheBareRefOverAnyTarget(t *testing.T) {
	t.Parallel()
	ref := "{$ref: '#/components/schemas/T'}"
	for _, site := range conjSites {
		for name, target := range nothingTargets {
			bare := writtenAnyway(t, site, target, ref)
			for _, schema := range []string{"{allOf: [" + ref + "]}", "{allOf: [" + ref + ", true]}", "{allOf: [" + ref + ", {}]}"} {
				got := writtenAnyway(t, site, target, schema)
				assert.Empty(t, cmp.Diff(bare, got, cmp.AllowUnexported(entryWritten{})), "%s, %s: %s", site.name, name, schema)
			}
		}
		obj := conjTargets["Obj"]
		empty := writtenAnyway(t, site, obj, "{allOf: [{"+refTo("Obj")+"}, {}]}")
		got := writtenAnyway(t, site, obj, "{allOf: [{"+refTo("Obj")+"}, true]}")
		assert.Empty(t, cmp.Diff(empty, got, cmp.AllowUnexported(entryWritten{})), "%s: true is the empty schema", site.name)
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
