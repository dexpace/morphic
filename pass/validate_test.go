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
		Params: []ir.Parameter{{ID: "param/x/id", Name: ir.Naming{Source: "id"}, Type: ir.TypeRef{Target: "t/prim/string"}}},
		Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
			Method: "GET", URITemplate: "/x",
			ParamBindings: []ir.HTTPParamBinding{{Param: "param/x/ghost", Location: ir.HTTPLocationPath}},
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
		Params: []ir.Parameter{{ID: "param/x/id", Name: ir.Naming{Source: "id"}, Type: ir.TypeRef{Target: "t/prim/string"}}},
		Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
			Method: "GET", URITemplate: "/x",
			ParamBindings: []ir.HTTPParamBinding{
				{Param: "param/x/id", Location: ir.HTTPLocationQuery},
				{Param: "param/x/id", Location: ir.HTTPLocationQuery},
			},
		}}},
	}
	diags := pass.Validate(docWithOperation(op))
	require.NotEmpty(t, diags)
	assert.Contains(t, codes(diags), "pass/param-binding-mismatch")
}

// TestValidate_ParamBinding_SameNameDifferentLocation is the shape the join used
// to alias: two parameters named id, one in the query and one in the path. Each
// binding names one by ID, so the pair is clean, and swapping a binding onto the
// other parameter's ID binds one twice and leaves the other unbound.
func TestValidate_ParamBinding_SameNameDifferentLocation(t *testing.T) {
	t.Parallel()
	op := func(first, second ir.ParamID) ir.Operation {
		return ir.Operation{
			ID: "op/a",
			Params: []ir.Parameter{
				{ID: "param/x/a/id/query", Name: ir.Naming{Source: "id"}, Type: ir.TypeRef{Target: "t/prim/integer"}},
				{ID: "param/x/a/id/path", Name: ir.Naming{Source: "id"}, Required: true, Type: ir.TypeRef{Target: "t/prim/string"}},
			},
			Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
				Method: "GET", URITemplate: "/x/{id}",
				ParamBindings: []ir.HTTPParamBinding{
					{Param: first, Location: ir.HTTPLocationQuery, WireName: "id"},
					{Param: second, Location: ir.HTTPLocationQuery, WireName: "id"},
				},
			}}},
		}
	}
	good := op("param/x/a/id/query", "param/x/a/id/path")
	good.Bindings.HTTP[0].ParamBindings[1].Location = ir.HTTPLocationPath
	assert.NotContains(t, codes(pass.Validate(docWithOperation(good))), "pass/param-binding-mismatch")

	swapped := op("param/x/a/id/path", "param/x/a/id/path")
	var msgs []string
	for _, d := range pass.Validate(docWithOperation(swapped)) {
		if d.Code == "pass/param-binding-mismatch" {
			msgs = append(msgs, d.Message)
		}
	}
	require.Len(t, msgs, 2, "one double-bind error and one unbound warning")
}

// TestValidate_ParamBinding_OtherOperationsParamIsRejected holds membership to
// the binding's own operation: an ID another operation declares still resolves
// document-wide, so only the per-operation join can reject it.
func TestValidate_ParamBinding_OtherOperationsParamIsRejected(t *testing.T) {
	t.Parallel()
	mk := func(id string, bound ir.ParamID) ir.Operation {
		return ir.Operation{
			ID:     ir.OpID(id),
			Params: []ir.Parameter{{ID: ir.ParamID("param/x/" + id), Name: ir.Naming{Source: "id"}, Type: ir.TypeRef{Target: "t/prim/string"}}},
			Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
				Method: "GET", URITemplate: "/x",
				ParamBindings: []ir.HTTPParamBinding{{Param: bound, Location: ir.HTTPLocationQuery}},
			}}},
		}
	}
	doc := docWithOperation(mk("op/a", "param/x/op/b"))
	doc.Services[0].Groups[0].Operations = append(doc.Services[0].Groups[0].Operations, mk("op/b", "param/x/op/b"))
	var errs []string
	for _, d := range pass.Validate(doc) {
		if d.Code == "pass/param-binding-mismatch" && d.Severity == ir.SeverityError {
			errs = append(errs, d.Message)
		}
	}
	require.Len(t, errs, 1, "op/a binds op/b's parameter")
	assert.Contains(t, errs[0], "does not declare")
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

// groupsDoc wraps top-level groups in two services so a duplicate can span them.
func groupsDoc(first []ir.OperationGroup, second []ir.OperationGroup) *ir.Document {
	doc := validDoc()
	doc.Services = []ir.Service{{ID: "s/a", Groups: first}, {ID: "s/b", Groups: second}}
	return doc
}

func TestValidate_DuplicateGroupIDIsReported(t *testing.T) {
	t.Parallel()
	same := ir.OperationGroup{ID: "g/openapi/tags/pets"}
	nested := ir.OperationGroup{ID: "g/other", Groups: []ir.OperationGroup{{ID: "g/openapi/tags/pets"}}}

	tests := map[string]*ir.Document{
		"across services": groupsDoc([]ir.OperationGroup{same}, []ir.OperationGroup{same}),
		"within a tree":   groupsDoc([]ir.OperationGroup{same, nested}, nil),
	}
	for name, doc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			diags := pass.Validate(doc)
			assert.Equal(t, 1, countCode(t, diags, "pass/duplicate-group-id"), "%v", codes(diags))
			for _, d := range diags {
				assert.Equal(t, ir.SeverityError, d.Severity)
			}
		})
	}
}

func TestValidate_DistinctOrEmptyGroupIDsAreClean(t *testing.T) {
	t.Parallel()
	doc := groupsDoc(
		[]ir.OperationGroup{{ID: "g/openapi/tags/a"}, {}, {}},
		[]ir.OperationGroup{{ID: "g/openapi/tags/b"}})
	assert.Zero(t, countCode(t, pass.Validate(doc), "pass/duplicate-group-id"))
}

// TestValidate_DuplicateGroupIDStopsAtTheDepthBound pins that the check shares
// the group-depth bound: a repeat buried past it is not reached, and the
// truncation is what says so.
func TestValidate_DuplicateGroupIDStopsAtTheDepthBound(t *testing.T) {
	t.Parallel()
	g := ir.OperationGroup{ID: "g/deep"}
	for range 200 {
		g = ir.OperationGroup{ID: "g/a", Groups: []ir.OperationGroup{g}}
	}
	got := codes(pass.Validate(groupsDoc([]ir.OperationGroup{g}, nil)))
	assert.Contains(t, got, "ir/walk-truncated")
	assert.Contains(t, got, "pass/duplicate-group-id", "the repeated g/a inside the bound is still found")
}
