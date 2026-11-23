// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// promoted marks a declaration whose deprecation a promotion read, as the
// promotion policy spells it.
const promoted ir.Inferred = "extension-promotion"

// noteCase is one note a declaration's property holds: how to write it on a
// property from a value's name, and how to read it off a property or a node.
type noteCase struct {
	detail string
	write  func(property *ir.Property, value string)
	read   func(docs ir.Docs, deprecation *ir.Deprecation) string
}

// noteCases are the notes lastWritten ranks a declaration the model writes
// itself first for, and leaveNotesOnNodes leaves on a node.
var noteCases = []noteCase{
	{"title", func(p *ir.Property, v string) { p.Docs.Summary = v }, func(d ir.Docs, _ *ir.Deprecation) string { return d.Summary }},
	{"description", func(p *ir.Property, v string) { p.Docs.Description = v },
		func(d ir.Docs, _ *ir.Deprecation) string { return d.Description }},
	{"external docs", func(p *ir.Property, v string) { p.Docs.ExternalDocs = []ir.Link{{URL: v}} },
		func(d ir.Docs, _ *ir.Deprecation) string {
			if len(d.ExternalDocs) == 0 {
				return ""
			}
			return d.ExternalDocs[0].URL
		}},
	{"deprecation", func(p *ir.Property, v string) { p.Deprecation = &ir.Deprecation{Message: v} },
		func(_ ir.Docs, d *ir.Deprecation) string {
			if d == nil {
				return ""
			}
			return d.Message
		}},
}

// inBothOrders runs check over decls as written and reversed, labelled.
func inBothOrders(decls []declaration, check func(label string, decls []declaration)) {
	for order := range 2 {
		ordered := slices.Clone(decls)
		if order == 1 {
			slices.Reverse(ordered)
		}
		check(fmt.Sprintf("order %d", order), ordered)
	}
}

// TestFold_ANoteTheModelWritesIsKeptOverOneItBrings pins lastWritten's
// ranking: of a declaration the model writes itself and one another model
// holds giving a note differently, the field keeps the model's own in either
// order, and the other, which stays declared on its model, is reported at the
// entry bringing it and kept nowhere verbatim. A default and XML hints keep the
// later declaration's, as the flattening's override does.
func TestFold_ANoteTheModelWritesIsKeptOverOneItBrings(t *testing.T) {
	t.Parallel()
	for _, note := range noteCases {
		own := declaration{property: ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveString), Provenance: ptrAt("/a")},
			source: written(`{"own":true}`)}
		brought := declaration{property: ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveString), Provenance: ptrAt("/part/p")},
			via: &composedEntry{target: "type/part", at: "/M/allOf/1"}, typeAt: sitesAt("/part/p")}
		note.write(&own.property, "own")
		note.write(&brought.property, "brought")
		inBothOrders([]declaration{own, brought}, func(label string, decls []declaration) {
			stubbed, recorded := stubMerger(narrowingRegistry())

			got := stubbed.fold(decls)

			assert.Equal(t, "own", note.read(got.Docs, got.Deprecation), "%s, %s: the model's own is the field's", note.detail, label)
			assert.Empty(t, got.Unmodeled, "%s, %s: nothing is kept verbatim", note.detail, label)
			require.Len(t, *recorded, 1, "%s, %s", note.detail, label)
			assert.Equal(t, jsontext.Pointer("/M/allOf/1"), (*recorded)[0].Provenance.Pointer, "%s, %s", note.detail, label)
			assert.Contains(t, (*recorded)[0].Message, "give its "+note.detail+" differently; kept the one the declaration at /a "+
				"gives over the one at /part/p, which stays declared on its model", "%s, %s", note.detail, label)
		})
	}

	t.Run("a default and xml hints", func(t *testing.T) {
		t.Parallel()
		one, two := number("1"), number("2")
		own := declaration{property: ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveInteger), Default: &one,
			XML: &ir.XMLHints{Name: "own"}, Provenance: ptrAt("/a")}, source: written(`{"own":true}`)}
		brought := declaration{property: ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveInteger), Default: &two,
			XML: &ir.XMLHints{Name: "brought"}, Provenance: ptrAt("/part/p")},
			via: &composedEntry{target: "type/part", at: "/M/allOf/1"}, typeAt: sitesAt("/part/p")}
		stubbed, _ := stubMerger(narrowingRegistry())

		got := stubbed.fold([]declaration{own, brought})

		assert.Equal(t, &two, got.Default, "the later default is the field's")
		assert.Equal(t, "brought", got.XML.Name, "and so are the later xml hints")
		assert.Contains(t, got.Unmodeled, losingDeclarationKey+"/a", "the model's own is kept verbatim")
	})
}

