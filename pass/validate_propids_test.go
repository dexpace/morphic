// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// This file covers the PropertyID reference class, whose two checks make
// different claims about the same kind of identity: membership anywhere in the
// document, and — where the root is written down — membership in the type that
// names it.
package pass_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// propertyPathOperation returns an operation whose HTTP binding walks path
// through the response body — one of the PropertyID carriers the reference walk
// sees and no registry can resolve.
func propertyPathOperation(path []ir.PropertyID) ir.Operation {
	return ir.Operation{
		ID: "operation/o", Name: ir.Naming{Source: "o"},
		Bindings: ir.OperationBindings{HTTP: []ir.HTTPBinding{{
			Method:           "GET",
			ResponseBodyPath: &ir.PropertyPath{Root: &ir.TypeRef{Target: "type/m"}, Segments: path},
		}}},
	}
}

// TestValidate_PropertyPathSegmentResolves accepts a segment naming a declared
// property and rejects one naming nothing, which is the whole of what was
// checked by nothing before: PropertyPath.Segments is a reference the
// type-driven walk classifies as unresolvable, because a property lives inside
// its model rather than in a document-level registry.
func TestValidate_PropertyPathSegmentResolves(t *testing.T) {
	t.Parallel()
	ok := validated(t, docWithOperation(propertyPathOperation([]ir.PropertyID{"property/m/a"})))
	assert.NotContains(t, irtest.DiagCodes(ok), "ir/dangling-property-ref",
		"a segment naming a declared property resolves")

	bad := validated(t, docWithOperation(propertyPathOperation([]ir.PropertyID{"property/m/ghost"})))
	assert.Contains(t, irtest.DiagCodes(bad), "ir/dangling-property-ref",
		"a segment naming no property is reported")
}

// TestValidate_APropertyIDSpelledAsAnotherClassesIDDangles pins that only a
// property declares a PropertyID: a segment spelling the operation's own ID, an
// OperationID, names no property.
func TestValidate_APropertyIDSpelledAsAnotherClassesIDDangles(t *testing.T) {
	t.Parallel()
	bad := validated(t, docWithOperation(propertyPathOperation([]ir.PropertyID{"operation/o"})))
	assert.Contains(t, irtest.DiagCodes(bad), "ir/dangling-property-ref")
}

// TestValidate_DanglingPropertyIDsAreReportedByLocation holds the diagnostics
// to the order of their locations, which is not the walk's: eleven segments are
// walked [0] to [10], and "[10]" sorts between "[0]" and "[1]".
func TestValidate_DanglingPropertyIDsAreReportedByLocation(t *testing.T) {
	t.Parallel()
	segments := make([]ir.PropertyID, 11)
	for i := range segments {
		segments[i] = ir.PropertyID(fmt.Sprintf("property/m/ghost%d", i))
	}
	var where []string
	for _, diagnostic := range validated(t, docWithOperation(propertyPathOperation(segments))) {
		if diagnostic.Code == "ir/dangling-property-ref" {
			where = append(where, diagnostic.Provenance.Node)
		}
	}
	require.Len(t, where, 11)
	assert.True(t, slices.IsSorted(where), "reported in location order, got %v", where)
	assert.True(t, strings.HasSuffix(where[1], ".Segments[10]"), "[10] comes second, got %v", where)
}

// TestValidate_ParamPathSegmentResolves covers the second carrier shape, whose
// segments sit inside a parameter binding rather than a body path. Both are
// reached by the same walk, so this is the assertion that the walk's reach —
// not a list of field names — is what makes the check complete.
func TestValidate_ParamPathSegmentResolves(t *testing.T) {
	t.Parallel()
	operation := ir.Operation{
		ID: "operation/o", Name: ir.Naming{Source: "o"},
		Params: []ir.Parameter{{Name: ir.Naming{Source: "book"}, Type: ir.TypeRef{Target: "type/m"}}},
		Bindings: ir.OperationBindings{HTTP: []ir.HTTPBinding{{
			Method: "GET",
			ParamBindings: []ir.HTTPParamBinding{{
				Param: "book", Location: ir.HTTPLocationPath,
				ParamPath: []ir.PropertyID{"property/m/ghost"},
			}},
		}}},
	}
	assert.Contains(t, irtest.DiagCodes(validated(t, docWithOperation(operation))), "ir/dangling-property-ref")
}

// TestValidate_PropertyIDMapKeyIsLeftToTheEncodingCheck pins the one exclusion.
// A PropertyID reached as a map key is a Content.Encoding key, and
// checkEncodingKeys resolves it against the properties of the model the content
// names — a tighter claim about the same defect. One defect must read as one
// location, so the membership check stays out of that map.
func TestValidate_PropertyIDMapKeyIsLeftToTheEncodingCheck(t *testing.T) {
	t.Parallel()
	doc := docWithOperation(ir.Operation{
		ID: "operation/o", Name: ir.Naming{Source: "o"},
		Request: &ir.Payload{Contents: []ir.Content{
			multipartContent(map[ir.PropertyID]ir.PartEncoding{"property/m/ghost": {RepeatsPerArrayItem: true}}),
		}},
	})
	got := irtest.DiagCodes(validated(t, doc))
	assert.Contains(t, got, "ir/encoding-key-unknown-property", "the tighter claim is made")
	assert.NotContains(t, got, "ir/dangling-property-ref", "and it is the only one")
}

// discriminatedDoc returns a document whose model carries a discriminator
// tagging property.
func discriminatedDoc(property ir.PropertyID) *ir.Document {
	doc := validDoc()
	base := doc.Types["type/m"].(*ir.Model) // validDoc builds it
	base.Discriminator = &ir.Discriminator{Property: property}
	return doc
}

// TestValidate_DiscriminatorPropertyBelongsToItsModel is the tighter claim the
// membership check leaves to checkDiscriminators: a tag on a property of some
// other model resolves as an identity and still routes nothing, because an
// emitter reads the tag off an instance of *this* model.
func TestValidate_DiscriminatorPropertyBelongsToItsModel(t *testing.T) {
	t.Parallel()
	assert.NotContains(t, irtest.DiagCodes(validated(t, discriminatedDoc("property/m/a"))),
		"pass/discriminator-unknown-property", "the model declares the tag property")

	assert.NotContains(t, irtest.DiagCodes(validated(t, discriminatedDoc(""))),
		"pass/discriminator-unknown-property",
		"a union-style discriminator tags no property by identity")

	doc := discriminatedDoc("property/other/a")
	doc.Types["type/other"] = &ir.Model{ID: "type/other", Properties: []ir.Property{
		{ID: "property/other/a", WireName: "a", Type: ir.TypeRef{Target: "type/primitive/string"}},
	}}
	got := irtest.DiagCodes(validated(t, doc))
	assert.Contains(t, got, "pass/discriminator-unknown-property",
		"a property of an unrelated model is not this model's tag")
	assert.NotContains(t, got, "ir/dangling-property-ref",
		"it is a declared property, so membership has nothing to say")
}

// TestValidate_DiscriminatorPropertyThroughComposition accepts a tag the model
// composes in rather than declares, which is how an OpenAPI subtype inherits the
// base's tag property.
func TestValidate_DiscriminatorPropertyThroughComposition(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["type/sub"] = &ir.Model{
		ID:            "type/sub",
		Base:          &ir.TypeRef{Target: "type/m"},
		Discriminator: &ir.Discriminator{Property: "property/m/a"},
	}
	assert.NotContains(t, irtest.DiagCodes(validated(t, doc)), "pass/discriminator-unknown-property",
		"a tag property reached through Base is one the model exposes")
}
