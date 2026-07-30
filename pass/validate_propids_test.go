// This file covers the PropID reference class, whose two checks make different
// claims about the same kind of identity: membership anywhere in the document,
// and — where the root is written down — membership in the type that names it.
package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// propPathOperation returns an operation whose HTTP binding walks path through
// the response body — one of the PropID carriers the reference walk sees and no
// registry can resolve.
func propPathOperation(path []ir.PropID) ir.Operation {
	return ir.Operation{
		ID: "op/o", Name: ir.Naming{Source: "o"},
		Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
			Method:           "GET",
			ResponseBodyPath: &ir.PropPath{Root: &ir.TypeRef{Target: "t/m"}, Segments: path},
		}}},
	}
}

// TestValidate_PropPathSegmentResolves accepts a segment naming a declared
// property and rejects one naming nothing, which is the whole of what was
// checked by nothing before: PropPath.Segments is a reference the type-driven
// walk classifies as unresolvable, because a property lives inside its model
// rather than in a document-level registry.
func TestValidate_PropPathSegmentResolves(t *testing.T) {
	t.Parallel()
	ok := pass.Validate(docWithOperation(propPathOperation([]ir.PropID{"p/m/a"})))
	assert.NotContains(t, codes(ok), "ir/dangling-prop-ref",
		"a segment naming a declared property resolves")

	bad := pass.Validate(docWithOperation(propPathOperation([]ir.PropID{"p/m/ghost"})))
	assert.Contains(t, codes(bad), "ir/dangling-prop-ref",
		"a segment naming no property is reported")
}

// TestValidate_ParamPathSegmentResolves covers the second carrier shape, whose
// segments sit inside a parameter binding rather than a body path. Both are
// reached by the same walk, so this is the assertion that the walk's reach —
// not a list of field names — is what makes the check complete.
func TestValidate_ParamPathSegmentResolves(t *testing.T) {
	t.Parallel()
	op := ir.Operation{
		ID: "op/o", Name: ir.Naming{Source: "o"},
		Params: []ir.Parameter{{Name: ir.Naming{Source: "book"}, Type: ir.TypeRef{Target: "t/m"}}},
		Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
			Method: "GET",
			ParamBindings: []ir.HTTPParamBinding{{
				Param: "book", Location: ir.HTTPLocationPath,
				ParamPath: []ir.PropID{"p/m/ghost"},
			}},
		}}},
	}
	assert.Contains(t, codes(pass.Validate(docWithOperation(op))), "ir/dangling-prop-ref")
}

// TestValidate_PropIDMapKeyIsLeftToTheEncodingCheck pins the one exclusion. A
// PropID reached as a map key is a Content.Encoding key, and checkEncodingKeys
// resolves it against the properties of the model the content names — a tighter
// claim about the same defect. One defect must read as one location, so the
// membership check stays out of that map.
func TestValidate_PropIDMapKeyIsLeftToTheEncodingCheck(t *testing.T) {
	t.Parallel()
	doc := docWithOperation(ir.Operation{
		ID: "op/o", Name: ir.Naming{Source: "o"},
		Request: &ir.Payload{Contents: []ir.Content{
			multipartContent(map[ir.PropID]ir.PartEncoding{"p/m/ghost": {Multi: true}}),
		}},
	})
	got := codes(pass.Validate(doc))
	assert.Contains(t, got, "ir/encoding-key-unknown-property", "the tighter claim is made")
	assert.NotContains(t, got, "ir/dangling-prop-ref", "and it is the only one")
}

// discriminatedDoc returns a document whose model carries a discriminator
// tagging prop.
func discriminatedDoc(prop ir.PropID) *ir.Document {
	doc := validDoc()
	base := doc.Types["t/m"].(*ir.Model) //nolint:forcetypeassert // validDoc builds it
	base.Discriminator = &ir.Discriminator{Property: prop}
	return doc
}

// TestValidate_DiscriminatorPropertyBelongsToItsModel is the tighter claim the
// membership check leaves to checkDiscriminators: a tag on a property of some
// other model resolves as an identity and still routes nothing, because an
// emitter reads the tag off an instance of *this* model.
func TestValidate_DiscriminatorPropertyBelongsToItsModel(t *testing.T) {
	t.Parallel()
	assert.NotContains(t, codes(pass.Validate(discriminatedDoc("p/m/a"))),
		"pass/discriminator-unknown-property", "the model declares the tag property")

	assert.NotContains(t, codes(pass.Validate(discriminatedDoc(""))),
		"pass/discriminator-unknown-property",
		"a union-style discriminator tags no property by identity")

	doc := discriminatedDoc("p/other/a")
	doc.Types["t/other"] = &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/other"}, Properties: []ir.Property{
		{ID: "p/other/a", WireName: "a", Type: ir.TypeRef{Target: "t/prim/string"}},
	}}
	got := codes(pass.Validate(doc))
	assert.Contains(t, got, "pass/discriminator-unknown-property",
		"a property of an unrelated model is not this model's tag")
	assert.NotContains(t, got, "ir/dangling-prop-ref",
		"it is a declared property, so membership has nothing to say")
}

// TestValidate_DiscriminatorPropertyThroughComposition accepts a tag the model
// composes in rather than declares, which is how an OpenAPI subtype inherits the
// base's tag property.
func TestValidate_DiscriminatorPropertyThroughComposition(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["t/sub"] = &ir.Model{
		TypeCommon:    ir.TypeCommon{ID: "t/sub"},
		Base:          &ir.TypeRef{Target: "t/m"},
		Discriminator: &ir.Discriminator{Property: "p/m/a"},
	}
	assert.NotContains(t, codes(pass.Validate(doc)), "pass/discriminator-unknown-property",
		"a tag property reached through Base is one the model exposes")
}
