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
	op := ir.Operation{ID: "op/x/o"}
	op.Bindings.Message = &ir.MessageBinding{Channel: "", Direction: ir.MsgDirectionSend}

	diags := validated(t, docWithOperation(op))

	require.Equal(t, 1, countCode(t, diags, "ir/empty-channel-ref"))
	for _, d := range diags {
		if d.Code == "ir/empty-channel-ref" {
			assert.Equal(t, ir.SeverityError, d.Severity)
			assert.Equal(t, scenarioOpPath+".Bindings.Message.Channel", d.Provenance.Node)
		}
	}

	valid := ir.Operation{ID: "op/x/o"}
	valid.Bindings.Message = &ir.MessageBinding{Channel: "chan/x/c", Direction: ir.MsgDirectionSend}
	doc := docWithOperation(valid)
	doc.Channels = map[ir.ChannelID]ir.Channel{"chan/x/c": {ID: "chan/x/c"}}
	assert.Zero(t, countCode(t, validated(t, doc), "ir/empty-channel-ref"))
}

// TestValidate_OneCodePerEmptyReference pins that a required reference left
// empty is reported under its empty-reference code alone: a binding's
// parameter, an overload's operation and a service's server, none of which
// also draws a second code from a check that reads the same position.
func TestValidate_OneCodePerEmptyReference(t *testing.T) {
	t.Parallel()
	op := paramBindingOp("op/x/o", ir.Parameter{ID: "param/x/o/q", Name: ir.Naming{Source: "q"}})
	op.Bindings.HTTP[0].ParamBindings = append(op.Bindings.HTTP[0].ParamBindings,
		ir.HTTPParamBinding{Location: ir.HTTPLocationQuery})
	op.OverloadOf = new(ir.OpID)
	doc := docWithOperation(op)
	doc.Services[0].Servers = []ir.ServerID{""}

	diags := validated(t, doc)

	assert.Equal(t, []string{scenarioOpPath + ".Bindings.HTTP[0].ParamBindings[1].Param"},
		nodesOfCode(diags, "ir/empty-param-ref"))
	assert.Equal(t, []string{scenarioOpPath + ".OverloadOf"}, nodesOfCode(diags, "ir/empty-op-ref"))
	assert.Equal(t, []string{"doc.Services[0].Servers[0]"}, nodesOfCode(diags, "ir/empty-server-ref"))
	assert.Zero(t, countCode(t, diags, "pass/param-binding-mismatch"))
	assert.Zero(t, countCode(t, diags, "ir/dangling-param-ref"))
}

// TestValidate_EmptyMappingValueIsAnEmptyReference pins that a discriminator's
// mapping to no type is an empty type reference and nothing about variant
// membership, which a type naming nothing cannot be held to, and that a mapping
// to a variant is clean.
func TestValidate_EmptyMappingValueIsAnEmptyReference(t *testing.T) {
	t.Parallel()
	union := func(target ir.TypeID) *ir.Document {
		doc := validDoc()
		doc.Types["t/u"] = &ir.Union{
			ID:            "t/u",
			Variants:      []ir.Variant{{Type: ir.TypeRef{Target: "t/m"}}},
			Discriminator: &ir.Discriminator{PropertyName: "k", Mapping: map[string]ir.TypeID{"a": target}},
		}
		return doc
	}

	diags := validated(t, union(""))

	assert.Equal(t, []string{"doc.Types[t/u].Discriminator.Mapping[a]"}, nodesOfCode(diags, "ir/empty-type-ref"))
	assert.Zero(t, countCode(t, diags, "pass/discriminator-missing-variant"))

	valid := validated(t, union("t/m"))
	assert.Zero(t, countCode(t, valid, "ir/empty-type-ref"))
	assert.Zero(t, countCode(t, valid, "pass/discriminator-missing-variant"))
}

// TestValidate_DocumentedEmptyPositionsStayLegal pins the positions whose
// comments spell "none" as an empty ID: a discriminator's default, an
// idempotency token parameter, and a tag property PropertyName locates.
func TestValidate_DocumentedEmptyPositionsStayLegal(t *testing.T) {
	t.Parallel()
	op := ir.Operation{ID: "op/x/o", Idempotency: ir.Idempotency{Kind: ir.IdempotencySafe, TokenParam: ""}}
	doc := docWithOperation(op)
	doc.Types["t/u"] = &ir.Union{
		ID:       "t/u",
		Variants: []ir.Variant{{Type: ir.TypeRef{Target: "t/m"}}},
		Discriminator: &ir.Discriminator{
			PropertyName: "k", Property: "", Default: "", Mapping: map[string]ir.TypeID{"a": "t/m"},
		},
	}

	diags := validated(t, doc)

	for _, d := range diags {
		assert.NotRegexp(t, `^ir/empty-.*-ref$`, d.Code, "%s at %s", d.Message, d.Provenance.Node)
	}
}

