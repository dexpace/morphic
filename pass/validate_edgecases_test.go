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
// checkDanglingTypeRefs: a message whose payload content references a missing
// type must be reported against the message location.
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
	var n int
	for _, c := range codes(diags) {
		if c == "ir/dangling-type-ref" {
			n++
		}
	}
	assert.Equal(t, 7, n)
}

// TestValidate_ModelImplementsAndAdditionalPropsWalked covers walkModelRefs'
// Implements branch and walkAdditionalPropsRefs' Key and Patterns branches.
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
	var n int
	for _, c := range codes(diags) {
		if c == "ir/dangling-type-ref" {
			n++
		}
	}
	assert.Equal(t, 4, n)
}

// TestValidate_OperationHeadersAndItemWalked covers the response-headers loop in
// walkOperationRefs and the Item branch in walkPayloadRefs.
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
	got := codes(diags)
	var n int
	for _, c := range got {
		if c == "ir/dangling-type-ref" {
			n++
		}
	}
	// item, header, and error targets are all dangling.
	assert.Equal(t, 3, n)
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
	var n int
	for _, c := range codes(diags) {
		if c == "pass/discriminator-missing-variant" {
			n++
		}
	}
	assert.Equal(t, 2, n)
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

// TestValidate_PerOperationAuthOverride covers the per-operation auth path in
// checkAuthRefs, including the empty-scheme skip and a dangling override scheme.
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
	assert.Contains(t, codes(diags), "pass/dangling-auth-ref")
}

// TestValidate_ExcessiveGroupNestingIsTruncated drives the maxGroupDepth guard in
// forEachGroupOperation: an operation buried below the depth cap is never
// visited, so its one-way violation goes unreported (the recursion stops).
func TestValidate_ExcessiveGroupNestingIsTruncated(t *testing.T) {
	t.Parallel()
	// A one-way op with responses would normally raise oneway-with-responses.
	buried := ir.Operation{ID: "deep", OneWay: true, Responses: []ir.Response{{}}}
	// Nest it 200 levels deep, past maxGroupDepth (128).
	group := ir.OperationGroup{Operations: []ir.Operation{buried}}
	for i := 0; i < 200; i++ {
		group = ir.OperationGroup{Groups: []ir.OperationGroup{group}}
	}
	doc := validDoc()
	doc.Services = []ir.Service{{ID: "s", Groups: []ir.OperationGroup{group}}}
	assert.NotContains(t, codes(pass.Validate(doc)), "pass/oneway-with-responses")
}
