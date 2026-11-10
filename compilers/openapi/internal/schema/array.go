// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/ir"
)

// lowerArray hoists an array schema as a Tuple when its `items` is a list its
// dialect reads as one (listedTuple), or when prefixItems is present, else a
// List over its item schema, each with the collection bounds it declares.
func lowerArray(c lowering.Ctx, ts *compile.Types, depth int, s *oas3.Schema, pointer jsontext.Pointer) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, func(common ir.TypeCommon) ir.TypeDef {
		if n, tuple := listedTuple(c, s, pointer); tuple {
			t, tupleDiags := buildListedTuple(c, ts, depth, s, common, pointer, n)
			diags = append(diags, tupleDiags...)
			return t
		}
		if prefix := s.GetPrefixItems(); len(prefix) > 0 {
			t, tupleDiags := buildTuple(c, ts, depth, s, common, pointer, prefix)
			diags = append(diags, tupleDiags...)
			return t
		}
		elem, elemDiags := Ref(c, ts, depth, itemsSchema(s), pointer+ids.Ptr("items"))
		diags = append(diags, elemDiags...)
		return &ir.List{
			TypeCommon:  common,
			Elem:        elem,
			Constraints: collectionConstraints(s),
		}
	})
	return id, diags
}

// listedTuple returns how many schemas s, at pointer, writes `items` as a list
// of where the rows of its dialect read that list as a tuple, as the drafts
// before 2020-12 spell one, and false where they do not or it writes no list.
// A list read so nowhere is the load phase's to report (load.ownedFinding).
func listedTuple(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) (int, bool) {
	n, listed := listedItems(s)
	return n, listed && lowering.SchemaRulesAt(c, pointer).Takes("items", oas.List)
}

// listedItems returns how many schemas s writes `items` as a list of, a tuple
// as the drafts before 2020-12 spell one, and false where it writes no list.
// The parser's model has no form for such a list, so only its node tells.
func listedItems(s *oas3.Schema) (int, bool) {
	n := annotation.RawPropertyNode(s, "items")
	if n == nil || n.Kind != yaml.SequenceNode {
		return 0, false
	}
	return len(n.Content), true
}

// itemsSchema returns the one schema s writes `items` as, and nil where it
// writes a list, a tuple, whose entries no reader of one items schema reads:
// the parser's model holds the false schema there.
func itemsSchema(s *oas3.Schema) *oas3.JSONSchema[oas3.Referenceable] {
	if _, listed := listedItems(s); listed {
		return nil
	}
	return s.GetItems()
}

// buildListedTuple lowers the n schemas s writes `items` as a list of into a
// Tuple's Elems, the `additionalItems` beside them into its Rest, and the
// collection bounds into its Constraints. The parser's model holds none of
// them, so each is the schema the load phase built at its position
// (bind.Landing). An absent additionalItems leaves every element past them
// unconstrained, as `true` does, and `false` admits none. An entry or rest
// written as a number, a string or a list lowers as `false` does, and a `null`
// as one left out, as 2020-12's are, which the load phase reports.
func buildListedTuple(c lowering.Ctx, ts *compile.Types, depth int, s *oas3.Schema, common ir.TypeCommon, pointer jsontext.Pointer, n int) (ir.TypeDef, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	there, site := writtenAt(c, s, pointer)
	elems := make([]ir.TypeRef, 0, n)
	for i := range n {
		entry := ids.Ptr("items", strconv.Itoa(i))
		ref, refDiags := Ref(c, ts, depth, heldAt(there, site+entry), pointer+entry)
		diags = append(diags, refDiags...)
		elems = append(elems, ref)
	}
	t := &ir.Tuple{TypeCommon: common, Elems: elems, Constraints: collectionConstraints(s)}
	rest := ids.Ptr("additionalItems")
	if js := heldAt(there, site+rest); !annotation.IsFalseSchema(js) {
		ref, restDiags := Ref(c, ts, depth, js, pointer+rest)
		diags = append(diags, restDiags...)
		t.Rest = &ref
	}
	return t, diags
}

// writtenAt returns where s, lowered at pointer, is written: where the
// bindings place it (placedAt), as a type list lowers each of its variants at
// a pointer of its own, and else pointer of c's document.
func writtenAt(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) (lowering.Ctx, jsontext.Pointer) {
	if there, at, placed := placedAt(c, s); placed {
		return there, at
	}
	return c, pointer
}

// ListedTuple returns the entries the array js writes as a tuple in a list of
// `items`, its $ref followed, where the dialect of where it is reads that list
// so (listedTuple), and the `additionalItems` beside them, nil where none is
// written: the schemas the load phase built there, as the parser's model holds
// neither. It is false where js is no such tuple, or the bindings place it in
// no document the compile read.
func ListedTuple(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable]) (
	entries []*oas3.JSONSchema[oas3.Referenceable], rest *oas3.JSONSchema[oas3.Referenceable], ok bool,
) {
	b := c.Bindings()
	if end := b.End(js); end != nil {
		js = end
	}
	at, placed := b.Site(js)
	there, read := lowering.InDocument(c, at.Doc)
	if !placed || !read {
		return nil, nil, false
	}
	n, tuple := listedTuple(there, js.GetSchema(), at.Pointer)
	if !tuple {
		return nil, nil, false
	}
	entries = make([]*oas3.JSONSchema[oas3.Referenceable], 0, n)
	for i := range n {
		entries = append(entries, heldAt(there, at.Pointer+ids.Ptr("items", strconv.Itoa(i))))
	}
	return entries, heldAt(there, at.Pointer+ids.Ptr("additionalItems")), true
}

