// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package replacement_test

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/replacement"
	"github.com/dexpace/morphic/ir"
)

// mapped is a context whose policy reads replacements from x-replaced-by.
func mapped() lowering.Ctx {
	policy := lowering.ExtensionPromotions{Targets: map[lowering.ExtensionTarget][]lowering.ExtensionSource{
		lowering.TargetDeprecationReplacedBy: {{Key: "x-replaced-by"}},
	}}
	return lowering.New(0, &load.Document{Doc: &soa.OpenAPI{}}, lowering.Policy{Promotions: policy})
}

// names is a node's x-replaced-by key, holding value.
func names(value string) ir.Unmodeled {
	return ir.Unmodeled{"openapi:x-replaced-by": {
		Reason: ir.ReasonVendorExtension, Value: ir.RawValue(value), Provenance: ir.Provenance{Pointer: "/x-replaced-by"},
	}}
}

// deprecated is a deprecation naming nothing yet.
func deprecated() *ir.Deprecation { return &ir.Deprecation{} }

// replacementDoc is a document in which each kind of node replacement reads
// is deprecated and names its replacement through value, and each has another
// of its kind to name.
func replacementDoc(c lowering.Ctx, value string) *ir.Document {
	v2 := c.IDs().NamedType("/components/schemas/v2")
	old := c.IDs().NamedType("/components/schemas/old")
	header := func(id ir.PropID, name string, unmodeled ir.Unmodeled, dep *ir.Deprecation) ir.Header {
		return ir.Header{ID: id, WireName: name, Unmodeled: unmodeled, Deprecation: dep}
	}
	return &ir.Document{
		Services: []ir.Service{{Groups: []ir.OperationGroup{{Operations: []ir.Operation{
			{ID: "op/old", Name: ir.Naming{Source: "getOld"}, Unmodeled: names(value), Deprecation: deprecated(),
				Params: []ir.Parameter{
					{ID: "param/old", Name: ir.Naming{Source: "old"}, Unmodeled: names(value), Deprecation: deprecated()},
					{ID: "param/v2", Name: ir.Naming{Source: "v2"}},
				},
				Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{ParamBindings: []ir.HTTPParamBinding{
					{Param: "param/old", Location: ir.HTTPLocationQuery}, {Param: "param/v2", Location: ir.HTTPLocationQuery},
				}}}},
				Responses: []ir.Response{{Headers: []ir.Header{
					header("p/h/old", "X-Old", names(value), deprecated()), header("p/h/v2", "V2", nil, nil),
				}}},
				Errors: []ir.ErrorCase{{Headers: []ir.Header{
					header("p/e/old", "X-Old", names(value), deprecated()), header("p/e/v2", "v2", nil, nil),
				}}},
				Default: &ir.Response{Headers: []ir.Header{
					header("p/d/old", "X-Old", names(value), deprecated()), header("p/d/v2", "v2", nil, nil),
				}},
			},
			{ID: "op/v2", Name: ir.Naming{Source: "v2"}},
		}}}}},
		Types: ir.TypeRegistry{
			old: &ir.Model{ID: old, Unmodeled: names(value), Deprecation: deprecated(), Properties: []ir.Property{
				{ID: "p/old", WireName: "old", Unmodeled: names(value), Deprecation: deprecated()},
				{ID: "p/v2", WireName: "v2"},
			}},
			v2:         &ir.Model{ID: v2},
			"t/scalar": &ir.Scalar{ID: "t/scalar"},
		},
		Auth: map[ir.AuthID]ir.AuthScheme{
			"auth/old": {ID: "auth/old", Name: ir.Naming{Source: "old"}, Unmodeled: names(value), Deprecation: deprecated()},
			"auth/v2":  {ID: "auth/v2", Name: ir.Naming{Source: "v2"}},
		},
	}
}

