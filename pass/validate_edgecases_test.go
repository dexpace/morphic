package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

func TestValidate_NilDocumentReturnsNil(t *testing.T) {
	t.Parallel()
	assert.Nil(t, pass.Validate(nil))
}

// TestValidate_DanglingRefInMessagePayload exercises the Messages walk in
// checkDanglingRefs: a message whose payload content references a missing type
// must be reported against the message location.
func TestValidate_DanglingRefInMessagePayload(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Messages = map[ir.MessageID]ir.Message{
		"msg/a": {
			ID: "msg/a",
			Payload: ir.Payload{Contents: []ir.Content{
				{MediaType: "application/json", Type: ir.TypeRef{Target: "t/ghost"}},
			}},
		},
	}
	diags := pass.Validate(doc)
	require.NotEmpty(t, diags)
	assert.Contains(t, codes(diags), "ir/dangling-type-ref")
}

// TestValidate_DanglingRefsAcrossContainerKinds drives every container TypeDef
// walker branch (Scalar base/encoding, List elem, MapT key+value, Tuple elems)
// with a dangling target so each visit fires.
func TestValidate_DanglingRefsAcrossContainerKinds(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["t/scalar"] = &ir.Scalar{
		TypeCommon: ir.TypeCommon{ID: "t/scalar"},
		Base:       &ir.TypeRef{Target: "t/ghost-base"},
		Encoding:   &ir.Encoding{Name: "x", WireType: &ir.TypeRef{Target: "t/ghost-wire"}},
	}
	doc.Types["t/list"] = &ir.List{
		TypeCommon: ir.TypeCommon{ID: "t/list"},
		Elem:       ir.TypeRef{Target: "t/ghost-elem"},
		Encoding:   &ir.Encoding{Name: "packed", WireType: &ir.TypeRef{Target: "t/ghost-lwire"}},
	}
	doc.Types["t/map"] = &ir.MapT{
		TypeCommon: ir.TypeCommon{ID: "t/map"},
		Key:        ir.TypeRef{Target: "t/ghost-key"},
		Value:      ir.TypeRef{Target: "t/ghost-val"},
	}
	doc.Types["t/tuple"] = &ir.Tuple{
		TypeCommon: ir.TypeCommon{ID: "t/tuple"},
		Elems:      []ir.TypeRef{{Target: "t/ghost-e0"}},
	}
	diags := pass.Validate(doc)
	// Each dangling target above yields exactly one dangling-type-ref diagnostic.
	assert.Equal(t, 7, countCode(t, diags, "ir/dangling-type-ref"))
}

// TestValidate_ModelImplementsAndAdditionalPropsWalked covers a model's
// Implements references and the Value, Key and Patterns references its
// AdditionalProps carries.
func TestValidate_ModelImplementsAndAdditionalPropsWalked(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	m := doc.Types["t/m"].(*ir.Model)
	m.Implements = []ir.TypeRef{{Target: "t/ghost-iface"}}
	m.AdditionalProps = &ir.AdditionalProps{
		Value: ir.TypeRef{Target: "t/ghost-apval"},
		Key:   &ir.TypeRef{Target: "t/ghost-apkey"},
		Patterns: []ir.PatternProps{
			{Pattern: "^x", Value: ir.TypeRef{Target: "t/ghost-appat"}},
		},
	}
	diags := pass.Validate(doc)
	assert.Equal(t, 4, countCode(t, diags, "ir/dangling-type-ref"))
}

// TestValidate_OperationHeadersAndItemWalked covers an operation's response
// headers and the per-media-type Item a content declares.
func TestValidate_OperationHeadersAndItemWalked(t *testing.T) {
	t.Parallel()
	op := ir.Operation{
		ID: "op",
		Request: &ir.Payload{Contents: []ir.Content{{
			MediaType: "application/json",
			Type:      ir.TypeRef{Target: "t/prim/string"},
			Item:      &ir.TypeRef{Target: "t/ghost-item"},
		}}},
		Responses: []ir.Response{{
			Payload: &ir.Payload{Contents: []ir.Content{{Type: ir.TypeRef{Target: "t/prim/string"}}}},
			Headers: []ir.Property{{
				ID: "p/h", Name: ir.Naming{Source: "X-Trace"}, WireName: "X-Trace",
				Type: ir.TypeRef{Target: "t/ghost-hdr"},
			}},
		}},
		Errors: []ir.ErrorCase{{Type: ir.TypeRef{Target: "t/ghost-err"}}},
	}
	diags := pass.Validate(docWithOperation(op))
	// item, header, and error targets are all dangling.
	assert.Equal(t, 3, countCode(t, diags, "ir/dangling-type-ref"))
}

