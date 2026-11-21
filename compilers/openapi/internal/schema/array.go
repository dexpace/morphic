// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// lowerArray hoists an array schema, written at pointer, at at, as a Tuple when
// its `items` is a list its dialect reads as one (listedTuple), or when
// prefixItems is present, else a List over its item schema, each with the
// collection bounds it declares met with those foldedEntries write, fin
// finishing it once built.
func lowerArray(loweringContext lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, pointer, at jsontext.Pointer,
	foldedEntries []annotation.FoldedAllOfEntry, fin finisher,
) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	read := keyBit("items") | typeReadAs(schema, statement.KindArray) | collectionReads(schema)
	id := internNode(loweringContext, ts, at, func(common ir.TypeCommon) (ir.TypeDef, keySet) {
		cons := collectionBoundsMetWithFoldedEntries(ownCollectionConstraints(schema), foldedEntries)
		if n, tuple := listedTuple(loweringContext, schema); tuple {
			typeDef, tupleDiags := buildListedTuple(loweringContext, ts, depth, schema, common, pointer, n, cons)
			diags = append(diags, tupleDiags...)
			return typeDef, read | keySetOf("prefixItems", "additionalItems")
		}
		if prefix := schema.PrefixItems(); len(prefix) > 0 {
			typeDef, tupleDiags := buildTuple(loweringContext, ts, depth, schema, common, pointer, prefix, cons)
			diags = append(diags, tupleDiags...)
			return typeDef, read | keySetOf("prefixItems", "additionalItems")
		}
		element, elementDiags := Ref(loweringContext, ts, depth, loweringContext.Statements().Subschema(itemsSchema(schema)), pointer+ids.Ptr("items"))
		diags = append(diags, elementDiags...)
		return &ir.List{TypeCommon: common, ElementType: element, Constraints: cons}, read
	}, fin)
	return id, diags
}

// listedTuple returns how many schemas schema writes `items` as a list of where
// the rows of its dialect, read where schema is written (rulesOf) whatever
// pointer its node stands at, read that list as a tuple, as the drafts before
// 2020-12 spell one, and false where they do not or it writes no list. A list
// read so nowhere is the table's to report (docs/compilers/openapi.md#tuples).
func listedTuple(loweringContext lowering.Ctx, schema *statement.Schema) (int, bool) {
	n, listed := listedItems(schema)
	return n, listed && rulesOf(loweringContext, schema).Takes("items", specrules.List)
}

// listedItems returns how many schemas schema writes `items` as a list of, a
// tuple as the drafts before 2020-12 spell one, and false where it writes no
// list. The parser's model has no form for such a list, so only its node tells.
func listedItems(schema *statement.Schema) (int, bool) {
	node := keyNode(schema, "items")
	if node == nil || node.Kind != yaml.SequenceNode {
		return 0, false
	}
	return len(node.Content), true
}

// itemsSchema returns the one schema schema writes `items` as, and the zero
// Subschema, which names none, where it writes a list, a tuple, whose entries
// no reader of one items schema reads: the parser's model holds the false
// schema there.
func itemsSchema(schema *statement.Schema) statement.Subschema {
	if _, listed := listedItems(schema); listed || schema == nil || schema.Array == nil {
		return statement.Subschema{}
	}
	return schema.Array.Items
}