// ownNodeAt registers at pointer, in reg, the node a declaration written there
// builds, and returns its ID.
func ownNodeAt(reg map[ir.TypeID]ir.TypeDef, node ir.TypeDef, pointer jsontext.Pointer) ir.TypeID {
	common := node.Common()
	common.ID, common.Anonymous, common.Provenance = ir.TypeID("type/anonymous"+pointer), true, ptrAt(pointer)
	reg[common.ID] = node
	return common.ID
}

// TestFold_ANoteTwoOwnDeclarationsGiveDifferentlyStaysOnItsNode pins
// leaveNotesOnNodes: of two declarations the model writes itself, each whose
// type is a model its schema builds, giving a note differently, the field
// keeps the later's, and the earlier's stays on the node its schema builds,
// which the field's type then is, in either order. The fold reports the tie,
// keeps nothing verbatim and marks the node with what promoted that
// declaration, and the field with what promoted its own.
func TestFold_ANoteTwoOwnDeclarationsGiveDifferentlyStaysOnItsNode(t *testing.T) {
	t.Parallel()
	for _, note := range noteCases {
		inBothOrders([]declaration{{}, {}}, func(label string, _ []declaration) {
			reg := narrowingRegistry()
			first, second := ownNodeAt(reg, &ir.Model{}, "/allOf/0/p"), ownNodeAt(reg, &ir.Model{}, "/allOf/1/p")
			decls := []declaration{
				{property: ir.Property{WireName: "p", Type: ir.TypeRef{Target: first}, Provenance: ptrAt("/allOf/0/p")}, source: unread(t)},
				{property: ir.Property{WireName: "p", Type: ir.TypeRef{Target: second}, Provenance: ptrAt("/allOf/1/p")}, source: unread(t)},
			}
			if label == "order 1" {
				slices.Reverse(decls)
			}
			note.write(&decls[0].property, "earlier")
			note.write(&decls[1].property, "later")
			decls[0].property.Provenance.Inferred = promoted
			stubbed, recorded := stubMerger(reg)

			got := stubbed.fold(decls)

			node := reg[decls[0].property.Type.Target].Common()
			assert.Equal(t, "later", note.read(got.Docs, got.Deprecation), "%s, %s: the field keeps the later", note.detail, label)
			assert.Equal(t, "earlier", note.read(node.Docs, node.Deprecation), "%s, %s: the earlier's node keeps its own", note.detail, label)
			assert.Equal(t, decls[0].property.Type.Target, got.Type.Target, "%s, %s: which is the field's type", note.detail, label)
			assert.Empty(t, reg[decls[1].property.Type.Target].Common().Docs, "%s, %s", note.detail, label)
			wantMark := ir.Inferred("")
			if note.detail == "deprecation" {
				wantMark = promoted
			}
			assert.Equal(t, wantMark, node.Provenance.Inferred, "%s, %s: the node takes its mark with a deprecation", note.detail, label)
			assert.Empty(t, got.Provenance.Inferred, "%s, %s: and the field keeps the later's", note.detail, label)
			require.Len(t, *recorded, 1, "%s, %s", note.detail, label)
			assert.Equal(t, diag.DegradedConstruct, (*recorded)[0].Code)
			assert.Contains(t, (*recorded)[0].Message, "give its "+note.detail+" differently; kept the one the declaration at "+
				string(decls[1].property.Provenance.Pointer)+" gives over the one at "+string(decls[0].property.Provenance.Pointer)+
				", which the field's type holds", "%s, %s", note.detail, label)
		})
	}
}

