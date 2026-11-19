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
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/compilers/openapi/internal/typeformat"
	"github.com/dexpace/morphic/ir"
)

// lowerUnion hoists a multi-typed schema (e.g. type: [string, integer]) as an
// untagged union with one variant per declared type, exclusive unless two of
// them overlap (typesDisjoint). A type array asks for one of its types, not for
// exactly one, so overlapping variants must not exclude each other; with an
// integer width, integer and number name the same primitive, and the two
// variants stay, for an emitter to keep apart (docs/emitters/targets/go/unions.md).
// The Union stands where st says, and each variant, which no source pointer
// names, under it at /type/<i>, built from what the Union reads beside schema.
func lowerUnion(loweringContext lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, pointer jsontext.Pointer, list typeArray,
	fin finisher, st stand,
) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	types := list.types
	at := st.pointer(pointer)
	id := internNode(loweringContext, ts, at, func(common ir.TypeCommon) (ir.TypeDef, keySet) {
		list.foldedBranches = st.foldedBranches(loweringContext, schema, pointer)
		list.held = st.heldWhole(loweringContext, pointer)
		var read keySet
		collect := finisher(func(_ ir.TypeDef, variant keySet, _ *principalKept) { read |= variant })
		variants := make([]ir.Variant, 0, len(types))
		for i, st := range types {
			vptr := at + ids.Ptr("type", strconv.Itoa(i))
			vid, shared, vDiags := lowerTypeVariant(loweringContext, ts, depth, schema, vptr, st, list, collect)
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

// typeArray is what every variant of one type array reads beside its schema:
// the types it lists, the folded branches of the position it is written at, and
// the mark an object or array variant's build writes, where the kind-inference
// policy read the types (lowerUntypedUnion).
type typeArray struct {
	types          []statement.JSONSchemaTypeName
	foldedBranches []annotation.FoldedAllOfBranch
	mark           finisher
	// held says the position keeps the principal writing the list whole, so a
	// variant keeps none of its bounds verbatim (stand.held).
	held bool
}

// lowerTypeVariant lowers the variant of schema's type array that st names, at
// pointer, as lowerTyped lowers st alone, but for what applies to another
// listed type (docs/compilers/openapi.md#type-arrays). An object variant holds
// no discriminator, which no $ref or mapping can name it by: the union keeps
// it. A variant reads, of schema and list's folded branches, the value bounds
// of the kinds of st the position admits (boundKinds), an array one the
// collection bounds. A scalar one holding them, the format or the content
// vocabulary is its own Scalar, else the shared primitive, returning what it
// read; fin finishes one built.
func lowerTypeVariant(loweringContext lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, pointer jsontext.Pointer, st statement.JSONSchemaTypeName,
	list typeArray, fin finisher,
) (ir.TypeID, keySet, []ir.Diagnostic) {
	takes := typeKinds(st) & boundKinds(loweringContext, schema)
	switch st {
	case statement.KindObject:
		id, diags := lowerModel(loweringContext, ts, depth, schema, pointer, false /* discriminated */, takes, list.mark.then(fin))
		return id, 0, diags
	case statement.KindArray:
		id, diags := lowerArray(loweringContext, ts, depth, schema, pointer, pointer, list.foldedBranches, list.mark.then(fin))
		return id, 0, diags
	default:
		shape := scalarShapeOf(schema, st, variantFormat(schema, st, list.types))
		if shape.shared && !statesBounds(loweringContext, schema, list.foldedBranches, pointer, takes) {
			return ts.PrimitiveID(shape.base), shape.read, nil
		}
		id, diags := hoistScalar(loweringContext, ts, depth, schema, shape, takes, list.foldedBranches, pointer, fin, stand{held: list.held})
		return id, 0, diags
	}
}

// variantFormat returns the format schema writes, for its type array's variant
// of type st among types: the format where it pairs with st, or with no listed
// type, since nothing then says which type it describes, and a format validates
// only the instances of the types it is defined for (JSON Schema Validation
// 2020-12 §7.1); none where it pairs with another listed type.
func variantFormat(schema *statement.Schema, st statement.JSONSchemaTypeName, types []statement.JSONSchemaTypeName) string {
	format := writtenFormat(schema)
	if format == "" || pairs(st, format) {
		return format
	}
	if slices.ContainsFunc(types, func(jsonSchemaTypeName statement.JSONSchemaTypeName) bool { return pairs(jsonSchemaTypeName, format) }) {
		return ""
	}
	return format
}

// pairs reports whether format pairs with st: typeformat.Lookup lists the
// pairing, or typeformat.LookupEncoded does, as it does byte beside a string.
func pairs(st statement.JSONSchemaTypeName, format string) bool {
	key := string(st) + "/" + format
	_, listed := typeformat.Lookup(key)
	_, encoded := typeformat.LookupEncoded(key)
	return listed || encoded
}

// statesBounds reports whether schema or one of foldedBranches, read at
// pointer, writes a value bound a reader of the kinds takes holds: one
// ConstraintsOf reads into a field or keeps, as an empty pattern or a keyword
// written null is not. It reads apart from the node, so what it reads is
// reported where hoistScalar reads it.
func statesBounds(loweringContext lowering.Ctx, schema *statement.Schema, foldedBranches []annotation.FoldedAllOfBranch, pointer jsontext.Pointer, takes kindSet) bool {
	var kept ir.Unmodeled
	cons, _ := ConstraintsOf(loweringContext, &kept, schema, foldedBranches, pointer, takes)
	return cons != nil || len(kept) > 0
}

// typesDisjoint reports whether no instance has two of types. A type listed
// twice overlaps itself, and JSON's types are otherwise disjoint but for one
// pair: every integer is a number.
func typesDisjoint(types []statement.JSONSchemaTypeName) bool {
	if len(slices.Compact(slices.Sorted(slices.Values(types)))) != len(types) {
		return false
	}
	return !slices.Contains(types, statement.KindInteger) || !slices.Contains(types, statement.KindNumber)
}
