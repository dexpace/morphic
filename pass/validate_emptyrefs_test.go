// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestValidate_ReportsAnEmptyRequiredReference pins that an ID-typed position
// holding no ID where its position requires one is reported by Validate as
// ir/empty-<noun>-ref, an error, at the position: here a message binding's
// channel, which Validate used to leave to irverify.
func TestValidate_ReportsAnEmptyRequiredReference(t *testing.T) {
	t.Parallel()
	op := ir.Operation{ID: "operation/x/o"}
	op.Bindings.Message = &ir.MessageBinding{Channel: "", Direction: ir.MessageDirectionSend}

	diags := validated(t, docWithOperation(op))

	require.Equal(t, 1, countCode(t, diags, "ir/empty-channel-ref"))
	for _, diagnostic := range diags {
		if diagnostic.Code == "ir/empty-channel-ref" {
			assert.Equal(t, ir.SeverityError, diagnostic.Severity)
			assert.Equal(t, scenarioOpPath+".Bindings.Message.Channel", diagnostic.Provenance.Node)
		}
	}

	valid := ir.Operation{ID: "operation/x/o"}
	valid.Bindings.Message = &ir.MessageBinding{Channel: "channel/x/c", Direction: ir.MessageDirectionSend}
	doc := docWithOperation(valid)
	doc.Channels = map[ir.ChannelID]ir.Channel{"channel/x/c": {ID: "channel/x/c"}}
	assert.Zero(t, countCode(t, validated(t, doc), "ir/empty-channel-ref"))
}

// TestValidate_OneCodePerEmptyReference pins that a required reference left
// empty is reported under its empty-reference code alone: a binding's
// parameter, an overload's operation and a service's server, none of which
// also draws a second code from a check that reads the same position.
func TestValidate_OneCodePerEmptyReference(t *testing.T) {
	t.Parallel()
	op := paramBindingOp("operation/x/o", ir.Parameter{ID: "parameter/x/o/q", Name: ir.Naming{Source: "q"}})
	op.Bindings.HTTP[0].ParamBindings = append(op.Bindings.HTTP[0].ParamBindings,
		ir.HTTPParamBinding{Location: ir.HTTPLocationQuery})
	op.OverloadOf = new(ir.OperationID)
	doc := docWithOperation(op)
	doc.Services[0].Servers = []ir.ServerID{""}

	diags := validated(t, doc)

	assert.Equal(t, []string{scenarioOpPath + ".Bindings.HTTP[0].ParamBindings[1].Param"},
		nodesOfCode(diags, "ir/empty-parameter-ref"))
	assert.Equal(t, []string{scenarioOpPath + ".OverloadOf"}, nodesOfCode(diags, "ir/empty-operation-ref"))
	assert.Equal(t, []string{"document.Services[0].Servers[0]"}, nodesOfCode(diags, "ir/empty-server-ref"))
	assert.Zero(t, countCode(t, diags, "pass/param-binding-mismatch"))
	assert.Zero(t, countCode(t, diags, "ir/dangling-parameter-ref"))
}

// TestValidate_EmptyMappingValueIsAnEmptyReference pins that a discriminator's
// mapping to no type is an empty type reference and nothing about variant
// membership, which a type naming nothing cannot be held to, and that a mapping
// to a variant is clean.
func TestValidate_EmptyMappingValueIsAnEmptyReference(t *testing.T) {
	t.Parallel()
	union := func(target ir.TypeID) *ir.Document {
		doc := validDoc()
		doc.Types["type/u"] = &ir.Union{
			ID:            "type/u",
			Variants:      []ir.Variant{{Type: ir.TypeRef{Target: "type/m"}}},
			Discriminator: &ir.Discriminator{PropertyName: "k", Mapping: map[string]ir.TypeID{"a": target}},
		}
		return doc
	}

	diags := validated(t, union(""))

	assert.Equal(t, []string{"document.Types[type/u].Discriminator.Mapping[a]"}, nodesOfCode(diags, "ir/empty-type-ref"))
	assert.Zero(t, countCode(t, diags, "pass/discriminator-missing-variant"))

	valid := validated(t, union("type/m"))
	assert.Zero(t, countCode(t, valid, "ir/empty-type-ref"))
	assert.Zero(t, countCode(t, valid, "pass/discriminator-missing-variant"))
}

// TestValidate_DocumentedEmptyPositionsStayLegal pins the positions whose
// comments spell "none" as an empty ID: a discriminator's default, an
// idempotency token parameter, and a tag property PropertyName locates.
func TestValidate_DocumentedEmptyPositionsStayLegal(t *testing.T) {
	t.Parallel()
	op := ir.Operation{ID: "operation/x/o", Idempotency: ir.Idempotency{Kind: ir.IdempotencySafe, TokenParam: ""}}
	doc := docWithOperation(op)
	doc.Types["type/u"] = &ir.Union{
		ID:       "type/u",
		Variants: []ir.Variant{{Type: ir.TypeRef{Target: "type/m"}}},
		Discriminator: &ir.Discriminator{
			PropertyName: "k", Property: "", Default: "", Mapping: map[string]ir.TypeID{"a": "type/m"},
		},
	}

	diags := validated(t, doc)

	for _, diagnostic := range diags {
		assert.NotRegexp(t, `^ir/empty-.*-ref$`, diagnostic.Code, "%s at %s", diagnostic.Message, diagnostic.Provenance.Node)
	}
}