// heldAt returns the schema the bindings hold at pointer of c's document, the
// one the parser modelled or the load phase built there, and nil where they
// hold none or c reads another document's content at the source's positions.
func heldAt(c lowering.Ctx, pointer jsontext.Pointer) *oas3.JSONSchema[oas3.Referenceable] {
	at, ok := c.Position(pointer)
	if !ok {
		return nil
	}
	return c.Bindings().At(at)
}

// buildTuple lowers prefixItems into a Tuple's Elems, the items beside them
// into its Rest, and the collection bounds into its Constraints.
//
// 2020-12 applies items to every element past prefixItems, so `items: false`
// leaves no Rest. Any other lowers as a List's items does, under the same name,
// since /items is named alike whichever node holds it (lowering.NameAt). An absent one
// lowers to the top type as `items: true` does, though only the latter makes an
// unevaluatedItems beside it inert (#862). No field is derived from another:
// Elems is never trimmed to maxItems, and a closed tuple gains no maxItems.
func buildTuple(c lowering.Ctx, ts *compile.Types, depth int, s *oas3.Schema, common ir.TypeCommon, pointer jsontext.Pointer, prefix []*oas3.JSONSchema[oas3.Referenceable]) (ir.TypeDef, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	elems := make([]ir.TypeRef, 0, len(prefix))
	for i, ps := range prefix {
		ref, refDiags := Ref(c, ts, depth, ps, pointer+ids.Ptr("prefixItems", strconv.Itoa(i)))
		diags = append(diags, refDiags...)
		elems = append(elems, ref)
	}
	t := &ir.Tuple{TypeCommon: common, Elems: elems, Constraints: collectionConstraints(s)}
	if items := itemsSchema(s); !annotation.IsFalseSchema(items) {
		rest, restDiags := Ref(c, ts, depth, items, pointer+ids.Ptr("items"))
		diags = append(diags, restDiags...)
		t.Rest = &rest
	}
	return t, diags
}

// collectionConstraints reads an array schema's collection bounds, which bound
// the whole instance whether it lowers to a List or a Tuple. Only the integer
// and boolean bounds are read here, from the parser's model, which holds an
// integer past int64 as 0 (GitHub #1050); numeric-value bounds go through raw
// nodes elsewhere to avoid the float64 trap.
func collectionConstraints(s *oas3.Schema) *ir.Constraints {
	if s.MinItems == nil && s.MaxItems == nil && s.UniqueItems == nil {
		return nil
	}
	c := &ir.Constraints{MinItems: s.MinItems, MaxItems: s.MaxItems}
	if s.UniqueItems != nil {
		c.UniqueItems = *s.UniqueItems
	}
	return c
}

// missingArrayItems reports a 3.0 schema that writes type array and no items,
// which 3.0 requires and 2020-12 leaves optional (GitHub #650). The lowering
// is alike in every dialect, so only this tells the document.
//
// It reads the declaration, never what it lowered to, so an array lowering to
// a tuple or an enum is held to the rule as a List is; that a 3.0 prefixItems
// lowers at all is GitHub #820. `items: {}` is silent, as is a list, read off
// the raw node, which no documented parser behaviour models.
// missingArrayItemsBesideRef holds a $ref site's keywords to it; a subschema
// kept verbatim never reaches it.
func missingArrayItems(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	_, listed := listedItems(s)
	if listed || annotation.Written(s.GetItems()) || !slices.Contains(s.GetType(), oas3.SchemaTypeArray) {
		return nil
	}
	if !c.SchemaRules().RequiredWhen("items", "type", string(oas3.SchemaTypeArray)) {
		return nil
	}
	return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.MissingArrayItems, pointer,
		"OpenAPI 3.0 requires items when type is array, and this schema writes none, "+
			"so only its other keywords constrain its elements")}
}

// missingArrayItemsBesideRef is missingArrayItems for the keywords beside the
// $ref js writes, which conjoin with what it lands on: a type array there
// leaves its elements to that, so it is reported only where admission's
// reading of the conjunction holds them to no items, which neither items: true
// nor a union's branches do. An items the reading found holds however much of
// the rest it read, and one cut short before reaching any leaves the keywords
// held to the rule as they are written.
func missingArrayItemsBesideRef(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable], pointer jsontext.Pointer) []ir.Diagnostic {
	diags := missingArrayItems(c, js.GetSchema(), pointer)
	if len(diags) == 0 {
		return nil
	}
	w := newAdmission(c)
	if w.whole(js).items != nil {
		return nil
	}
	return diags
}
