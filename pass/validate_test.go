package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// validDoc returns a minimal internally consistent document that every case
// mutates. Keep it tiny: one model referencing one primitive.
func validDoc() *ir.Document {
	return &ir.Document{
		IRVersion: ir.IRVersion, Name: "t", Version: "1",
		Types: ir.TypeRegistry{
			"t/prim/string": &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "t/prim/string"}, Prim: "string"},
			"t/m": &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/m"}, Properties: []ir.Property{
				{ID: "p/m/a", WireName: "a", Type: ir.TypeRef{Target: "t/prim/string"}},
			}},
		},
	}
}

// docWithOperation wraps a single operation in a service/group so the operation
// walkers reach it.
func docWithOperation(op ir.Operation) *ir.Document {
	doc := validDoc()
	doc.Services = []ir.Service{{
		ID:     "s",
		Groups: []ir.OperationGroup{{Operations: []ir.Operation{op}}},
	}}
	return doc
}

func codes(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, d.Code)
	}
	return out
}

func TestValidate_CleanDocumentHasNoDiagnostics(t *testing.T) {
	t.Parallel()
	assert.Empty(t, pass.Validate(validDoc()))
}

func TestValidate_DanglingTypeRef(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	m := doc.Types["t/m"].(*ir.Model)
	m.Properties[0].Type.Target = "t/nowhere"
	diags := pass.Validate(doc)
	require.NotEmpty(t, diags)
	assert.Contains(t, codes(diags), "ir/dangling-type-ref")
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
}

func TestValidate_DiscriminatorMissingVariant(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["t/u"] = &ir.Union{
		TypeCommon: ir.TypeCommon{ID: "t/u"},
		Variants:   []ir.Variant{{Type: ir.TypeRef{Target: "t/m"}}},
		Discriminator: &ir.Discriminator{
			PropertyName: "kind",
			// t/prim/string exists but is not one of the union's variants.
			Mapping: map[string]ir.TypeID{"a": "t/prim/string"},
		},
	}
	diags := pass.Validate(doc)
	require.NotEmpty(t, diags)
	assert.Contains(t, codes(diags), "pass/discriminator-missing-variant")
}

func TestValidate_DuplicateWireName(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	m := doc.Types["t/m"].(*ir.Model)
	m.Properties = append(m.Properties, ir.Property{
		ID: "p/m/b", WireName: "a", Type: ir.TypeRef{Target: "t/prim/string"},
	})
	diags := pass.Validate(doc)
	require.NotEmpty(t, diags)
	assert.Contains(t, codes(diags), "pass/duplicate-wire-name")
}

func TestValidate_ParamBinding_UnknownParam(t *testing.T) {
	t.Parallel()
	op := ir.Operation{
		ID:     "op",
		Params: []ir.Parameter{{Name: ir.Naming{Source: "id"}, Type: ir.TypeRef{Target: "t/prim/string"}}},
		Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
			Method: "GET", URITemplate: "/x",
			ParamBindings: []ir.HTTPParamBinding{{Param: "ghost", Location: ir.HTTPLocationPath}},
		}}},
	}
	diags := pass.Validate(docWithOperation(op))
	require.NotEmpty(t, diags)
	assert.Contains(t, codes(diags), "pass/param-binding-mismatch")
}

func TestValidate_ParamBinding_DoubleBound(t *testing.T) {
	t.Parallel()
	op := ir.Operation{
		ID:     "op",
		Params: []ir.Parameter{{Name: ir.Naming{Source: "id"}, Type: ir.TypeRef{Target: "t/prim/string"}}},
		Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
			Method: "GET", URITemplate: "/x",
			ParamBindings: []ir.HTTPParamBinding{
				{Param: "id", Location: ir.HTTPLocationQuery},
				{Param: "id", Location: ir.HTTPLocationQuery},
			},
		}}},
	}
	diags := pass.Validate(docWithOperation(op))
	require.NotEmpty(t, diags)
	assert.Contains(t, codes(diags), "pass/param-binding-mismatch")
}

func TestValidate_OneWayWithResponses(t *testing.T) {
	t.Parallel()
	op := ir.Operation{ID: "op", OneWay: true, Responses: []ir.Response{{}}}
	diags := pass.Validate(docWithOperation(op))
	require.NotEmpty(t, diags)
	assert.Contains(t, codes(diags), "pass/oneway-with-responses")
}

func TestValidate_ArgsOutsideGraphQL(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	m := doc.Types["t/m"].(*ir.Model)
	m.Properties[0].Args = []ir.Parameter{
		{Name: ir.Naming{Source: "first"}, Type: ir.TypeRef{Target: "t/prim/string"}},
	}
	diags := pass.Validate(doc)
	require.NotEmpty(t, diags)
	assert.Contains(t, codes(diags), "pass/args-outside-graphql")
}

func TestValidate_DanglingAuthRef(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Services = []ir.Service{{
		ID:   "s",
		Auth: []ir.AuthRequirement{{Schemes: []ir.SchemeUse{{Scheme: "auth/nope"}}}},
	}}
	diags := pass.Validate(doc)
	require.NotEmpty(t, diags)
	assert.Contains(t, codes(diags), "pass/dangling-auth-ref")
}

func TestValidate_DuplicateEnumValuesAreLegal(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["t/e"] = &ir.Enum{
		TypeCommon: ir.TypeCommon{ID: "t/e"},
		ValueType:  ir.PrimString,
		Members: []ir.EnumMember{
			{Name: ir.Naming{Source: "a"}, Value: ir.Value{Kind: ir.ValueString, Str: "x"}},
			{Name: ir.Naming{Source: "b"}, Value: ir.Value{Kind: ir.ValueString, Str: "x"}},
		},
	}
	assert.Empty(t, pass.Validate(doc))
}

func TestValidate_SharedRouteIsLegal(t *testing.T) {
	t.Parallel()
	mk := func() ir.Operation {
		return ir.Operation{Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
			Method: "GET", URITemplate: "/x", SharedRoute: true,
		}}}}
	}
	doc := validDoc()
	doc.Services = []ir.Service{{
		ID:     "s",
		Groups: []ir.OperationGroup{{Operations: []ir.Operation{mk(), mk()}}},
	}}
	assert.Empty(t, pass.Validate(doc))
}
