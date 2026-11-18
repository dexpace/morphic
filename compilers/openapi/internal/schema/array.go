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
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// lowerArray hoists an array schema as a Tuple when its `items` is a list its
// dialect reads as one (listedTuple), or when prefixItems is present, else a
// List over its item schema, each with the collection bounds it declares, fin
// finishing it once built.
func lowerArray(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer, fin finisher,
) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	read := keyBit("items") | typeReadAs(s, statement.KindArray) | collectionReads(s)
	id := internNode(c, ts, pointer, func(common ir.TypeCommon) (ir.TypeDef, keySet) {
		if n, tuple := listedTuple(c, s, pointer); tuple {
			t, tupleDiags := buildListedTuple(c, ts, depth, s, common, pointer, n)
			diags = append(diags, tupleDiags...)
			return t, read | keySetOf("prefixItems", "additionalItems")
		}
		if prefix := s.PrefixItems(); len(prefix) > 0 {
			t, tupleDiags := buildTuple(c, ts, depth, s, common, pointer, prefix)
			diags = append(diags, tupleDiags...)
			return t, read | keySetOf("prefixItems", "additionalItems")
		}
		elem, elemDiags := Ref(c, ts, depth, c.Statements().Sub(itemsSchema(s)), pointer+ids.Ptr("items"))
		diags = append(diags, elemDiags...)
		return &ir.List{
			TypeCommon:  common,
			Elem:        elem,
			Constraints: collectionConstraints(c, s, pointer),
		}, read
	}, fin)
	return id, diags
}

// listedTuple returns how many schemas s, at pointer, writes `items` as a list
// of where the rows of its dialect read that list as a tuple, as the drafts
// before 2020-12 spell one, and false where they do not or it writes no list.
// A list read so nowhere is the table's to report
// (docs/compilers/openapi.md#tuples).
func listedTuple(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) (int, bool) {
	n, listed := listedItems(s)
	return n, listed && c.Rules().At(c.Position(pointer)).Takes("items", oas.List)
}

// listedItems returns how many schemas s writes `items` as a list of, a tuple
// as the drafts before 2020-12 spell one, and false where it writes no list.
// The parser's model has no form for such a list, so only its node tells.
func listedItems(s *statement.Schema) (int, bool) {
	n := keyNode(s, "items")
	if n == nil || n.Kind != yaml.SequenceNode {
		return 0, false
	}
	return len(n.Content), true
}

// itemsSchema returns the one schema s writes `items` as, and the zero Sub,
// which names none, where it writes a list, a tuple, whose entries no reader
// of one items schema reads: the parser's model holds the false schema there.
func itemsSchema(s *statement.Schema) statement.Sub {
	if _, listed := listedItems(s); listed || s == nil || s.Array == nil {
		return statement.Sub{}
	}
	return s.Array.Items
}

// buildListedTuple lowers the n schemas s writes `items` as a list of into a
// Tuple's Elems, the `additionalItems` beside them into its Rest, and the
// collection bounds into its Constraints. The parser's model holds none of
// them, so each is the schema the load phase built at its position
// (bind.Landing). An absent additionalItems leaves every element past them
// unconstrained, as `true` does, and `false` admits none. An entry or rest
// written as a number, a string or a list lowers as `false` does, and a `null`
// as one left out, as 2020-12's are, which the load phase reports.
func buildListedTuple(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, common ir.TypeCommon, pointer jsontext.Pointer, n int) (ir.TypeDef, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	there, site := writtenAt(c, s, pointer)
	elems := make([]ir.TypeRef, 0, n)
	for i := range n {
		entry := ids.Ptr("items", strconv.Itoa(i))
		ref, refDiags := Ref(c, ts, depth, heldAt(there, site+entry), pointer+entry)
		diags = append(diags, refDiags...)
		elems = append(elems, ref)
	}
	t := &ir.Tuple{TypeCommon: common, Elems: elems, Constraints: collectionConstraints(c, s, pointer)}
	diags = append(diags, keepPrefixItems(c, &t.Unmodeled, s, pointer)...)
	rest := ids.Ptr("additionalItems")
	if js := heldAt(there, site+rest); !js.Truth.ReadsFalse() {
		ref, restDiags := Ref(c, ts, depth, js, pointer+rest)
		diags = append(diags, restDiags...)
		t.Rest = &ref
	}
	return t, diags
}

// keepPrefixItems keeps on p, verbatim, the prefixItems s writes beside the
// items list its dialect reads as the tuple, which is the dialect's own
// spelling of one, where no text makes the two one tuple
// (docs/compilers/openapi.md#tuples), and reports it once.
func keepPrefixItems(c lowering.Ctx, p *ir.Unmodeled, s *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	if !hasKey(s, "prefixItems") {
		return nil
	}
	kept, diags := annotation.PreserveSchemaKeyword(p, s, "prefixItems", ir.ReasonDegradedLowering, pointer, c.Place())
	if kept {
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer+ids.Ptr("prefixItems"),
			"prefixItems is kept verbatim: this schema's dialect reads its items list as the tuple"))
	}
	return diags
}