// TestValidate_ModelDiscriminator drives checkModelDiscriminator and every
// isSubtype branch: subtype via Base, subtype via Implements, a non-model target,
// and a model that is neither, plus the clean (valid) mapping path.
func TestValidate_ModelDiscriminator(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	base := &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/base"}, Abstract: true}
	doc.Types["t/base"] = base
	// Legal subtype via Base.
	doc.Types["t/viaBase"] = &ir.Model{
		TypeCommon: ir.TypeCommon{ID: "t/viaBase"}, Base: &ir.TypeRef{Target: "t/base"},
	}
	// Legal subtype via Implements.
	doc.Types["t/viaImpl"] = &ir.Model{
		TypeCommon: ir.TypeCommon{ID: "t/viaImpl"}, Implements: []ir.TypeRef{{Target: "t/base"}},
	}
	// Model that is neither a subtype nor implementer of base.
	doc.Types["t/unrelated"] = &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/unrelated"}}

	base.Discriminator = &ir.Discriminator{
		PropertyName: "kind",
		Mapping: map[string]ir.TypeID{
			"a": "t/viaBase",     // valid via Base
			"b": "t/viaImpl",     // valid via Implements
			"c": "t/prim/string", // not a model -> invalid
			"d": "t/unrelated",   // model but not a subtype -> invalid
		},
	}
	diags := pass.Validate(doc)
	assert.Equal(t, 2, countCode(t, diags, "pass/discriminator-missing-variant"))
}

// TestValidate_SubtypeThroughAliasChain pins that a subtype composing an alias
// of the base is still a subtype. A `$ref` carrying siblings composes the
// branch's own node (ir-design §4.3), so a hierarchy written that way names its
// base one hop away — two hops where the alias itself was reached through
// another, which a single dereference would still miss.
//
// The cyclic chain is here because the IR permits one and the walk must
// terminate on it: the alias resolves to no base, so the mapping is reported
// rather than hanging the pass.
func TestValidate_SubtypeThroughAliasChain(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["t/base"] = &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/base"}, Abstract: true}
	doc.Types["t/alias"] = &ir.Scalar{
		TypeCommon: ir.TypeCommon{ID: "t/alias"}, Base: &ir.TypeRef{Target: "t/aliasOfAlias"},
	}
	doc.Types["t/aliasOfAlias"] = &ir.Scalar{
		TypeCommon: ir.TypeCommon{ID: "t/aliasOfAlias"}, Base: &ir.TypeRef{Target: "t/base"},
	}
	doc.Types["t/sub"] = &ir.Model{
		TypeCommon: ir.TypeCommon{ID: "t/sub"}, Base: &ir.TypeRef{Target: "t/alias"},
	}
	// A two-node alias cycle: following it must stop, not recurse forever.
	doc.Types["t/loopA"] = &ir.Scalar{
		TypeCommon: ir.TypeCommon{ID: "t/loopA"}, Base: &ir.TypeRef{Target: "t/loopB"},
	}
	doc.Types["t/loopB"] = &ir.Scalar{
		TypeCommon: ir.TypeCommon{ID: "t/loopB"}, Base: &ir.TypeRef{Target: "t/loopA"},
	}
	doc.Types["t/looped"] = &ir.Model{
		TypeCommon: ir.TypeCommon{ID: "t/looped"}, Base: &ir.TypeRef{Target: "t/loopA"},
	}
	doc.Types["t/base"].(*ir.Model).Discriminator = &ir.Discriminator{
		PropertyName: "kind",
		Mapping:      map[string]ir.TypeID{"a": "t/sub", "b": "t/looped"},
	}

	diags := pass.Validate(doc)
	assert.Equal(t, 1, countCode(t, diags, "pass/discriminator-missing-variant"),
		"only the cyclic alias fails to reach the base: %v", codes(diags))
	assert.Contains(t, messageForCode(t, diags, "pass/discriminator-missing-variant"), "t/looped")
}

