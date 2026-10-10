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
			"t/prim/string": &ir.Primitive{ID: "t/prim/string", Prim: "string"},
			"t/m": &ir.Model{ID: "t/m", Properties: []ir.Property{
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
		ID:       "t/u",
		Variants: []ir.Variant{{Type: ir.TypeRef{Target: "t/m"}}},
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
		Params: []ir.Parameter{{ID: "param/x/op/id/path", Name: ir.Naming{Source: "id"}, Type: ir.TypeRef{Target: "t/prim/string"}}},
		Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
			Method: "GET", URITemplate: "/x",
			ParamBindings: []ir.HTTPParamBinding{{Param: "param/x/op/ghost/path", Location: ir.HTTPLocationPath}},
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
		Params: []ir.Parameter{{ID: "param/x/op/id/query", Name: ir.Naming{Source: "id"}, Type: ir.TypeRef{Target: "t/prim/string"}}},
		Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
			Method: "GET", URITemplate: "/x",
			ParamBindings: []ir.HTTPParamBinding{
				{Param: "param/x/op/id/query", Location: ir.HTTPLocationQuery},
				{Param: "param/x/op/id/query", Location: ir.HTTPLocationQuery},
			},
		}}},
	}
	diags := pass.Validate(docWithOperation(op))
	require.NotEmpty(t, diags)
	assert.Contains(t, codes(diags), "pass/param-binding-mismatch")
}

// paramBindingOp returns an operation declaring the given parameters and
// binding each one once, in the query.
func paramBindingOp(id ir.OpID, params ...ir.Parameter) ir.Operation {
	op := ir.Operation{ID: id, Params: params}
	bindings := make([]ir.HTTPParamBinding, 0, len(params))
	for _, p := range params {
		bindings = append(bindings, ir.HTTPParamBinding{Param: p.ID, Location: ir.HTTPLocationQuery})
	}
	op.Bindings = ir.OpBindings{HTTP: []ir.HTTPBinding{{Method: "GET", URITemplate: "/x", ParamBindings: bindings}}}
	return op
}

// TestValidate_ParamBinding_SameNameDifferentLocation pins that a parameter is
// identified by its ID: id in the query and id in the path are two parameters,
// each bound once, so neither binding is a duplicate of the other.
func TestValidate_ParamBinding_SameNameDifferentLocation(t *testing.T) {
	t.Parallel()
	op := paramBindingOp("op/x/get",
		ir.Parameter{ID: "param/x/get/id/query", Name: ir.Naming{Source: "id"}},
		ir.Parameter{ID: "param/x/get/id/path", Name: ir.Naming{Source: "id"}},
	)
	op.Bindings.HTTP[0].ParamBindings[1].Location = ir.HTTPLocationPath
	assert.NotContains(t, codes(pass.Validate(docWithOperation(op))), "pass/param-binding-mismatch")
}

