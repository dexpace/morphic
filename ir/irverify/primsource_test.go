// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// primSourceCode is what a primitive credited to a source draws.
const primSourceCode = "ir/prim-names-source"

// primitiveOf returns the one primitive of scenarioBase.
func primitiveOf(doc *ir.Document) *ir.Primitive {
	return doc.Types["type/primitive/string"].(*ir.Primitive)
}

// TestVerify_PrimitiveNamesNoSource pins that a primitive is shared by every
// source, so it is credited to none: a real source index draws the code at the
// primitive, and ir.NoSource draws none, with or without sources declared.
func TestVerify_PrimitiveNamesNoSource(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		sources []ir.SourceInfo
		prov    ir.Provenance
		want    []string
	}{
		{"credited to the first source", oneSource(), ir.Provenance{Source: 0}, []string{"types[type/primitive/string]"}},
		{"credited to a later source", append(oneSource(), oneSource()...), ir.Provenance{Source: 1},
			[]string{"types[type/primitive/string]"}},
		{"shared, sources declared", oneSource(), ir.Provenance{Source: ir.NoSource}, nil},
		{"shared, no sources declared", nil, ir.Provenance{Source: ir.NoSource}, nil},
		{"zero provenance, no sources declared", nil, ir.Provenance{}, nil},
		{"a pointer on the first source, no sources declared", nil, ir.Provenance{Pointer: "/components/schemas/S"},
			[]string{"types[type/primitive/string]"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := scenarioBase()
			doc.Sources = tc.sources
			primitiveOf(doc).Provenance = tc.prov
			assert.Equal(t, tc.want, violationPaths(irverify.Verify(doc), primSourceCode))
		})
	}
}

// TestVerify_PrimitiveSourceMessage pins the wording, which names the source
// the primitive was credited to.
func TestVerify_PrimitiveSourceMessage(t *testing.T) {
	t.Parallel()
	doc := scenarioBase()
	doc.Sources = oneSource()
	primitiveOf(doc).Provenance = ir.Provenance{Source: 0}
	var got []string
	for _, violation := range irverify.Verify(doc) {
		if violation.Code == primSourceCode {
			got = append(got, violation.Message)
		}
	}
	assert.Equal(t, []string{"primitive is credited to source 0, and a primitive shared by every source names none"}, got)
}

// TestVerify_OnlyAPrimitiveIsHeldToNoSource pins that the rule is a primitive's:
// a model credited to the first source is clean.
func TestVerify_OnlyAPrimitiveIsHeldToNoSource(t *testing.T) {
	t.Parallel()
	doc := scenarioBase()
	doc.Sources = oneSource()
	doc.Types["type/x/M"].Common().Provenance = ir.Provenance{Source: 0}
	assert.Empty(t, violationPaths(irverify.Verify(doc), primSourceCode))
}