// discriminatedUnion returns a document whose union declares t/m as its only
// variant and maps the wire value "a" onto target.
func discriminatedUnion(target ir.TypeID) *ir.Document {
	doc := validDoc()
	doc.Types["t/u"] = &ir.Union{
		TypeCommon: ir.TypeCommon{ID: "t/u"},
		Variants:   []ir.Variant{{Type: ir.TypeRef{Target: "t/m"}}},
		Discriminator: &ir.Discriminator{
			PropertyName: "kind",
			Mapping:      map[string]ir.TypeID{"a": target},
		},
	}
	return doc
}

// messageForCode returns the message of the single diagnostic carrying code.
func messageForCode(t *testing.T, diags []ir.Diagnostic, code string) string {
	t.Helper()
	require.Equal(t, 1, countCode(t, diags, code), "expected exactly one %s", code)
	for _, d := range diags {
		if d.Code == code {
			return d.Message
		}
	}
	return ""
}

// TestValidate_DanglingDiscriminatorTargetIsReportedTwice pins the deliberate
// double report. A mapping target no type declares breaks two separate
// guarantees, so both codes fire and each says its own thing: the reference is
// not closed, and the discriminator cannot route that wire value. Collapsing to
// one code would make an emitter reading discriminator completeness subscribe to
// a reference-integrity code to learn about its own defect.
func TestValidate_DanglingDiscriminatorTargetIsReportedTwice(t *testing.T) {
	t.Parallel()
	diags := pass.Validate(discriminatedUnion("t/ghost"))

	assert.Equal(t, 1, countCode(t, diags, "ir/dangling-type-ref"))
	assert.Contains(t, messageForCode(t, diags, "pass/discriminator-missing-variant"),
		"no type in the document declares",
		"the mapping diagnostic must name the cause it found, not the one it did not")
}

// TestValidate_DeclaredNonVariantIsReportedOnce is the other half: a target that
// resolves leaves the document referentially closed, so only the discriminator's
// own code fires — the double report above tracks the dangling reference, not the
// discriminator check firing twice.
func TestValidate_DeclaredNonVariantIsReportedOnce(t *testing.T) {
	t.Parallel()
	diags := pass.Validate(discriminatedUnion("t/prim/string"))

	assert.Equal(t, 0, countCode(t, diags, "ir/dangling-type-ref"))
	assert.Contains(t, messageForCode(t, diags, "pass/discriminator-missing-variant"),
		"is not a variant of it")
}

// TestValidate_EmptyEffectiveWireNameIsSkipped covers effectiveWireName's
// source-name fallback and the empty-name continue in checkDuplicateWireNames.
func TestValidate_EmptyEffectiveWireNameIsSkipped(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["t/blank"] = &ir.Model{
		TypeCommon: ir.TypeCommon{ID: "t/blank"},
		Properties: []ir.Property{
			// No WireName, no Source name -> effective name "" -> skipped.
			{ID: "p/a", Type: ir.TypeRef{Target: "t/prim/string"}},
			{ID: "p/b", Type: ir.TypeRef{Target: "t/prim/string"}},
			// No WireName but a Source name -> effectiveWireName returns Source.
			{ID: "p/c", Name: ir.Naming{Source: "named"}, Type: ir.TypeRef{Target: "t/prim/string"}},
		},
	}
	// Two blank-named properties must NOT collide (both skipped), and the
	// source-named one is unique, so no duplicate-wire-name diagnostic fires.
	assert.NotContains(t, codes(pass.Validate(doc)), "pass/duplicate-wire-name")
}