// writtenAt returns where s, lowered at pointer, is written: where the
// bindings place it (placedAt), as a type list lowers each of its variants at
// a pointer of its own, and else pointer of c's document.
func writtenAt(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) (lowering.Ctx, jsontext.Pointer) {
	if there, at, placed := placedAt(c, s); placed {
		return there, at
	}
	return c, pointer
}

// ListedTuple returns the entries the array js writes as a tuple in a list of
// `items`, its $ref followed, where the dialect of where it is reads that list
// so (listedTuple), and the `additionalItems` beside them, Unwritten where
// none is written: the schemas the load phase built there, as the parser's
// model holds neither. It is false where js is no such tuple, or the bindings
// place it in no document the compile read.
func ListedTuple(c lowering.Ctx, js *statement.Statement) (
	entries []*statement.Statement, rest *statement.Statement, ok bool,
) {
	if js.Ref != nil {
		if end, ok := runEnd(c, js); ok && end.Held() {
			js = end
		}
	}
	at, placed := siteOf(js.Schema)
	there, read := lowering.InDocument(c, at.Doc)
	if !placed || !read {
		return nil, nil, false
	}
	n, tuple := listedTuple(there, js.Schema, at.Pointer)
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
// c's document, the one the parser modelled or the load phase built there,
// and an Unwritten one where they hold none.
func heldAt(c lowering.Ctx, pointer jsontext.Pointer) *statement.Statement {
	return c.Statements().At(c.Position(pointer))
}

// buildTuple lowers prefixItems into a Tuple's Elems, the items beside them
// into its Rest, and the collection bounds into its Constraints.
//
// 2020-12 applies items to every element past prefixItems, so `items: false`
// leaves no Rest. Any other lowers as a List's items does, under the same name,
// since /items is named alike whichever node holds it (lowering.NameAt). An absent one
// lowers to the top type as `items: true` does, though only the latter makes an
// unevaluatedItems beside it inert. No field is derived from another: Elems is
// never trimmed to maxItems, and a closed tuple gains no maxItems.
func buildTuple(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, common ir.TypeCommon, pointer jsontext.Pointer, prefix []statement.Sub) (ir.TypeDef, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	elems := make([]ir.TypeRef, 0, len(prefix))
	for i, ps := range prefix {
		ref, refDiags := Ref(c, ts, depth, c.Statements().Sub(ps), pointer+ids.Ptr("prefixItems", strconv.Itoa(i)))
		diags = append(diags, refDiags...)
		elems = append(elems, ref)
	}
	t := &ir.Tuple{TypeCommon: common, Elems: elems, Constraints: collectionConstraints(c, s, pointer)}
	if items := c.Statements().Sub(itemsSchema(s)); !items.Truth.ReadsFalse() {
		rest, restDiags := Ref(c, ts, depth, items, pointer+ids.Ptr("items"))
		diags = append(diags, restDiags...)
		t.Rest = &rest
	}
	return t, diags
}

// collectionConstraints reads the collection bounds of an array schema s, lowered
// at pointer, which bound the whole instance whether it lowers to a List or a
// Tuple, met with those the branches of its allOf write for it (positionVoices).
// Only the integer and boolean bounds are read here, as the statement holds the
// model's, each count where the model holds the number its literal states
// (annotation.ExactCount); the census keeps one it does not (keptCount).
func collectionConstraints(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) *ir.Constraints {
	return voicedCollectionBounds(ownCollectionConstraints(s), positionVoices(c, s, pointer, ir.TypeRef{}))
}

// ownCollectionConstraints reads the collection bounds s itself writes, and nil
// where it states none.
func ownCollectionConstraints(s *statement.Schema) *ir.Constraints {
	array := s.ArrayKeywords()
	minItems, maxItems := annotation.ExactCount(s, "minItems", array.MinItems), annotation.ExactCount(s, "maxItems", array.MaxItems)
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
func missingArrayItems(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	if !slices.Contains(s.Types(), statement.KindArray) {
		return nil
	}
	if _, listed := listedItems(s); listed || written(s.ArrayKeywords().Items) {
		return nil
	}
	if !c.SchemaRules().RequiredWhen("items", "type", string(statement.KindArray)) {
		return nil
	}
	return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.MissingArrayItems, pointer,
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
func missingArrayItemsConjoined(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	diags := missingArrayItems(c, s, pointer)
	if len(diags) == 0 || !writesConjuncts(s) {
		return diags
	}
	w := newAdmissionReader(c)
	if w.of(c.Statements().Read(s)).items != nil {
		return nil
	}
	return diags
}