// TestFold_ANoteIsLeftOnNoNodeTheFieldDoesNotHold pins where
// leaveNotesOnNodes leaves nothing, so the earlier declaration is kept
// verbatim, as for any note the field does not keep: a node the field's type
// does not hold once it holds the note, as of two lists judged apart for a
// note alone, which takeBack writes back as it was; a node holding
// that note already; a node written elsewhere; and a declaration another model
// holds, which stays declared there.
func TestFold_ANoteIsLeftOnNoNodeTheFieldDoesNotHold(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		earlier func(reg map[ir.TypeID]ir.TypeDef) declaration
		keptAs  string
	}{
		{"a list judged apart", func(reg map[ir.TypeID]ir.TypeDef) declaration {
			return ownDeclaration(ownNodeAt(reg, &ir.List{ElementType: primitiveRef(ir.PrimitiveString)}, "/allOf/0/p"))
		}, "is kept verbatim under Unmodeled"},
		{"a node holding one", func(reg map[ir.TypeID]ir.TypeDef) declaration {
			model := &ir.Model{Docs: ir.Docs{Description: "its own"}}
			return ownDeclaration(ownNodeAt(reg, model, "/allOf/0/p"))
		}, "is kept verbatim under Unmodeled"},
		{"a node written elsewhere", func(reg map[ir.TypeID]ir.TypeDef) declaration {
			return ownDeclaration(ownNodeAt(reg, &ir.Model{}, "/elsewhere"))
		}, "is kept verbatim under Unmodeled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg := narrowingRegistry()
			earlier := tc.earlier(reg)
			earlier.property.Docs.Description = "earlier"
			later := ownDeclaration(ownNodeAt(reg, cloneNode(reg[earlier.property.Type.Target]), "/allOf/1/p"))
			later.property.Provenance = ptrAt("/allOf/1/p")
			later.property.Docs.Description = "later"
			before := *reg[earlier.property.Type.Target].Common()
			stubbed, recorded := stubMerger(reg)

			got := stubbed.fold([]declaration{earlier, later})

			assert.Equal(t, "later", got.Docs.Description)
			assert.Empty(t, cmp.Diff(before, *reg[earlier.property.Type.Target].Common()), "the earlier's node is as it was")
			assert.Contains(t, got.Unmodeled, losingDeclarationKey+"/allOf/0/p", "the earlier is kept verbatim")
			require.NotEmpty(t, *recorded)
			assert.Contains(t, (*recorded)[len(*recorded)-1].Message, "give its description differently")
			assert.Contains(t, (*recorded)[len(*recorded)-1].Message, tc.keptAs)
		})
	}
}

// TestFold_ANoteIsLeftOnNoNodeHoldingOneAlready pins that leaveNotesOnNodes
// writes over no note a node holds: the earlier declaration is kept verbatim,
// and its node keeps its own, whichever note it is.
func TestFold_ANoteIsLeftOnNoNodeHoldingOneAlready(t *testing.T) {
	t.Parallel()
	for _, note := range noteCases {
		reg := narrowingRegistry()
		var holding ir.Property
		note.write(&holding, "its own")
		earlier := ownDeclaration(ownNodeAt(reg, &ir.Model{Docs: holding.Docs, Deprecation: holding.Deprecation}, "/allOf/0/p"))
		later := ownDeclaration(ownNodeAt(reg, &ir.Model{}, "/allOf/1/p"))
		later.property.Provenance = ptrAt("/allOf/1/p")
		note.write(&earlier.property, "earlier")
		note.write(&later.property, "later")
		stubbed, _ := stubMerger(reg)

		got := stubbed.fold([]declaration{earlier, later})

		node := reg[earlier.property.Type.Target].Common()
		assert.Equal(t, "later", note.read(got.Docs, got.Deprecation), note.detail)
		assert.Equal(t, "its own", note.read(node.Docs, node.Deprecation), "%s: the node keeps its own", note.detail)
		assert.Contains(t, got.Unmodeled, losingDeclarationKey+"/allOf/0/p", "%s: the earlier is kept verbatim", note.detail)
	}
}

// TestFold_ANoteLeftOnAConjunctStaysThere pins that a note left on the node of
// a declaration whose type the field composes stays on that node, which the
// field's composition names, in either order.
func TestFold_ANoteLeftOnAConjunctStaysThere(t *testing.T) {
	t.Parallel()
	inBothOrders([]declaration{{}, {}}, func(label string, _ []declaration) {
		reg := narrowingRegistry()
		first := ownNodeAt(reg, &ir.Model{Properties: []ir.Property{{WireName: "a"}}}, "/allOf/0/p")
		second := ownNodeAt(reg, &ir.Model{Properties: []ir.Property{{WireName: "b"}}}, "/allOf/1/p")
		decls := []declaration{ownDeclaration(first), ownDeclaration(second)}
		decls[1].property.Provenance = ptrAt("/allOf/1/p")
		if label == "order 1" {
			slices.Reverse(decls)
		}
		decls[0].property.Docs.Description, decls[1].property.Docs.Description = "earlier", "later"
		stubbed, _ := stubMerger(reg)

		got := stubbed.fold(decls)

		composed, isModel := reg[got.Type.Target].(*ir.Model)
		require.True(t, isModel, label)
		assert.Len(t, composed.Mixins, 2, "%s: the field composes both", label)
		assert.Equal(t, "earlier", reg[decls[0].property.Type.Target].Common().Docs.Description, label)
		assert.Equal(t, "later", got.Docs.Description, label)
		assert.Empty(t, got.Unmodeled, label)
	})
}