// TestValidate_HostParamBoundMultipleTimesIsLegal covers the host-location
// continue: a host label may be filled by a param several times without a
// double-bind error.
func TestValidate_HostParamBoundMultipleTimesIsLegal(t *testing.T) {
	t.Parallel()
	op := ir.Operation{
		ID:     "op",
		Params: []ir.Parameter{{Name: ir.Naming{Source: "region"}, Type: ir.TypeRef{Target: "t/prim/string"}}},
		Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
			Method: "GET", URITemplate: "/x", HostPrefix: "{region}.{region}",
			ParamBindings: []ir.HTTPParamBinding{
				{Param: "region", Location: ir.HTTPLocationHost},
				{Param: "region", Location: ir.HTTPLocationHost},
			},
		}}},
	}
	assert.NotContains(t, codes(pass.Validate(docWithOperation(op))), "pass/param-binding-mismatch")
}

// TestValidate_GraphQLReachableTypesAllowArgs drives graphqlReachableTypes: an
// operation with a GraphQL binding makes referenced types reachable (so their
// field arguments are legal), and a self-referential model exercises the
// visited-set skip in the traversal.
func TestValidate_GraphQLReachableTypesAllowArgs(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["t/gql"] = &ir.Model{
		TypeCommon: ir.TypeCommon{ID: "t/gql"},
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
	assert.NotContains(t, codes(pass.Validate(doc)), "pass/args-outside-graphql")
}

// argModel is a model whose one property carries a field argument, so the model
// is legal only where a GraphQL binding reaches it.
func argModel(id ir.TypeID) *ir.Model {
	return &ir.Model{
		TypeCommon: ir.TypeCommon{ID: id},
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
				TypeCommon: ir.TypeCommon{ID: "t/outer"},
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
			doc.Types["t/page"] = &ir.Model{TypeCommon: ir.TypeCommon{
				ID: "t/page",
				Instantiation: &ir.TemplateInstantiation{
					Template: "Page",
					Args:     []ir.TemplateArg{{Type: &ir.TypeRef{Target: "t/arg"}}},
				},
			}}
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

			assert.NotContains(t, codes(pass.Validate(doc)), "pass/args-outside-graphql")

			// The other half of the proof: the same document with the binding
			// removed must report, or the case would pass on a reachability walk
			// that reaches nothing at all.
			doc.Services[0].Groups[0].Operations[0].Bindings.GraphQL = nil
			assert.Contains(t, codes(pass.Validate(doc)), "pass/args-outside-graphql")
		})
	}
}

// TestValidate_PerOperationAuthOverride covers a per-operation auth override,
// including the empty-scheme skip and a dangling override scheme.
func TestValidate_PerOperationAuthOverride(t *testing.T) {
	t.Parallel()
	op := ir.Operation{
		ID: "op",
		Auth: []ir.AuthRequirement{{Schemes: []ir.SchemeUse{
			{Scheme: ""},          // empty scheme is skipped
			{Scheme: "auth/nope"}, // dangling override
		}}},
	}
	diags := pass.Validate(docWithOperation(op))
	require.NotEmpty(t, diags)
	assert.Contains(t, codes(diags), "ir/dangling-auth-ref")
}

// TestValidate_ExcessiveGroupNestingIsTruncated drives the maxGroupDepth guard in
// forEachGroupOperation: an operation buried below the depth cap is never
// visited, so its one-way violation goes unreported (the recursion stops). The
// truncation itself is reported instead — see
// TestValidate_GroupWalkTruncationIsReported.
func TestValidate_ExcessiveGroupNestingIsTruncated(t *testing.T) {
	t.Parallel()
	// A one-way op with responses would normally raise oneway-with-responses.
	buried := ir.Operation{ID: "deep", OneWay: true, Responses: []ir.Response{{}}}
	// Nest it 200 levels deep, past maxGroupDepth (128).
	group := ir.OperationGroup{Operations: []ir.Operation{buried}}
	for range 200 {
		group = ir.OperationGroup{Groups: []ir.OperationGroup{group}}
	}
	doc := validDoc()
	doc.Services = []ir.Service{{ID: "s", Groups: []ir.OperationGroup{group}}}
	assert.NotContains(t, codes(pass.Validate(doc)), "pass/oneway-with-responses")
}
