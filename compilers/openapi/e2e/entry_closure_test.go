// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// closureUser is the object the cases write a $ref to.
const closureUser = "User: {type: object, properties: {id: {type: string}}}"

// closureRef is the $ref conjunct naming closureUser.
const closureRef = "{$ref: '#/components/schemas/User'}"

// closureOrder is an allOf's entries written in one order, and the index the
// entry a case is about is written at.
type closureOrder struct {
	list string
	at   int
}

// closureOrders writes target and each of others as the entries of an allOf,
// with target at every index, the others in the order given.
func closureOrders(target string, others ...string) []closureOrder {
	var out []closureOrder
	for at := 0; at <= len(others); at++ {
		entries := make([]string, 0, len(others)+1)
		entries = append(entries, others[:at]...)
		entries = append(entries, target)
		entries = append(entries, others[at:]...)
		out = append(out, closureOrder{list: "[" + strings.Join(entries, ", ") + "]", at: at})
	}
	return out
}

// closureModelAt returns the model the schema written at at lowers to.
func closureModelAt(t *testing.T, doc *ir.Document, at jsontext.Pointer) *ir.Model {
	t.Helper()
	model, ok := kindNode(t, doc, usedAt(t, doc, at)).(*ir.Model)
	require.True(t, ok, "a model at %s", at)
	return model
}

// requireClosureKeptWhole holds that the entry of the allOf at at written at
// index is kept whole, for want of a home, as the one thing kept under at and
// the one thing reported there, and that the model it was lowered into is
// open: it carries neither the closure nor a catch-all.
func requireClosureKeptWhole(t *testing.T, doc *ir.Document, diags []ir.Diagnostic, at jsontext.Pointer, index int) {
	t.Helper()
	entry := at + jsontext.Pointer(fmt.Sprintf("/allOf/%d", index))
	assert.Equal(t, map[jsontext.Pointer]ir.UnmodeledReason{entry: ir.ReasonDegradedLowering},
		keptUnder(doc, at), "what is kept at %s", at)
	assert.Equal(t, []jsontext.Pointer{entry}, degradedUnder(diags, at), "what is reported at %s", at)
	model := closureModelAt(t, doc, at)
	assert.Equal(t, entryKept(fmt.Sprintf("openapi:allOf/%d", index)), keptOn(model), "what the model at %s keeps", at)
	assert.Equal(t, ir.AdditionalUnspecified, model.Additional, "the model at %s is open", at)
	assert.Nil(t, model.AdditionalProps, "the model at %s has no catch-all", at)
}

// TestEntryClosure_AClosureThatIsNotTheOnlyDeclarationOfMembersIsKeptWhole
// holds that an entry writing additionalProperties or patternProperties is
// kept whole, under its index and for want of a home, where another conjunct
// declares the members it would read against: another entry, whichever side
// of it the entry is written on, or the position's own properties. The model
// holds the members of every conjunct and is open, in every order the entries
// are written in.
func TestEntryClosure_AClosureThatIsNotTheOnlyDeclarationOfMembersIsKeptWhole(t *testing.T) {
	t.Parallel()
	const props = "{properties: {b: {type: string}}}"
	cases := []struct {
		name    string
		closure string
		others  []string
		own     string // what the position writes beside its allOf
		only31  bool
	}{
		{name: "closure beside another entry's properties", closure: "{additionalProperties: false}", others: []string{closureRef, props}},
		{name: "closure beside a typed entry's properties", closure: "{additionalProperties: false}",
			others: []string{"{type: object, properties: {b: {type: string}}, required: [b]}"}},
		{name: "a catch-all beside another entry's properties", closure: "{type: object, additionalProperties: {type: string}}",
			others: []string{"{type: object, properties: {b: {type: number}}, required: [b]}"}},
		{name: "closure beside the position's own properties", closure: "{additionalProperties: false}",
			others: []string{closureRef}, own: "properties: {b: {type: string}}, "},
		{name: "patterns beside another entry's properties", closure: "{patternProperties: {'^x-': {type: string}}}",
			others: []string{closureRef, props}, only31: true},
	}
	for _, tc := range cases {
		for _, version := range []string{"3.0.3", "3.1.0"} {
			if tc.only31 && version != "3.1.0" {
				continue
			}
			for _, order := range closureOrders(tc.closure, tc.others...) {
				t.Run(fmt.Sprintf("%s/%s/at-%d", version, tc.name, order.at), func(t *testing.T) {
					t.Parallel()
					schema := "{" + tc.own + "allOf: " + order.list + "}"
					inBothMappingOrders(t, conjunctionSpec(version, schema, closureUser), func(t *testing.T, spec string) {
						doc, diags := compileAnnotationSpec(t, "kept", spec)
						requireErrorsAndWarnings(t, diags, nil)
						for _, at := range conjunctionPositions {
							requireClosureKeptWhole(t, doc, diags, at, order.at)
						}
					})
				})
			}
		}
	}
}

// TestEntryClosure_AClosureBesideTheAllOfOfAnObjectWritingItIsTyped holds the
// control the cases that move a closure are read against: the closure written
// beside an allOf of a $ref, and the closure written beside the position's own
// properties, close the model, since the object writing it declares the
// members, whether the allOf names a $ref or not.
func TestEntryClosure_AClosureBesideTheAllOfOfAnObjectWritingItIsTyped(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			for name, schema := range map[string]string{
				"beside a ref":          "{allOf: [" + closureRef + "], additionalProperties: false}",
				"beside own properties": "{properties: {b: {type: string}}, additionalProperties: false, allOf: [" + closureRef + "]}",
			} {
				doc, diags := compileAnnotationSpec(t, "typed", conjunctionSpec(version, schema, closureUser))
				requireErrorsAndWarnings(t, diags, nil)
				for _, at := range conjunctionPositions {
					model := closureModelAt(t, doc, at)
					assert.Equal(t, ir.AdditionalClosed, model.Additional, "%s at %s", name, at)
					require.NotNil(t, model.Base, "%s at %s", name, at)
					assert.Equal(t, componentID("User"), model.Base.Target, "%s at %s", name, at)
					assert.Empty(t, keptUnder(doc, at), "%s at %s", name, at)
					assert.Empty(t, degradedUnder(diags, at), "%s at %s", name, at)
				}
			}
		})
	}
}

// TestEntryClosure_AUseRepeatedLeavesTheModelOfTheClosureAlone holds that a
// closure on an entry, kept whole or typed, lowers to the same types
// whether one property uses the schema or several positions do.
func TestEntryClosure_AUseRepeatedLeavesTheModelOfTheClosureAlone(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for name, schema := range map[string]string{
			"closure beside a declaring entry": "{allOf: [" + closureRef + ", {properties: {b: {type: string}}}, {additionalProperties: false}]}",
			"closure of the only entry":        "{allOf: [" + closureRef + ", {additionalProperties: false}]}",
		} {
			t.Run(version+"/"+name, func(t *testing.T) {
				t.Parallel()
				requireRepeatedUsesLeaveNodesAlone(t, version, schema, closureUser)
			})
		}
	}
}
