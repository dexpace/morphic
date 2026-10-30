// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
	"github.com/dexpace/morphic/pass"
)

func TestValidate_ArgsOutsideGraphQL(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	m := doc.Types["t/m"].(*ir.Model)
	m.Properties[0].Args = []ir.Parameter{
		{Name: ir.Naming{Source: "first"}, Type: ir.TypeRef{Target: "t/prim/string"}},
	}
	diags := validated(t, doc)
	require.NotEmpty(t, diags)
	assert.Contains(t, irtest.DiagCodes(diags), "pass/args-outside-graphql")
}

// TestValidate_GraphQLReachableTypesAllowArgs drives graphqlReachableTypes: an
// operation with a GraphQL binding makes referenced types reachable (so their
// field arguments are legal), and a self-referential model exercises the
// visited-set skip in the traversal.
func TestValidate_GraphQLReachableTypesAllowArgs(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["t/gql"] = &ir.Model{
		ID: "t/gql",
		Properties: []ir.Property{
			// Self-reference: forces the traversal to re-enqueue an already-seen id.
			{ID: "p/self", Name: ir.Naming{Source: "child"}, WireName: "child",
				Type: ir.TypeRef{Target: "t/gql"}},
			// Field arguments are legal here because the type is GraphQL-reachable.
			{ID: "p/args", Name: ir.Naming{Source: "field"}, WireName: "field",
				Type: ir.TypeRef{Target: "t/prim/string"},
				Args: []ir.Parameter{{Name: ir.Naming{Source: "first"}, Type: ir.TypeRef{Target: "t/prim/string"}}}},
		},
	}
	op := ir.Operation{
		ID:       "q",
		Params:   []ir.Parameter{{Name: ir.Naming{Source: "in"}, Type: ir.TypeRef{Target: "t/gql"}}},
		Bindings: ir.OpBindings{GraphQL: &ir.GraphQLBinding{Kind: "query", FieldPath: []string{"q"}}},
	}
	doc.Services = []ir.Service{{
		ID:     "s",
		Groups: []ir.OperationGroup{{Operations: []ir.Operation{op}}},
	}}
	assert.NotContains(t, irtest.DiagCodes(validated(t, doc)), "pass/args-outside-graphql")
}

// argModel is a model whose one property carries a field argument, so the model
// is legal only where a GraphQL binding reaches it.
func argModel(id ir.TypeID) *ir.Model {
	return &ir.Model{
		ID: id,
		Properties: []ir.Property{{
			ID: ir.PropID("p/" + id), Name: ir.Naming{Source: "field"}, WireName: "field",
			Type: ir.TypeRef{Target: "t/prim/string"},
			Args: []ir.Parameter{{Name: ir.Naming{Source: "since"}, Type: ir.TypeRef{Target: "t/prim/string"}}},
		}},
	}
}

