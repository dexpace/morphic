// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/spelling"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// lowerUnion hoists a multi-typed schema (e.g. type: [string, integer]) as an
// untagged union with one variant per declared type, exclusive unless two of
// them overlap (typesDisjoint). A type array asks for one of its types, not for
// exactly one, so overlapping variants must not exclude each other; with an
// integer width, integer and number name the same primitive, and the two
// variants stay, for an emitter to keep apart (docs/emitters/targets/go/unions.md).
// Each variant reads what the position's folding allOf branches write, at the
// pointers they are written at, and stands where it is written, the Union
// where st says.
func lowerUnion(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer, list typeArray,
	fin finisher, st stand,
) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	types := list.types
	id := internNode(c, ts, st.pointer(pointer), func(common ir.TypeCommon) (ir.TypeDef, keySet) {
		list.voices = st.voices(c, s, pointer)
		var read keySet
		collect := finisher(func(_ ir.TypeDef, variant keySet) { read |= variant })
		variants := make([]ir.Variant, 0, len(types))
		for i, st := range types {
			vptr := pointer + ids.Ptr("type", strconv.Itoa(i))
			vid, shared, vDiags := lowerTypeVariant(c, ts, depth, s, vptr, st, list, collect)
			read |= shared
			diags = append(diags, vDiags...)
			variants = append(variants, ir.Variant{
				Name: compile.NamingHint(string(st)),
				Type: ir.TypeRef{Target: vid},
			})
		}
		return &ir.Union{
			TypeCommon: common,
			Variants:   variants,
			Exclusive:  typesDisjoint(types),
			Closed:     true,
		}, read
	}, fin)
	return id, diags
}

// typeArray is what every variant of one type array reads beside its schema: the
// types it lists, the voices of the position it is written at, and the mark an
// object or array variant's build writes, where the kind-inference policy read
// the types (lowerUntypedUnion).
type typeArray struct {
	types  []statement.Kind
	voices []annotation.Voice
	mark   finisher
}

// lowerTypeVariant lowers the variant of s's type array that st names, at
// pointer, as lowerTyped lowers st alone, but for what applies to another listed
// type (docs/compilers/openapi.md#type-arrays). An object variant holds no
// discriminator, which no $ref or mapping can name it by: the union keeps
// it. A variant reads the value bounds of the kinds of st the
// position admits (boundKinds), so a bound is held or kept, never both. A
// scalar one holding them, the format or the content vocabulary is a Scalar of
// its own, else the shared primitive, which returns what it read; fin
// finishes one built.
func lowerTypeVariant(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer, st statement.Kind,
	list typeArray, fin finisher,
) (ir.TypeID, keySet, []ir.Diagnostic) {
	takes := typeKinds(st) & boundKinds(c, s)
	switch st {
	case statement.KindObject:
		id, diags := lowerModel(c, ts, depth, s, pointer, false /* discriminated */, takes, list.mark.then(fin))
		return id, 0, diags
	case statement.KindArray:
		id, diags := lowerArray(c, ts, depth, s, pointer, list.mark.then(fin), stand{})
		return id, 0, diags
	default:
		shape := scalarShapeOf(s, st, variantFormat(s, st, list.types))
		if shape.shared && !statesBounds(c, s, list.voices, pointer, takes) {
			return ts.PrimID(shape.base), shape.read, nil
		}
		id, diags := hoistScalar(c, ts, depth, s, shape, takes, list.voices, pointer, fin, stand{})
		return id, 0, diags
	}
}

// variantFormat returns the format s writes, for its type array's variant of
// type st among types: the format where it pairs with st, or with no listed
// type, since nothing then says which type it describes, and a format validates
// only the instances of the types it is defined for (JSON Schema Validation
// 2020-12 §7.1); none where it pairs with another listed type.
func variantFormat(s *statement.Schema, st statement.Kind, types []statement.Kind) string {
	format := writtenFormat(s)
	if format == "" || pairs(st, format) {
		return format
	}
	if slices.ContainsFunc(types, func(t statement.Kind) bool { return pairs(t, format) }) {
		return ""
	}
	return format
}

// pairs reports whether format pairs with st: spelling.Lookup lists the
// pairing, or spelling.LookupEncoded does, as it does byte beside a string.
func pairs(st statement.Kind, format string) bool {
	key := string(st) + "/" + format
	_, listed := spelling.Lookup(key)
	_, encoded := spelling.LookupEncoded(key)
	return listed || encoded
}

// statesBounds reports whether s or one of voices, read at pointer, writes a
// value bound a reader of the kinds takes holds: one ConstraintsOf reads into a
// field or keeps, as an empty pattern or a keyword written null is not. It reads
// apart from the node, so what it reads is reported where hoistScalar reads it.
func statesBounds(c lowering.Ctx, s *statement.Schema, voices []annotation.Voice, pointer jsontext.Pointer, takes kindSet) bool {
	var kept ir.Unmodeled
	cons, _ := ConstraintsOf(c, &kept, s, voices, pointer, takes)
	return cons != nil || len(kept) > 0
}

// typesDisjoint reports whether no instance has two of types. A type listed
// twice overlaps itself, and JSON's types are otherwise disjoint but for one
// pair: every integer is a number.
func typesDisjoint(types []statement.Kind) bool {
	if len(slices.Compact(slices.Sorted(slices.Values(types)))) != len(types) {
		return false
	}
	return !slices.Contains(types, statement.KindInteger) || !slices.Contains(types, statement.KindNumber)
}