// TestValidate_AnEmptyReferenceDrawsNoSecondCode pins the readers of a required
// ID beside the binding and mapping checks: a message a binding uses, an
// encoding key and a link's parameter, each left empty, draw the empty-reference
// code alone, where each check reading them also reported it under its own.
func TestValidate_AnEmptyReferenceDrawsNoSecondCode(t *testing.T) {
	t.Parallel()
	const op = "doc.Services[0].Groups[0].Operations[0]"
	for _, tc := range []struct {
		name  string
		doc   func() *ir.Document
		code  string
		nodes []string
	}{
		{
			name: "a message a binding uses, beside one its channel carries",
			doc: func() *ir.Document {
				o := ir.Operation{ID: "op/x/o"}
				o.Bindings.Message = &ir.MessageBinding{
					Channel: "chan/x/c", Direction: ir.MsgDirectionSend, Messages: []ir.MessageID{"msg/x/m", ""},
				}
				doc := docWithOperation(o)
				doc.Channels = map[ir.ChannelID]ir.Channel{"chan/x/c": {ID: "chan/x/c", Messages: []ir.MessageID{"msg/x/m"}}}
				doc.Messages = map[ir.MessageID]ir.Message{"msg/x/m": {ID: "msg/x/m"}}
				return doc
			},
			code:  "ir/empty-message-ref",
			nodes: []string{op + ".Bindings.Message.Messages[1]"},
		},
		{
			name: "an encoding key",
			doc: func() *ir.Document {
				return docWithOperation(ir.Operation{ID: "op/x/o", Request: &ir.Payload{Contents: []ir.Content{{
					MediaType: "multipart/form-data", Type: ir.TypeRef{Target: "t/m"},
					Encoding: map[ir.PropID]ir.PartEncoding{"": {}},
				}}}})
			},
			code:  "ir/empty-prop-ref",
			nodes: []string{op + ".Request.Contents[0].Encoding[].key"},
		},
		{
			name: "a link's parameter, given twice",
			doc: func() *ir.Document {
				return docWithOperation(ir.Operation{
					ID: "op/x/o", Params: []ir.Parameter{{ID: "param/x/o/q", Name: ir.Naming{Source: "q"}}},
					Responses: []ir.Response{{Links: []ir.OperationLink{{
						Name: ir.Naming{Source: "l"}, Operation: "op/x/o", Params: []ir.LinkParam{{Param: ""}, {Param: ""}},
					}}}},
				})
			},
			code:  "ir/empty-param-ref",
			nodes: []string{op + ".Responses[0].Links[0].Params[0].Param", op + ".Responses[0].Links[0].Params[1].Param"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			diags := validated(t, tc.doc())

			assert.Equal(t, tc.nodes, nodesOfCode(diags, tc.code))
			for _, d := range diags {
				if d.Code == tc.code || d.Severity != ir.SeverityError {
					continue
				}
				assert.Failf(t, "a second code", "%s at %s: %s", d.Code, d.Provenance.Node, d.Message)
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
	mismatches := func(diags []ir.Diagnostic) []ir.Diagnostic {
		var out []ir.Diagnostic
		for _, d := range diags {
			if d.Code == "pass/param-binding-mismatch" {
				out = append(out, d)
			}
		}
		return out
	}
	op := paramBindingOp("op/x/o", ir.Parameter{ID: "param/x/o/q", Name: ir.Naming{Source: "q"}})
	op.Bindings.HTTP[0].ParamBindings[0].Param = ""
	diags := validated(t, docWithOperation(op))

	assert.Equal(t, []string{scenarioOpPath + ".Bindings.HTTP[0].ParamBindings[0].Param"},
		nodesOfCode(diags, "ir/empty-param-ref"))
	got := mismatches(diags)
	require.Len(t, got, 1)
	assert.Equal(t, ir.SeverityWarning, got[0].Severity)

	unbound := paramBindingOp("op/x/o", ir.Parameter{ID: "param/x/o/q", Name: ir.Naming{Source: "q"}})
	unbound.Bindings.HTTP[0].ParamBindings = nil
	assert.Equal(t, mismatches(validated(t, docWithOperation(unbound))), got,
		"the warning is the one a binding left out draws")
}

// TestValidate_ADiscriminatorLocatingNoTagIsAnEmptyReference pins that a
// discriminator whose Property is empty while neither PropertyName nor Index
// locates the tag is reported as an empty PropID reference, an error, at the
// property, in the wording that says what it lacks, and as nothing else; one
// PropertyName locates is clean.
func TestValidate_ADiscriminatorLocatingNoTagIsAnEmptyReference(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["t/m"].(*ir.Model).Discriminator = &ir.Discriminator{}

	diags := validated(t, doc)

	require.Len(t, diags, 1, "the empty reference alone")
	assert.Equal(t, "ir/empty-prop-ref", diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, "doc.Types[t/m].Discriminator.Property", diags[0].Provenance.Node)
	assert.Contains(t, diags[0].Message, "neither PropertyName nor Index locates the tag")

	located := validDoc()
	located.Types["t/m"].(*ir.Model).Discriminator = &ir.Discriminator{PropertyName: "a"}
	assert.Empty(t, validated(t, located))
}
