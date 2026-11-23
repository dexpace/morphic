// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"
	"strings"
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
// entry bringing it and kept nowhere verbatim; a deprecation brings the mark of
// what promoted it. A default and XML hints keep the later declaration's, as
// the flattening's override does.
func TestFold_ANoteTheModelWritesIsKeptOverOneItBrings(t *testing.T) {
	t.Parallel()
	for _, note := range noteCases {
		own := declaration{property: ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveString), Provenance: ptrAt("/a")},
			source: written(`{"own":true}`)}
		brought := declaration{property: ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveString), Provenance: ptrAt("/part/p")},
			via: &composedEntry{target: "type/part", at: "/M/allOf/1"}, typeAt: sitesAt("/part/p")}
		note.write(&own.property, "own")
		note.write(&brought.property, "brought")
		own.property.Provenance.Inferred = promoted
		inBothOrders([]declaration{own, brought}, func(label string, decls []declaration) {
			stubbed, recorded := stubMerger(narrowingRegistry())

			got := stubbed.fold(decls)

			assert.Equal(t, "own", note.read(got.Docs, got.Deprecation), "%s, %s: the model's own is the field's", note.detail, label)
			if note.detail == "deprecation" {
				assert.Equal(t, promoted, got.Provenance.Inferred, "%s, %s: with the mark of what promoted it", note.detail, label)
			}
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
// note alone, which takeBack writes back as it was; a node holding that note
// already; a named node, a node written elsewhere or in another document; and
// the node of a declaration the model writes itself that a model of its own
// holds too (apart), whose member carries the note.
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
		{"a named node", func(reg map[ir.TypeID]ir.TypeDef) declaration {
			declarationValue := ownDeclaration(ownNodeAt(reg, &ir.Model{}, "/allOf/0/p"))
			reg[declarationValue.property.Type.Target].Common().Anonymous = false
			return declarationValue
		}, "is kept verbatim under Unmodeled"},
		{"a node another document writes at the pointer", func(reg map[ir.TypeID]ir.TypeDef) declaration {
			declarationValue := ownDeclaration(ownNodeAt(reg, &ir.Model{}, "/allOf/0/p"))
			reg[declarationValue.property.Type.Target].Common().Provenance.Source = 1
			return declarationValue
		}, "is kept verbatim under Unmodeled"},
		{"a declaration another model holds too", func(reg map[ir.TypeID]ir.TypeDef) declaration {
			declarationValue := ownDeclaration(ownNodeAt(reg, &ir.Model{}, "/allOf/0/p"))
			declarationValue.apart = true
			return declarationValue
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
			require.Len(t, *recorded, 1, "the tie alone is reported, the types judged as with no note left: %v", *recorded)
			assert.Contains(t, (*recorded)[0].Message, "give its description differently")
			assert.Contains(t, (*recorded)[0].Message, tc.keptAs)
		})
	}
}

// TestFold_ANoteTakenBackLeavesTheNodeAsItWas pins takeBack: where two lists
// judged apart for their notes alone differ in every note, a title, a
// description, external links and a deprecation a promotion read, the earlier
// list's node is left whole as it was, its mark too, in either order, and
// each note it gave is reported kept verbatim, as the declaration is.
func TestFold_ANoteTakenBackLeavesTheNodeAsItWas(t *testing.T) {
	t.Parallel()
	inBothOrders([]declaration{{}, {}}, func(label string, _ []declaration) {
		reg := narrowingRegistry()
		element := primitiveRef(ir.PrimitiveString)
		decls := []declaration{
			ownDeclaration(ownNodeAt(reg, &ir.List{ElementType: element}, "/allOf/0/p")),
			ownDeclaration(ownNodeAt(reg, &ir.List{ElementType: element}, "/allOf/1/p")),
		}
		decls[1].property.Provenance = ptrAt("/allOf/1/p")
		if label == "order 1" {
			slices.Reverse(decls)
		}
		for k, value := range []string{"earlier", "later"} {
			for _, note := range noteCases {
				note.write(&decls[k].property, value)
			}
		}
		decls[0].property.Provenance.Inferred = promoted
		before := *reg[decls[0].property.Type.Target].Common()
		stubbed, recorded := stubMerger(reg)

		got := stubbed.fold(decls)

		assert.Empty(t, cmp.Diff(before, *reg[decls[0].property.Type.Target].Common()), "%s: the earlier's node is as it was", label)
		assert.Contains(t, got.Unmodeled, losingDeclarationKey+string(decls[0].property.Provenance.Pointer), "%s", label)
		require.Len(t, *recorded, len(noteCases), "%s: each note is reported, the types judged as with none left", label)
		for _, report := range *recorded {
			assert.Contains(t, report.Message, "which is kept verbatim under Unmodeled", label)
		}
	})
}