// buildListedTuple lowers the n schemas schema writes `items` as a list of into
// a Tuple's ElementTypes, the `additionalItems` beside them into its Rest, its
// Constraints being cons. The parser's model holds none of them, so each is the
// schema the load phase built at its position (reftable.Landing). An absent
// additionalItems leaves every element past them unconstrained, as `true` does,
// and `false` admits none. An entry or rest written as a number, a string or a
// list lowers as `false` does, and a `null` as one left out, as 2020-12's are,
// which the load phase reports.
func buildListedTuple(loweringContext lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, common ir.TypeCommon, pointer jsontext.Pointer, n int,
	cons *ir.Constraints,
) (ir.TypeDef, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	there, site := writtenAt(loweringContext, schema, pointer)
	elements := make([]ir.TypeRef, 0, n)
	for i := range n {
		entry := ids.Ptr("items", strconv.Itoa(i))
		ref, refDiags := Ref(loweringContext, ts, depth, heldAt(there, site+entry), pointer+entry)
		diags = append(diags, refDiags...)
		elements = append(elements, ref)
	}
	tuple := &ir.Tuple{TypeCommon: common, ElementTypes: elements, Constraints: cons}
	diags = append(diags, keepPrefixItems(loweringContext, &tuple.Unmodeled, schema, pointer)...)
	rest := ids.Ptr("additionalItems")
	if js := heldAt(there, site+rest); !js.Truth.ReadsFalse() {
		ref, restDiags := Ref(loweringContext, ts, depth, js, pointer+rest)
		diags = append(diags, restDiags...)
		tuple.Rest = &ref
	}
	return tuple, diags
}

// keepPrefixItems keeps on unmodeled, verbatim, the prefixItems schema writes
// beside the items list its dialect reads as the tuple, which is the dialect's
// own spelling of one, where no text makes the two one tuple
// (docs/compilers/openapi.md#tuples), and reports it once.
func keepPrefixItems(loweringContext lowering.Ctx, unmodeled *ir.Unmodeled, schema *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	if !hasKey(schema, "prefixItems") {
		return nil
	}
	kept, diags := annotation.PreserveSchemaKeyword(unmodeled, schema, "prefixItems", ir.ReasonDegradedLowering, pointer, loweringContext.Place())
	if kept {
		diags = append(diags, loweringContext.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer+ids.Ptr("prefixItems"),
			"prefixItems is kept verbatim: this schema's dialect reads its items list as the tuple"))
	}
	return diags
}

// writtenAt returns where schema, lowered at pointer, is written: where the
// bindings place it (placedAt), as a type list lowers each of its variants at
// a pointer of its own, and else pointer of loweringContext's document.
func writtenAt(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) (lowering.Ctx, jsontext.Pointer) {
	if there, at, placed := placedAt(loweringContext, schema); placed {
		return there, at
	}
	return loweringContext, pointer
}

// ListedTuple returns the entries the array js writes as a tuple in a list of
// `items`, its $ref followed, where the dialect of where it is reads that list
// so (listedTuple), and the `additionalItems` beside them, Unwritten where
// none is written: the schemas the load phase built there, as the parser's
// model holds neither. It is false where js is no such tuple, or the bindings
// place it in no document the compile read.
func ListedTuple(loweringContext lowering.Ctx, js *statement.Statement) (
	entries []*statement.Statement, rest *statement.Statement, ok bool,
) {
	if js.Ref != nil {
		if end, ok := runEnd(loweringContext, js); ok && end.Held() {
			js = end
		}
	}
	at, placed := siteOf(js.Schema)
	there, read := lowering.InDocument(loweringContext, at.Doc)
	if !placed || !read {
		return nil, nil, false
	}
	n, tuple := listedTuple(there, js.Schema)
	if !tuple {
		return nil, nil, false
	}
	entries = make([]*statement.Statement, 0, n)
	for i := range n {
		entries = append(entries, heldAt(there, at.Pointer+ids.Ptr("items", strconv.Itoa(i))))
	}
	return entries, heldAt(there, at.Pointer+ids.Ptr("additionalItems")), true
}

// heldAt returns the statement of the schema the bindings hold at pointer of
// loweringContext's document, the one the parser modelled or the load phase
// built there, and an Unwritten one where they hold none.
func heldAt(loweringContext lowering.Ctx, pointer jsontext.Pointer) *statement.Statement {
	return loweringContext.Statements().At(loweringContext.Position(pointer))
}