// TestValidate_AnEmptyReferenceDrawsNoSecondCode pins the readers of a required
// ID beside the binding and mapping checks: a message a binding uses, an
// encoding key and a link's parameter, each left empty, draw the empty-reference
// code alone, where each check reading them also reported it under its own.
func TestValidate_AnEmptyReferenceDrawsNoSecondCode(t *testing.T) {
	t.Parallel()
	const op = "document.Services[0].Groups[0].Operations[0]"
	for _, tc := range []struct {
		name  string
		doc   func() *ir.Document
		code  string
		nodes []string
	}{
		{
			name: "a message a binding uses, beside one its channel carries",
			doc: func() *ir.Document {
				operation := ir.Operation{ID: "operation/x/o"}
				operation.Bindings.Message = &ir.MessageBinding{
					Channel: "channel/x/c", Direction: ir.MessageDirectionSend, Messages: []ir.MessageID{"message/x/m", ""},
				}
				doc := docWithOperation(operation)
				doc.Channels = map[ir.ChannelID]ir.Channel{"channel/x/c": {ID: "channel/x/c", Messages: []ir.MessageID{"message/x/m"}}}
				doc.Messages = map[ir.MessageID]ir.Message{"message/x/m": {ID: "message/x/m"}}
				return doc
			},
			code:  "ir/empty-message-ref",
			nodes: []string{op + ".Bindings.Message.Messages[1]"},
		},
		{
			name: "an encoding key",
			doc: func() *ir.Document {
				return docWithOperation(ir.Operation{ID: "operation/x/o", Request: &ir.Payload{Contents: []ir.Content{{
					MediaType: "multipart/form-data", Type: ir.TypeRef{Target: "type/m"},
					Encoding: map[ir.PropertyID]ir.PartEncoding{"": {}},
				}}}})
			},
			code:  "ir/empty-property-ref",
			nodes: []string{op + ".Request.Contents[0].Encoding[].key"},
		},
		{
			name: "a link's parameter, given twice",
			doc: func() *ir.Document {
				return docWithOperation(ir.Operation{
					ID: "operation/x/o", Params: []ir.Parameter{{ID: "parameter/x/o/q", Name: ir.Naming{Source: "q"}}},
					Responses: []ir.Response{{Links: []ir.OperationLink{{
						Name: ir.Naming{Source: "l"}, Operation: "operation/x/o", Params: []ir.LinkParam{{Param: ""}, {Param: ""}},
					}}}},
				})
			},
			code:  "ir/empty-parameter-ref",
			nodes: []string{op + ".Responses[0].Links[0].Params[0].Param", op + ".Responses[0].Links[0].Params[1].Param"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			diags := validated(t, tc.doc())

			assert.Equal(t, tc.nodes, nodesOfCode(diags, tc.code))
			for _, diagnostic := range diags {
				if diagnostic.Code == tc.code || diagnostic.Severity != ir.SeverityError {
					continue
				}
				assert.Failf(t, "a second code", "%s at %s: %s", diagnostic.Code, diagnostic.Provenance.Node, diagnostic.Message)
			}
		})
	}
}

// TestValidate_AnEmptiedBindingLeavesItsParameterUnbound pins what blanking the
// parameter of a binding that bound one draws: its empty reference, an error,
// once, and the warning that the parameter it bound is bound nowhere, a fact
// about another entity that leaving the binding out altogether states too. No
// error reports the empty ID under the binding's own code.
func TestValidate_AnEmptiedBindingLeavesItsParameterUnbound(t *testing.T) {
	t.Parallel()
	unbound := func(diags []ir.Diagnostic) []ir.Diagnostic { return withCode(diags, "pass/param-unbound") }
	op := paramBindingOp("operation/x/o", ir.Parameter{ID: "parameter/x/o/q", Name: ir.Naming{Source: "q"}})
	op.Bindings.HTTP[0].ParamBindings[0].Param = ""
	diags := validated(t, docWithOperation(op))

	assert.Equal(t, []string{scenarioOpPath + ".Bindings.HTTP[0].ParamBindings[0].Param"},
		nodesOfCode(diags, "ir/empty-parameter-ref"))
	got := unbound(diags)
	require.Len(t, got, 1)
	assert.Equal(t, ir.SeverityWarning, got[0].Severity)
	assert.Empty(t, withCode(diags, "pass/param-binding-mismatch"), "an unplaced parameter is no binding mismatch")

	left := paramBindingOp("operation/x/o", ir.Parameter{ID: "parameter/x/o/q", Name: ir.Naming{Source: "q"}})
	left.Bindings.HTTP[0].ParamBindings = nil
	assert.Equal(t, unbound(validated(t, docWithOperation(left))), got,
		"the warning is the one a binding left out draws")
}

// TestValidate_ADiscriminatorLocatingNoTagIsAnEmptyReference pins that a
// discriminator whose Property is empty while neither PropertyName nor Index
// locates the tag is reported as an empty PropertyID reference, an error, at the
// property, in the wording that says what it lacks, and as nothing else; one
// PropertyName locates is clean.
func TestValidate_ADiscriminatorLocatingNoTagIsAnEmptyReference(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["type/m"].(*ir.Model).Discriminator = &ir.Discriminator{}

	diags := validated(t, doc)

	require.Len(t, diags, 1, "the empty reference alone")
	assert.Equal(t, "ir/empty-property-ref", diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, "document.Types[type/m].Discriminator.Property", diags[0].Provenance.Node)
	assert.Contains(t, diags[0].Message, "neither PropertyName nor TupleElementIndex locates the tag")

	located := validDoc()
	located.Types["type/m"].(*ir.Model).Discriminator = &ir.Discriminator{PropertyName: "a"}
	assert.Empty(t, validated(t, located))
}
