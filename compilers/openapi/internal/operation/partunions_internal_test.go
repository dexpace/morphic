// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
	"github.com/dexpace/morphic/ir"
)

// unionWidth is the number of models the union of unionRegistry holds.
const unionWidth = 12

// unionRegistry holds the union t/u of unionWidth models, and the set of those.
func unionRegistry() (ir.TypeRegistry, map[ir.TypeID]bool) {
	reg := ir.TypeRegistry{}
	variants := make([]ir.Variant, 0, unionWidth)
	models := map[ir.TypeID]bool{}
	for i := range unionWidth {
		id := ir.TypeID(fmt.Sprintf("type/m%02d", i))
		reg[id] = &ir.Model{ID: id}
		models[id] = true
		variants = append(variants, ir.Variant{Type: ir.TypeRef{Target: id}})
	}
	reg["type/u"] = &ir.Union{ID: "type/u", Variants: variants}
	return reg, models
}

// countingNode is a node over reg that counts in reads each lookup of a type in
// counted.
func countingNode(reg ir.TypeRegistry, counted map[ir.TypeID]bool, reads *int) func(ir.TypeID) (ir.TypeDef, bool) {
	return func(id ir.TypeID) (ir.TypeDef, bool) {
		if counted[id] {
			*reads++
		}
		td, ok := reg[id]
		return td, ok
	}
}

// TestPartUnions_ReadAUnionOncePerRun holds what a run reads of a union that
// parts name to one reading of its members, whatever the number of parts: the
// flags of a part, for a form body and for a multipart one, and the values its
// encoding applies to. The lookups of the union's models made for one part are
// all that are made for eight.
func TestPartUnions_ReadAUnionOncePerRun(t *testing.T) {
	t.Parallel()
	reg, models := unionRegistry()
	ref := ir.TypeRef{Target: "type/u"}
	decls := []*ir.Property{{Type: ref}}
	readers := map[string]func(unions *partUnions, node func(ir.TypeID) (ir.TypeDef, bool)){
		"a part's flags": func(unions *partUnions, node func(ir.TypeID) (ir.TypeDef, bool)) {
			partTypeFlags(unions, node, ref, true /* multipart */)
			partTypeFlags(unions, node, ref, false)
		},
		"a part's values": func(unions *partUnions, node func(ir.TypeID) (ir.TypeDef, bool)) {
			partValues(unions, node, decls)
		},
	}
	for name, read := range readers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reads := map[int]int{}
			for _, parts := range []int{1, 8} {
				n := 0
				unions := newPartUnions()
				node := countingNode(reg, models, &n)
				for range parts {
					read(unions, node)
				}
				reads[parts] = n
			}
			require.Positive(t, reads[1], "a part's union is read")
			assert.Equal(t, reads[1], reads[8], "lookups of the union's models for 1 part and for 8")
		})
	}
}

