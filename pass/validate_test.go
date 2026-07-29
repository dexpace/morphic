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

// countCode returns how many of diags carry the given diagnostic code.
func countCode(t *testing.T, diags []ir.Diagnostic, code string) int {
	t.Helper()
	var n int
	for _, d := range diags {
		if d.Code == code {
			n++
		}
	}
	return n
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
	assert.Contains(t, codes(diags), "ir/dangling-auth-ref")
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

// TestValidate_NilTypeDefIsReportedNotPanicked covers every TypeDef kind, not
// just the two whose dereference was observed to crash: a nil guard added only
// where a panic was noticed is scoped to the site rather than the mechanism, and
// any future check that dereferences a matched TypeDef reintroduces the fault.
func TestValidate_NilTypeDefIsReportedNotPanicked(t *testing.T) {
	t.Parallel()
	kinds := map[string]ir.TypeDef{
		"primitive": (*ir.Primitive)(nil),
		"scalar":    (*ir.Scalar)(nil),
		"model":     (*ir.Model)(nil),
		"union":     (*ir.Union)(nil),
		"enum":      (*ir.Enum)(nil),
		"list":      (*ir.List)(nil),
		"map":       (*ir.MapT)(nil),
		"tuple":     (*ir.Tuple)(nil),
		"literal":   (*ir.Literal)(nil),
		"external":  (*ir.External)(nil),
		"any":       (*ir.Any)(nil),
		"untyped":   nil,
	}
	for name, td := range kinds {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := validDoc()
			doc.Types["t/nil"] = td

			var diags []ir.Diagnostic
			require.NotPanics(t, func() { diags = pass.Validate(doc) })

			codes := make([]string, 0, len(diags))
			for _, d := range diags {
				codes = append(codes, d.Code)
			}
			assert.Contains(t, codes, "ir/nil-type",
				"a nil type definition must be reported, not skipped: %v", codes)
		})
	}
}

// nestGroups buries op under depth levels of operation groups.
func nestGroups(depth int, op ir.Operation) *ir.Document {
	g := ir.OperationGroup{Operations: []ir.Operation{op}}
	for range depth {
		g = ir.OperationGroup{Groups: []ir.OperationGroup{g}}
	}
	doc := validDoc()
	doc.Services = []ir.Service{{ID: "s", Groups: []ir.OperationGroup{g}}}
	return doc
}

// TestValidate_GroupWalkTruncationIsReported pins the depth bound itself: a walk
// that stopped early must say so. Without it every check reaching operations
// walks a subset while Validate still reports the document consistent.
func TestValidate_GroupWalkTruncationIsReported(t *testing.T) {
	t.Parallel()
	oneway := ir.Operation{ID: "deep", OneWay: true, Responses: []ir.Response{{}}}

	within := codes(pass.Validate(nestGroups(127, oneway)))
	assert.Contains(t, within, "pass/oneway-with-responses")
	assert.NotContains(t, within, "ir/walk-truncated", "a walk that completed must not claim truncation")

	past := codes(pass.Validate(nestGroups(200, oneway)))
	assert.Contains(t, past, "ir/walk-truncated", "a walk that stopped early must say so")
}

// TestValidate_ArgsCheckFailsOpenWhenWalkTruncated covers the polarity flip: the
// legality of Property.Args is decided from a reachability set built by the same
// bounded walk, so past the cap a GraphQL operation goes unseen and the types it
// reaches look unreachable. Accusing them reports a defect that is not there.
func TestValidate_ArgsCheckFailsOpenWhenWalkTruncated(t *testing.T) {
	t.Parallel()
	gqlOp := func() ir.Operation {
		return ir.Operation{
			ID:       "gql",
			Bindings: ir.OpBindings{GraphQL: &ir.GraphQLBinding{}},
			Params:   []ir.Parameter{{Name: ir.Naming{Source: "in"}, Type: ir.TypeRef{Target: "t/m"}}},
		}
	}
	withArgs := func(doc *ir.Document) *ir.Document {
		m, ok := doc.Types["t/m"].(*ir.Model)
		require.True(t, ok)
		m.Properties[0].Args = []ir.Parameter{
			{Name: ir.Naming{Source: "first"}, Type: ir.TypeRef{Target: "t/prim/string"}},
		}
		return doc
	}

	within := codes(pass.Validate(withArgs(nestGroups(127, gqlOp()))))
	assert.NotContains(t, within, "pass/args-outside-graphql",
		"the binding is visible within the cap, so Args is legal")

	past := codes(pass.Validate(withArgs(nestGroups(200, gqlOp()))))
	assert.NotContains(t, past, "pass/args-outside-graphql",
		"past the cap the binding went unseen; unreachable-by-truncation is not evidence of illegality")
	assert.Contains(t, past, "ir/walk-truncated", "and the truncation must be reported instead")
}