// TestValidate_ParamBinding_OtherOperationsParamIsRejected pins that a binding
// names a parameter of its own operation: another operation's parameter is a
// well-formed reference, so only this check can say it is the wrong one.
func TestValidate_ParamBinding_OtherOperationsParamIsRejected(t *testing.T) {
	t.Parallel()
	get := paramBindingOp("op/x/get", ir.Parameter{ID: "param/x/get/id/query", Name: ir.Naming{Source: "id"}})
	post := paramBindingOp("op/x/post", ir.Parameter{ID: "param/x/post/id/query", Name: ir.Naming{Source: "id"}})
	post.Bindings.HTTP[0].ParamBindings[0].Param = "param/x/get/id/query"

	doc := validDoc()
	doc.Services = []ir.Service{{ID: "s", Groups: []ir.OperationGroup{{Operations: []ir.Operation{get, post}}}}}
	diags := pass.Validate(doc)

	var mismatches []string
	for _, d := range diags {
		if d.Code == "pass/param-binding-mismatch" {
			mismatches = append(mismatches, d.Message)
		}
	}
	assert.Len(t, mismatches, 2, "the foreign binding, and the parameter post then leaves unbound: %v", mismatches)
	assert.Contains(t, mismatches[0], "does not declare")
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
		ID:        "t/e",
		ValueType: ir.PrimString,
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

	// Pinned at the boundary, not near it. Every leaf recurses once into its own
	// empty Groups, so a cap reported without checking whether anything was
	// actually skipped fires at exactly the depth where the last operation still
	// fits — visible only by testing maxGroupDepth itself.
	for _, depth := range []int{1, 127, 128} {
		within := codes(pass.Validate(nestGroups(depth, oneway)))
		assert.Contains(t, within, "pass/oneway-with-responses",
			"at depth %d the operation is still reached", depth)
		assert.NotContains(t, within, "ir/walk-truncated",
			"at depth %d nothing was skipped, so nothing may claim truncation", depth)
	}

	for _, depth := range []int{129, 200} {
		past := codes(pass.Validate(nestGroups(depth, oneway)))
		assert.Contains(t, past, "ir/walk-truncated",
			"at depth %d the walk stopped early and must say so", depth)
		assert.NotContains(t, past, "pass/oneway-with-responses",
			"and the operation past the cap genuinely went unvisited")
	}
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

// paramRefOp returns an operation declaring one parameter and pointing its
// pagination cursor, limit and idempotency token at the given IDs.
func paramRefOp(id ir.OpID, own ir.ParamID, cursor, limit, token ir.ParamID) ir.Operation {
	return ir.Operation{
		ID:     id,
		Params: []ir.Parameter{{ID: own, Name: ir.Naming{Source: "p"}}},
		Pagination: &ir.Pagination{
			InputCursor: &ir.ParamPath{Param: cursor},
			InputLimit:  &ir.ParamPath{Param: limit},
		},
		Idempotency: ir.Idempotency{Kind: ir.IdempotencyToken, TokenParam: token},
	}
}

// docWithOperations wraps ops in one service and group.
func docWithOperations(ops ...ir.Operation) *ir.Document {
	doc := validDoc()
	doc.Services = []ir.Service{{ID: "s", Groups: []ir.OperationGroup{{Operations: ops}}}}
	return doc
}

func TestValidate_ParamReferences_OwnOperationIsClean(t *testing.T) {
	t.Parallel()
	op := paramRefOp("op/x/get", "param/x/get/p", "param/x/get/p", "param/x/get/p", "param/x/get/p")
	diags := pass.Validate(docWithOperations(op))
	assert.NotContains(t, codes(diags), "pass/param-binding-mismatch")
	assert.NotContains(t, codes(diags), "ir/dangling-param-ref")
}

func TestValidate_ParamReferences_OtherOperationsParamIsRejected(t *testing.T) {
	t.Parallel()
	get := paramRefOp("op/x/get", "param/x/get/p", "param/x/get/p", "param/x/get/p", "param/x/get/p")
	post := paramRefOp("op/x/post", "param/x/post/p", "param/x/get/p", "param/x/post/p", "param/x/get/p")

	diags := pass.Validate(docWithOperations(get, post))

	var at []string
	for _, d := range diags {
		if d.Code == "pass/param-binding-mismatch" {
			at = append(at, d.Provenance.Node)
		}
	}
	assert.Equal(t, []string{"op/x/post/pagination/inputCursor", "op/x/post/idempotency/tokenParam"}, at)
}

func TestValidate_ParamReferences_UndeclaredIDIsReportedOnceAsDangling(t *testing.T) {
	t.Parallel()
	op := paramRefOp("op/x/get", "param/x/get/p", "param/x/ghost", "param/x/get/p", "param/x/get/p")
	diags := pass.Validate(docWithOperations(op))
	assert.Equal(t, 1, countCode(t, diags, "ir/dangling-param-ref"))
	assert.Zero(t, countCode(t, diags, "pass/param-binding-mismatch"), "the dangling walk owns an ID nothing declares")
}

func TestValidate_ParamReferences_EmptyTokenParamWithoutTokenKindIsClean(t *testing.T) {
	t.Parallel()
	op := paramRefOp("op/x/get", "param/x/get/p", "param/x/get/p", "param/x/get/p", "")
	op.Idempotency = ir.Idempotency{Kind: ir.IdempotencySafe}
	// Another operation holds a parameter with no ID, so "" is a declared ID
	// somewhere and only the empty-ID rule keeps the empty token from naming it.
	unidentified := ir.Operation{ID: "op/x/other", Params: []ir.Parameter{{Name: ir.Naming{Source: "q"}}}}
	diags := pass.Validate(docWithOperations(op, unidentified))
	assert.Zero(t, countCode(t, diags, "pass/param-binding-mismatch"))
	assert.Zero(t, countCode(t, diags, "ir/dangling-param-ref"))
}

// TestValidate_DuplicateEnumMemberIDIsAnError pins that two members of one enum
// may not share an ID, and that the same ID in two enums is no repeat: an
// emitter keys collision resolution by it within an enum.
func TestValidate_DuplicateEnumMemberIDIsAnError(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["t/e"] = &ir.Enum{ID: "t/e", ValueType: ir.PrimString, Closed: true, Members: []ir.EnumMember{
		{ID: "e/x/e/s:a", Value: ir.Value{Kind: ir.ValueString, Str: "a"}},
		{ID: "e/x/e/s:b", Value: ir.Value{Kind: ir.ValueString, Str: "b"}},
		{ID: "e/x/e/s:a", Value: ir.Value{Kind: ir.ValueString, Str: "a"}},
		{Value: ir.Value{Kind: ir.ValueString, Str: "c"}},
		{Value: ir.Value{Kind: ir.ValueString, Str: "d"}},
	}}
	doc.Types["t/f"] = &ir.Enum{ID: "t/f", ValueType: ir.PrimString, Closed: true, Members: []ir.EnumMember{
		{ID: "e/x/e/s:a", Value: ir.Value{Kind: ir.ValueString, Str: "a"}},
	}}

	var at []string
	for _, d := range pass.Validate(doc) {
		if d.Code == "pass/duplicate-enum-member-id" {
			assert.Equal(t, ir.SeverityError, d.Severity)
			at = append(at, d.Provenance.Node)
		}
	}
	assert.Equal(t, []string{"t/e/members/2"}, at)
}