// TestResolve_NamesEachReplacementInItsNodesScope pins how each node's name
// resolves: an operation by operationId, a parameter and a header among its
// siblings, a property within its model, a schema by its components name and a
// security scheme by its key, each marked as promoted.
func TestResolve_NamesEachReplacementInItsNodesScope(t *testing.T) {
	t.Parallel()
	c := mapped()
	doc := replacementDoc(c, `"v2"`)
	assert.Empty(t, replacement.Resolve(c, doc))

	op := doc.Services[0].Groups[0].Operations[0]
	v2 := c.IDs().NamedType("/components/schemas/v2")
	old := c.IDs().NamedType("/components/schemas/old")
	for _, tc := range []struct {
		name string
		dep  *ir.Deprecation
		prov ir.Provenance
		want ir.EntityRef
	}{
		{"an operation", op.Deprecation, op.Provenance, ir.EntityRef{Operation: "op/v2"}},
		{"a parameter", op.Params[0].Deprecation, op.Params[0].Provenance, ir.EntityRef{Param: "param/v2"}},
		{"a response header, in any case", op.Responses[0].Headers[0].Deprecation, op.Responses[0].Headers[0].Provenance,
			ir.EntityRef{Property: "p/h/v2"}},
		{"an error case's header", op.Errors[0].Headers[0].Deprecation, op.Errors[0].Headers[0].Provenance,
			ir.EntityRef{Property: "p/e/v2"}},
		{"a default's header", op.Default.Headers[0].Deprecation, op.Default.Headers[0].Provenance,
			ir.EntityRef{Property: "p/d/v2"}},
		{"a schema", doc.Types[old].Common().Deprecation, doc.Types[old].Common().Provenance, ir.EntityRef{Type: v2}},
		{"a property", doc.Types[old].(*ir.Model).Properties[0].Deprecation,
			doc.Types[old].(*ir.Model).Properties[0].Provenance, ir.EntityRef{Property: "p/v2"}},
		{"a security scheme", doc.Auth["auth/old"].Deprecation, doc.Auth["auth/old"].Provenance, ir.EntityRef{Auth: "auth/v2"}},
	} {
		require.NotNil(t, tc.dep, tc.name)
		assert.Equal(t, []ir.EntityRef{tc.want}, tc.dep.ReplacedBy, tc.name)
		assert.Equal(t, lowering.ExtensionPromotionHeuristic, tc.prov.Inferred, "%s is marked promoted", tc.name)
	}
}

// TestResolve_ANameNamingNothingFillsNothing pins that the names fill the field
// all or none: a list one of whose names names nothing, or one naming a node
// itself, leaves the field unset and is reported at the key.
func TestResolve_ANameNamingNothingFillsNothing(t *testing.T) {
	t.Parallel()
	for _, value := range []string{`["v2","gone"]`, `"old"`, `["getOld"]`, `"X-Old"`} {
		c := mapped()
		doc := replacementDoc(c, value)
		diags := replacement.Resolve(c, doc)
		require.NotEmpty(t, diags, value)
		for _, d := range diags {
			assert.Equal(t, diag.DegradedConstruct, d.Code, value)
			assert.Equal(t, ir.SeverityInfo, d.Severity, value)
		}
		op := doc.Services[0].Groups[0].Operations[0]
		assert.Empty(t, op.Deprecation.ReplacedBy, value)
		assert.Empty(t, op.Errors[0].Headers[0].Deprecation.ReplacedBy, value)
	}
}

// TestResolve_AnUnmappedOrUndeprecatedNodeTakesNone pins that a policy naming
// no key reads nothing, and that a name beside no deprecation annotates
// nothing, as a deprecation's other members do.
func TestResolve_AnUnmappedOrUndeprecatedNodeTakesNone(t *testing.T) {
	t.Parallel()
	unmapped := lowering.New(0, &load.Document{Doc: &soa.OpenAPI{}}, lowering.Policy{})
	doc := replacementDoc(unmapped, `"v2"`)
	assert.Empty(t, replacement.Resolve(unmapped, doc))
	assert.Empty(t, doc.Services[0].Groups[0].Operations[0].Deprecation.ReplacedBy)

	c := mapped()
	doc = replacementDoc(c, `"v2"`)
	doc.Services[0].Groups[0].Operations[0].Deprecation = nil
	doc.Types[c.IDs().NamedType("/components/schemas/v2")] = (*ir.Model)(nil)
	assert.Empty(t, replacement.Resolve(c, doc))
	assert.Nil(t, doc.Services[0].Groups[0].Operations[0].Deprecation)
}

// TestResolve_StopsAtTheGroupDepthBound holds the walk to ir.MaxGroupDepth: an
// operation nested deeper than any document the compiler builds is not read.
func TestResolve_StopsAtTheGroupDepthBound(t *testing.T) {
	t.Parallel()
	c := mapped()
	deep := ir.OperationGroup{Operations: []ir.Operation{
		{ID: "op/deep", Unmodeled: names(`"v2"`), Deprecation: deprecated()}, {ID: "op/v2", Name: ir.Naming{Source: "v2"}},
	}}
	for range ir.MaxGroupDepth + 1 {
		deep = ir.OperationGroup{Groups: []ir.OperationGroup{deep}}
	}
	doc := &ir.Document{Services: []ir.Service{{Groups: []ir.OperationGroup{deep}}}}
	assert.Empty(t, replacement.Resolve(c, doc))
	for range ir.MaxGroupDepth + 1 {
		deep = deep.Groups[0]
	}
	assert.Empty(t, deep.Operations[0].Deprecation.ReplacedBy)
}