// ownDeclaration is a declaration the model writes itself at /allOf/0/p, of
// the type target names.
func ownDeclaration(target ir.TypeID) declaration {
	return declaration{property: ir.Property{WireName: "p", Type: ir.TypeRef{Target: target}, Provenance: ptrAt("/allOf/0/p")},
		source: written(`{"declared":"here"}`)}
}

// cloneNode is a copy of node with no notes, for another declaration to
// build: a list over the same element, or a bare model.
func cloneNode(node ir.TypeDef) ir.TypeDef {
	if list, isList := node.(*ir.List); isList {
		return &ir.List{ElementType: list.ElementType}
	}
	return &ir.Model{}
}

// TestFold_EveryExampleTheModelWritesIsHeld pins heldExamples: the field holds
// every example the model's own declarations give that it takes, each once,
// each declaration's in its order and the declarations' in the order of their
// encodings, the same in either order, and reports none of them. One another
// model's declaration brings that the field holds too is not reported, and
// one it does not hold is, kept on that model.
func TestFold_EveryExampleTheModelWritesIsHeld(t *testing.T) {
	t.Parallel()
	one, two, three := number("1"), number("2"), number("3")
	invalid := ir.Value{Kind: ir.ValueString, String: "\xff"}
	writing := func(at jsontext.Pointer, values ...*ir.Value) declaration {
		declarationValue := declaration{property: ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveAny), Provenance: ptrAt(at)},
			source: written(`{"at":"` + string(at) + `"}`)}
		for _, value := range values {
			declarationValue.property.Examples = append(declarationValue.property.Examples, ir.Example{Value: value})
		}
		return declarationValue
	}
	bringing := func(at jsontext.Pointer, values ...*ir.Value) declaration {
		declarationValue := writing(at, values...)
		declarationValue.via, declarationValue.typeAt = &composedEntry{target: "type/part", at: "/M/allOf/9"}, sitesAt(at)
		return declarationValue
	}
	for _, tc := range []struct {
		name   string
		decls  []declaration
		orders [2][]*ir.Value
		notes  []string
	}{
		{"two the model writes, one repeated", []declaration{writing("/a", &one), writing("/b", &two, &one)},
			[2][]*ir.Value{{&one, &two}, {&one, &two}}, nil},
		{"one brought that the field holds too", []declaration{writing("/a", &one), bringing("/part/p", &one)},
			[2][]*ir.Value{{&one}, {&one}}, nil},
		{"one brought that it does not", []declaration{writing("/a", &one), writing("/b", &two), bringing("/part/p", &three)},
			[2][]*ir.Value{{&one, &two}, {&one, &two}},
			[]string{"give its examples differently; kept those the declarations the model writes itself give over the ones " +
				"the declaration at /part/p gives, which stays declared on its model"}},
		{"two the model writes, held in one order", []declaration{writing("/a", &two), writing("/b", &one, &three)},
			[2][]*ir.Value{{&one, &three, &two}, {&one, &three, &two}}, nil},
		{"one that does not encode, which sorts first", []declaration{writing("/a", &one), writing("/b", &invalid)},
			[2][]*ir.Value{{&invalid, &one}, {&invalid, &one}}, nil},
		{"brought alone", []declaration{bringing("/part/p", &one), bringing("/part/q", &two)},
			[2][]*ir.Value{{&two}, {&one}}, []string{"give its examples differently; kept the one the declaration at "}},
	} {
		inBothOrders(tc.decls, func(label string, decls []declaration) {
			stubbed, recorded := stubMerger(narrowingRegistry())

			got := stubbed.fold(decls)

			want := tc.orders[map[string]int{"order 0": 0, "order 1": 1}[label]]
			values := make([]*ir.Value, 0, len(got.Examples))
			for _, example := range got.Examples {
				values = append(values, example.Value)
			}
			assert.Equal(t, want, values, "%s, %s", tc.name, label)
			assert.Empty(t, got.Unmodeled, "%s, %s: nothing is kept verbatim", tc.name, label)
			require.Len(t, *recorded, len(tc.notes), "%s, %s", tc.name, label)
			for i, note := range tc.notes {
				assert.Contains(t, (*recorded)[i].Message, note, "%s, %s", tc.name, label)
			}
		})
	}
}