// TestValidate_GraphQLReachabilityFollowsEveryReference drives the three sites a
// GraphQL-legal model can hang off that an operation does not name directly:
// the event type of a subscription's response stream, the type of another
// property's field argument, and a template instantiation argument.
//
// A subscription binds through a GraphQL binding plus streaming fields on the
// core (ir/bindings.go), so its event model is reachable only through
// ResponseStream.Events. Reachability that missed any of these rejected valid IR
// with a severity-error diagnostic, which is fatal (GitHub #51).
func TestValidate_GraphQLReachabilityFollowsEveryReference(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		bind func(doc *ir.Document, op *ir.Operation)
	}{
		{"subscription response stream", func(doc *ir.Document, op *ir.Operation) {
			doc.Types["t/ev"] = argModel("t/ev")
			op.Streaming = ir.StreamingServer
			op.ResponseStream = &ir.StreamDetail{Events: &ir.TypeRef{Target: "t/ev"}}
		}},
		{"nested field-argument type", func(doc *ir.Document, op *ir.Operation) {
			doc.Types["t/inner"] = argModel("t/inner")
			doc.Types["t/outer"] = &ir.Model{
				ID: "t/outer",
				Properties: []ir.Property{{
					ID: "p/outer", Name: ir.Naming{Source: "list"}, WireName: "list",
					Type: ir.TypeRef{Target: "t/prim/string"},
					Args: []ir.Parameter{{Name: ir.Naming{Source: "filter"}, Type: ir.TypeRef{Target: "t/inner"}}},
				}},
			}
			op.Params = []ir.Parameter{{Name: ir.Naming{Source: "in"}, Type: ir.TypeRef{Target: "t/outer"}}}
		}},
		{"template instantiation argument", func(doc *ir.Document, op *ir.Operation) {
			doc.Types["t/arg"] = argModel("t/arg")
			doc.Types["t/page"] = &ir.Model{
				ID: "t/page",
				Instantiation: &ir.TemplateInstantiation{
					Template: "Page",
					Args:     []ir.TemplateArg{{Type: &ir.TypeRef{Target: "t/arg"}}},
				},
			}
			op.Params = []ir.Parameter{{Name: ir.Naming{Source: "in"}, Type: ir.TypeRef{Target: "t/page"}}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := validDoc()
			op := ir.Operation{ID: "op/gql", Bindings: ir.OpBindings{
				GraphQL: &ir.GraphQLBinding{Kind: "subscription", FieldPath: []string{"f"}},
			}}
			tc.bind(doc, &op)
			doc.Services = []ir.Service{{ID: "s", Groups: []ir.OperationGroup{{Operations: []ir.Operation{op}}}}}

			assert.NotContains(t, irtest.DiagCodes(validated(t, doc)), "pass/args-outside-graphql")

			// The other half of the proof: the same document with the binding
			// removed must report, or the case would pass on a reachability walk
			// that reaches nothing at all.
			doc.Services[0].Groups[0].Operations[0].Bindings.GraphQL = nil
			assert.Contains(t, irtest.DiagCodes(validated(t, doc)), "pass/args-outside-graphql")
		})
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

	within := irtest.DiagCodes(validated(t, withArgs(nestGroups(127, gqlOp()))))
	assert.NotContains(t, within, "pass/args-outside-graphql",
		"the binding is visible within the cap, so Args is legal")

	past := irtest.DiagCodes(validated(t, withArgs(nestGroups(200, gqlOp()))))
	assert.NotContains(t, past, "pass/args-outside-graphql",
		"past the cap the binding went unseen; unreachable-by-truncation is not evidence of illegality")
	assert.Contains(t, past, "ir/walk-truncated", "and the truncation must be reported instead")
}

// TestCheckDanglingRefs_DeepValueTreeIsTruncated drives what this pass does with
// a truncated walk: a value tree nested past the shared cap must not be silently
// under-checked, so Validate turns the walk's flag into a diagnostic rather than
// claiming the document is referentially closed. The bound itself is
// ir.MaxWalkDepth's to hold; this is the reporting half.
func TestCheckDanglingRefs_DeepValueTreeIsTruncated(t *testing.T) {
	t.Parallel()
	v := ir.Value{Kind: ir.ValueNull}
	for range ir.MaxWalkDepth {
		v = ir.Value{Kind: ir.ValueList, List: []ir.Value{v}}
	}
	m := &ir.Model{ID: "t/m", Examples: []ir.Example{{Value: &v}}}
	doc := &ir.Document{Types: ir.TypeRegistry{m.ID: m}}

	diags := danglingRefs(t, doc)
	require.NotEmpty(t, diags)
	assert.Equal(t, "ir/walk-truncated", diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
}

// TestAppendTypeIDs_UnmodeledBytesYieldNoReference states the result the byte
// payloads must produce: a preserved JSON blob is opaque data, so bytes that
// happen to spell a type ID are not a reference.
func TestAppendTypeIDs_UnmodeledBytesYieldNoReference(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Unmodeled: ir.Unmodeled{"openapi:x-thing": {
		Reason: ir.ReasonVendorExtension,
		Value:  ir.RawValue(`"t/ghost/in-bytes"`),
	}}}
	assert.Empty(t, pass.AppendTypeIDs(nil, doc))
}