// buildTuple lowers prefixItems into a Tuple's ElementTypes, the items beside
// them into its Rest, its Constraints being cons.
//
// 2020-12 applies items to every element past prefixItems, so `items: false`
// leaves no Rest. Any other lowers as a List's items does, under the same name,
// since /items is named alike whichever node holds it (lowering.NameAt). An absent one
// lowers to the top type as `items: true` does, though only the latter makes an
// unevaluatedItems beside it inert. No field is derived from another:
// ElementTypes is never trimmed to maxItems, and a closed tuple gains no
// maxItems.
func buildTuple(loweringContext lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, common ir.TypeCommon, pointer jsontext.Pointer, prefix []statement.Subschema,
	cons *ir.Constraints,
) (ir.TypeDef, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	elements := make([]ir.TypeRef, 0, len(prefix))
	for i, ps := range prefix {
		ref, refDiags := Ref(loweringContext, ts, depth, loweringContext.Statements().Subschema(ps), pointer+ids.Ptr("prefixItems", strconv.Itoa(i)))
		diags = append(diags, refDiags...)
		elements = append(elements, ref)
	}
	tuple := &ir.Tuple{TypeCommon: common, ElementTypes: elements, Constraints: cons}
	if items := loweringContext.Statements().Subschema(itemsSchema(schema)); !items.Truth.ReadsFalse() {
		rest, restDiags := Ref(loweringContext, ts, depth, items, pointer+ids.Ptr("items"))
		diags = append(diags, restDiags...)
		tuple.Rest = &rest
	}
	return tuple, diags
}

// ownCollectionConstraints reads the collection bounds s itself writes, which
// bound the whole instance whether it lowers to a List or a Tuple, and nil
// where it states none. Only the integer and boolean bounds are read here, as
// the statement holds the model's, each count where the model holds the
// number its literal states (annotation.ExactCount); one it does not
// is kept unread (keptCount).
func ownCollectionConstraints(schema *statement.Schema) *ir.Constraints {
	array := schema.ArrayKeywords()
	minItems, maxItems := annotation.ExactCount(schema, "minItems", array.MinItems), annotation.ExactCount(schema, "maxItems", array.MaxItems)
	if minItems == nil && maxItems == nil && array.UniqueItems == nil {
		return nil
	}
	cons := &ir.Constraints{MinItems: minItems, MaxItems: maxItems}
	if array.UniqueItems != nil {
		cons.UniqueItems = *array.UniqueItems
	}
	return cons
}

// missingArrayItems reports a 3.0 schema that writes type array and no items,
// which 3.0 requires and 2020-12 leaves optional (GitHub #650). The lowering
// is alike in every dialect, so only this tells the document.
//
// It reads the declaration, never what it lowered to, so an array lowering to
// a tuple or an enum is held to the rule as a List is, a 3.0 prefixItems
// among them (docs/compilers/openapi.md#later-version-construct). `items: {}`
// is silent, as is a list, read off the raw node, which no documented parser
// behaviour models. missingArrayItemsConjoined reads past a schema's
// conjuncts; a subschema kept verbatim never reaches it.
func missingArrayItems(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	if !slices.Contains(schema.Types(), statement.KindArray) {
		return nil
	}
	if _, listed := listedItems(schema); listed || written(schema.ArrayKeywords().Items) {
		return nil
	}
	if !loweringContext.SchemaRules().RequiredWhen("items", "type", string(statement.KindArray)) {
		return nil
	}
	return []ir.Diagnostic{loweringContext.DiagAt(ir.SeverityWarning, diag.MissingArrayItems, pointer,
		"OpenAPI 3.0 requires items when type is array, and this schema writes none, "+
			"so only its other keywords constrain its elements")}
}

// missingArrayItemsConjoined is missingArrayItems for a schema writing
// conjuncts beside its keywords (writesConjuncts), a $ref or an allOf, which
// conjoin with them: a type array there leaves its elements to them, so it is
// reported only where admission's reading of the conjunction holds them to no
// items, which neither items: true nor a union's branches do. An items the
// reading found holds however much of the rest it read, and one cut short
// before reaching any leaves the keywords held to the rule as they are
// written.
func missingArrayItemsConjoined(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	diags := missingArrayItems(loweringContext, schema, pointer)
	if len(diags) == 0 || !writesConjuncts(schema) {
		return diags
	}
	admissionReader := newAdmissionReader(loweringContext)
	if admissionReader.of(loweringContext.Statements().Read(schema)).items != nil {
		return nil
	}
	return diags
}