// TestFold_ANoteTheFieldKeepsIsLeftOnNoNode pins that leaveNotesOnNodes leaves
// a note only where it differs from the one the field keeps: two or three
// declarations the model writes itself, each whose type is a model its schema
// builds, giving the kept note alike leave every node bare, in either order,
// and report nothing.
func TestFold_ANoteTheFieldKeepsIsLeftOnNoNode(t *testing.T) {
	t.Parallel()
	for _, note := range noteCases {
		for _, count := range []int{2, 3} {
			inBothOrders(make([]declaration, count), func(label string, _ []declaration) {
				reg := narrowingRegistry()
				decls := make([]declaration, count)
				for k := range decls {
					at := jsontext.Pointer(fmt.Sprintf("/allOf/%d/p", k))
					decls[k] = ownDeclaration(ownNodeAt(reg, &ir.Model{}, at))
					decls[k].property.Provenance = ptrAt(at)
					note.write(&decls[k].property, "alike")
				}
				if label == "order 1" {
					slices.Reverse(decls)
				}
				stubbed, recorded := stubMerger(reg)

				got := stubbed.fold(decls)

				assert.Equal(t, "alike", note.read(got.Docs, got.Deprecation), "%s, %d, %s", note.detail, count, label)
				for _, declarationValue := range decls {
					node := reg[declarationValue.property.Type.Target].Common()
					assert.Empty(t, node.Docs, "%s, %d, %s: every node stays bare", note.detail, count, label)
					assert.Nil(t, node.Deprecation, "%s, %d, %s", note.detail, count, label)
				}
				assert.Empty(t, *recorded, "%s, %d, %s", note.detail, count, label)
			})
		}
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

// TestFold_ANoteTheBuiltNodeDoesNotAdoptIsKeptVerbatim pins takeBack after
// the type is built: of three declarations the model writes itself, enums
// sharing some members, two giving a note the field does not keep, the node
// they meet in adopts one of the two, which is reported held there, and the
// other is reported and kept verbatim, every declaration's own node left
// bare, in either order.
func TestFold_ANoteTheBuiltNodeDoesNotAdoptIsKeptVerbatim(t *testing.T) {
	t.Parallel()
	for _, note := range noteCases {
		inBothOrders(make([]declaration, 3), func(label string, _ []declaration) {
			reg := narrowingRegistry()
			decls := make([]declaration, 3)
			for k, extra := range []string{"a", "d", "e"} {
				at := jsontext.Pointer(fmt.Sprintf("/allOf/%d/p", k))
				members := make([]ir.EnumMember, 0, 3)
				for _, value := range []string{"b", "c", extra} {
					members = append(members, ir.EnumMember{ID: ir.EnumMemberID("enum-member/anonymous" + string(at) + "/s:" + value), Value: stringValue(value)})
				}
				decls[k] = ownDeclaration(ownNodeAt(reg, &ir.Enum{ValueType: ir.PrimitiveString, Members: members}, at))
				decls[k].property.Provenance = ptrAt(at)
				note.write(&decls[k].property, []string{"first", "second", "kept"}[k])
			}
			if label == "order 1" {
				decls[0], decls[1] = decls[1], decls[0]
			}
			stubbed, recorded := stubMerger(reg)

			got := stubbed.fold(decls)

			built := reg[got.Type.Target].Common()
			require.False(t, slices.ContainsFunc(decls, func(d declaration) bool { return d.property.Type.Target == got.Type.Target }),
				"%s, %s: the field's type is a node the fold built", note.detail, label)
			assert.Equal(t, "kept", note.read(got.Docs, got.Deprecation), "%s, %s", note.detail, label)
			adopted := note.read(built.Docs, built.Deprecation)
			require.Contains(t, []string{"first", "second"}, adopted, "%s, %s: the built node adopts one", note.detail, label)
			for k := range 2 {
				at := string(decls[k].property.Provenance.Pointer)
				node := reg[decls[k].property.Type.Target].Common()
				assert.Empty(t, note.read(node.Docs, node.Deprecation), "%s, %s: %s's node is bare", note.detail, label, at)
				which := "which is kept verbatim under Unmodeled"
				if note.read(decls[k].property.Docs, decls[k].property.Deprecation) == adopted {
					which = "which the field's type holds"
				} else {
					assert.Contains(t, got.Unmodeled, losingDeclarationKey+at, "%s, %s", note.detail, label)
				}
				assert.True(t, slices.ContainsFunc(*recorded, func(d ir.Diagnostic) bool {
					return strings.Contains(d.Message, "give its "+note.detail+" differently") && strings.Contains(d.Message, "over the one at "+at+", "+which)
				}), "%s, %s: %s is reported %q: %v", note.detail, label, at, which, *recorded)
			}
		})
	}
}

// TestFold_ABuiltNodeHoldsALeftDeprecationOnlyBesideItsKeys pins
// builder.keepDeprecationWhole: of three enums meeting in a node the fold
// builds, the last giving the deprecation the field keeps and one or two
// others another, the node adopts one of the others only where the keys it
// holds that a deprecation is read from are that declaration's, and otherwise
// holds none, each other reported where it is, in each of three orders
// writing the kept one last.
func TestFold_ABuiltNodeHoldsALeftDeprecationOnlyBesideItsKeys(t *testing.T) {
	t.Parallel()
	type writes struct {
		deprecation *ir.Deprecation
		key, value  string
	}
	read := func(message string) writes { return writes{&ir.Deprecation{Message: message}, deprecationKey, message} }
	byKeyword := writes{deprecation: &ir.Deprecation{}}
	for _, tc := range []struct {
		name    string
		given   [3]writes
		adopted bool
	}{
		{"each beside its own key", [3]writes{read("left"), {}, read("kept")}, false},
		{"the kept one by its keyword", [3]writes{read("left"), {}, byKeyword}, true},
		{"the left one by its keyword", [3]writes{byKeyword, {}, read("kept")}, false},
		{"the kept one beside a key read for none", [3]writes{read("left"), {}, {&ir.Deprecation{}, "openapi:x-other", "other"}}, true},
		{"two left beside their own keys, the kept one by its keyword", [3]writes{read("left"), read("other"), byKeyword}, true},
	} {
		for _, order := range [][3]int{{0, 1, 2}, {1, 0, 2}, {0, 2, 1}} {
			if order[2] != 2 && tc.given[order[2]].deprecation != nil {
				continue // the field would keep another than the one given last
			}
			label := fmt.Sprintf("%s, order %v", tc.name, order)
			reg := narrowingRegistry()
			decls := make([]declaration, 3)
			for k, role := range order {
				at := jsontext.Pointer(fmt.Sprintf("/allOf/%d/p", k))
				members := make([]ir.EnumMember, 0, 3)
				for _, value := range []string{"b", "c", fmt.Sprint(k)} {
					members = append(members, ir.EnumMember{ID: ir.EnumMemberID("enum-member/anonymous" + string(at) + "/s:" + value), Value: stringValue(value)})
				}
				node := &ir.Enum{ValueType: ir.PrimitiveString, Members: members}
				decls[k] = ownDeclaration(ownNodeAt(reg, node, at))
				decls[k].property.Provenance = ptrAt(at)
				decls[k].property.Deprecation = tc.given[role].deprecation
				if tc.given[role].key != "" {
					node.Unmodeled = ir.Unmodeled{tc.given[role].key: {Reason: ir.ReasonVendorExtension,
						Value: ir.RawValue(`"` + tc.given[role].value + `"`), Provenance: ptrAt(at + "/x")}}
				}
			}
			stubbed, recorded := stubMerger(reg)

			got := stubbed.fold(decls)

			require.Equal(t, tc.given[2].deprecation, got.Deprecation, "%s: the field keeps the last", label)
			require.Equal(t, composedAt("/allOf/0/p"), got.Type.Target, "%s: the field's type is a node the fold built", label)
			built := reg[got.Type.Target].Common()
			require.Equal(t, tc.adopted, built.Deprecation != nil, "%s: the built node adopts one", label)
			if built.Deprecation != nil && built.Deprecation.Message != "" {
				assert.Equal(t, ir.RawValue(`"`+built.Deprecation.Message+`"`), built.Unmodeled[deprecationKey].Value, "%s: beside its own key", label)
			}
			for k, role := range order {
				if role == 2 || tc.given[role].deprecation == nil {
					continue
				}
				at := string(decls[k].property.Provenance.Pointer)
				which := "which is kept verbatim under Unmodeled"
				if built.Deprecation != nil && built.Deprecation.Message == tc.given[role].deprecation.Message {
					which = "which the field's type holds"
				} else {
					assert.Contains(t, got.Unmodeled, losingDeclarationKey+at, "%s: %s is kept verbatim", label, at)
				}
				assert.True(t, slices.ContainsFunc(*recorded, func(d ir.Diagnostic) bool {
					return strings.Contains(d.Message, "give its deprecation differently") && strings.Contains(d.Message, "over the one at "+at+", "+which)
				}), "%s: %s is reported %q: %v", label, at, which, *recorded)
			}
		}
	}
}

// TestHeldIn_ATypeTheRegistryLacksHoldsNoNote pins that a field's type the
// registry does not hold holds no note, which no fold reaches yet, rather than
// reading a nil node.
func TestHeldIn_ATypeTheRegistryLacksHoldsNoNote(t *testing.T) {
	t.Parallel()
	stubbed, _ := stubMerger(narrowingRegistry())

	held := heldIn(&stubbed.Merger, "type/missing")(func(*ir.TypeCommon) bool { return true })

	assert.False(t, held)
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
// where it first stands in their lists ordered by their encodings, the same in
// either order, and reports none of them, an own declaration a model of its
// own holds too (apart) among them. One another model's declaration brings
// that the field holds too is not reported, and one it does not hold is,
// kept on that model.
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
	heldApart := func(at jsontext.Pointer, values ...*ir.Value) declaration {
		declarationValue := writing(at, values...)
		declarationValue.apart = true
		return declarationValue
	}
	for _, tc := range []struct {
		name     string
		decls    []declaration
		orders   [2][]*ir.Value
		notes    []string
		verbatim []jsontext.Pointer
	}{
		{"two the model writes, one repeated", []declaration{writing("/a", &one), writing("/b", &two, &one)},
			[2][]*ir.Value{{&one, &two}, {&one, &two}}, nil, nil},
		{"one brought that the field holds too", []declaration{writing("/a", &one), bringing("/part/p", &one)},
			[2][]*ir.Value{{&one}, {&one}}, nil, nil},
		{"one brought that it does not", []declaration{writing("/a", &one), writing("/b", &two), bringing("/part/p", &three)},
			[2][]*ir.Value{{&one, &two}, {&one, &two}},
			[]string{"give its examples differently; kept those the declarations the model writes itself give over the one " +
				"at /part/p, which stays declared on its model"}, nil},
		{"one brought beside one the model writes", []declaration{writing("/a", &one), bringing("/part/p", &three)},
			[2][]*ir.Value{{&one}, {&one}},
			[]string{"give its examples differently; kept the one the declaration at /a gives over the one at /part/p, " +
				"which stays declared on its model"}, nil},
		{"two the model writes, held in one order", []declaration{writing("/a", &two), writing("/b", &one, &three)},
			[2][]*ir.Value{{&one, &three, &two}, {&one, &three, &two}}, nil, nil},
		{"two the model writes, one sorting first sharing one", []declaration{writing("/a", &one, &three), writing("/b", &two, &one)},
			[2][]*ir.Value{{&one, &three, &two}, {&one, &three, &two}}, nil, nil},
		{"one another model holds too", []declaration{writing("/a", &two), heldApart("/b", &one)},
			[2][]*ir.Value{{&one, &two}, {&one, &two}}, nil, nil},
		{"one another model holds too, giving one held", []declaration{writing("/a", &one), heldApart("/b", &one)},
			[2][]*ir.Value{{&one}, {&one}}, nil, nil},
		{"two another model holds too, beside one brought", []declaration{heldApart("/a", &two), heldApart("/b", &one), bringing("/part/p", &three)},
			[2][]*ir.Value{{&one, &two}, {&one, &two}},
			[]string{"give its examples differently; kept those the declarations the model writes itself give over the one " +
				"at /part/p, which stays declared on its model"}, nil},
		{"one that does not encode, which sorts first", []declaration{writing("/a", &one), writing("/b", &invalid)},
			[2][]*ir.Value{{&invalid, &one}, {&invalid, &one}}, nil, nil},
		{"brought alone", []declaration{bringing("/part/p", &one), bringing("/part/q", &two)},
			[2][]*ir.Value{{&two}, {&one}}, []string{"give its examples differently; kept the one the declaration at "}, nil},
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
			var verbatim []string
			for _, at := range tc.verbatim {
				verbatim = append(verbatim, losingDeclarationKey+string(at))
			}
			assert.Equal(t, verbatim, slices.Sorted(maps.Keys(got.Unmodeled)), "%s, %s: the declarations kept verbatim", tc.name, label)
			require.Len(t, *recorded, len(tc.notes), "%s, %s", tc.name, label)
			for i, note := range tc.notes {
				assert.Contains(t, (*recorded)[i].Message, note, "%s, %s", tc.name, label)
			}
		})
	}
}