// TestPartUnions_ReadADefaultContentTypeOncePerRun holds the default content
// type of a part or an item naming a union to one reading of the union a run:
// the lookups of its models made for one part are all that are made for
// eight, and a second type reaching the union reads its members' values again
// and not its variants.
func TestPartUnions_ReadADefaultContentTypeOncePerRun(t *testing.T) {
	t.Parallel()
	reg, models := unionRegistry()
	reg["type/list"] = &ir.List{ID: "type/list", ElementType: ir.TypeRef{Target: "type/u"}}
	rules := specrules.Table().EncodingRules("3.1.0")
	partOf := func(id ir.TypeID) func(*partUnions, func(ir.TypeID) (ir.TypeDef, bool)) {
		return func(unions *partUnions, node func(ir.TypeID) (ir.TypeDef, bool)) {
			pe := withDefaultContentType(unions, node, rules, &ir.Property{Type: ir.TypeRef{Target: id}}, unencoded, ir.PartEncoding{})
			assert.Equal(t, []string{"application/json"}, pe.ContentTypes, "a part of objects takes JSON")
		}
	}
	item := func(unions *partUnions, node func(ir.TypeID) (ir.TypeDef, bool)) {
		got := unions.itemDefault(node, rules, ir.TypeRef{Target: "type/u"}, unencoded)
		assert.Equal(t, []string{"application/json"}, got, "an item of objects takes JSON")
	}
	readers := map[string]func(*partUnions, func(ir.TypeID) (ir.TypeDef, bool)){
		"a part of the union":      partOf("type/u"),
		"a part listing the union": partOf("type/list"),
		"an item of the union":     item,
	}
	for name, read := range readers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reads := map[int]int{}
			for _, parts := range []int{1, 8} {
				n := 0
				unions := newPartUnions()
				node := countingNode(reg, models, &n)
				for range parts {
					read(unions, node)
				}
				reads[parts] = n
			}
			require.Positive(t, reads[1], "the union is read")
			assert.Equal(t, reads[1], reads[8], "lookups of the union's models for 1 part and for 8")
		})
	}

	n := 0
	unions := newPartUnions()
	node := countingNode(reg, models, &n)
	partOf("type/list")(unions, node)
	first := n
	item(unions, node)
	assert.LessOrEqual(t, n-first, unionWidth, "an item of the union reads its members' values, not its variants again")
}

// TestPartUnions_KeepsADefaultContentTypeByAllItTurnsOn holds the store of
// default content types to every input the reading turns on. Each pair of
// readings differs in one of them, a part's or an item's, the minor's rules,
// or whether the declarations write contentEncoding, and gives two answers;
// asked of one store in turn, the second takes its own answer, not the
// first's.
func TestPartUnions_KeepsADefaultContentTypeByAllItTurnsOn(t *testing.T) {
	t.Parallel()
	reg := ir.TypeRegistry{
		"type/text":   &ir.Primitive{ID: "type/text", PrimitiveKind: ir.PrimitiveString},
		"type/tags":   &ir.List{ID: "type/tags", ElementType: ir.TypeRef{Target: "type/text"}},
		"type/nested": &ir.List{ID: "type/nested", ElementType: ir.TypeRef{Target: "type/tags"}},
	}
	node := func(id ir.TypeID) (ir.TypeDef, bool) {
		td, ok := reg[id]
		return td, ok
	}
	v31, v32 := specrules.Table().EncodingRules("3.1.0"), specrules.Table().EncodingRules("3.2.0")
	part := func(of ir.TypeID, rules specrules.EncodingRules, by contentEncoding) func(*testing.T, *partUnions) []string {
		return func(t *testing.T, u *partUnions) []string {
			t.Helper()
			types, ok := u.partDefault(node, rules, &ir.Property{Type: ir.TypeRef{Target: of}}, by)
			require.True(t, ok, "the values of a part of %s are read", of)
			return types
		}
	}
	item := func(of ir.TypeID, rules specrules.EncodingRules, by contentEncoding) func(*testing.T, *partUnions) []string {
		return func(_ *testing.T, u *partUnions) []string {
			return u.itemDefault(node, rules, ir.TypeRef{Target: of}, by)
		}
	}
	for name, pair := range map[string][2]func(*testing.T, *partUnions) []string{
		"a part and an item of one list":   {part("type/tags", v32, unencoded), item("type/tags", v32, unencoded)},
		"an item under two minors' rules":  {item("type/tags", v31, unencoded), item("type/tags", v32, unencoded)},
		"an item encoded and not":          {item("type/text", v31, unencoded), item("type/text", v31, encoded)},
		"one type under two minors' rules": {part("type/nested", v31, unencoded), part("type/nested", v32, unencoded)},
		"one type encoded and not":         {part("type/text", v31, unencoded), part("type/text", v31, encoded)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			alone := pair[1](t, newPartUnions())
			require.NotEqual(t, pair[0](t, newPartUnions()), alone, "the two readings differ")

			shared := newPartUnions()
			pair[0](t, shared)
			assert.Equal(t, alone, pair[1](t, shared), "the second reading takes its own answer")
		})
	}
}
