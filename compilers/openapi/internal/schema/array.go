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
// unconstrained, as `true` does, and `false` admits none.
func buildListedTuple(c lowering.Ctx, ts *compile.Types, depth int, s *oas3.Schema, common ir.TypeCommon, pointer jsontext.Pointer, n int) (ir.TypeDef, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	elems := make([]ir.TypeRef, 0, n)
	for i := range n {
		at := pointer + ids.Ptr("items", strconv.Itoa(i))
		ref, refDiags := Ref(c, ts, depth, heldAt(c, at), at)
		diags = append(diags, refDiags...)
		elems = append(elems, ref)
	}
	t := &ir.Tuple{TypeCommon: common, Elems: elems, Constraints: collectionConstraints(s)}
	at := pointer + ids.Ptr("additionalItems")
	if rest := heldAt(c, at); !annotation.IsFalseSchema(rest) {
		ref, restDiags := Ref(c, ts, depth, rest, at)
		diags = append(diags, restDiags...)
		t.Rest = &ref
	}
	return t, diags
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
// is the same in every dialect, so this is the only place the document is told.
//
// It reads the declaration, never what it lowered to, so an array that lowers
// to a tuple, an enum or an allOf is held to the rule as a List is; that a 3.0
// prefixItems lowers at all is GitHub #820. `items: {}` is silent. A $ref
// site's keywords are held to it; a subschema kept verbatim, such as one under
// `not`, never reaches it.
func missingArrayItems(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	if annotation.Written(s.GetItems()) || !slices.Contains(s.GetType(), oas3.SchemaTypeArray) ||
		!c.SchemaRules().RequiredWhen("items", "type", string(oas3.SchemaTypeArray)) {
		return nil
	}
	return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.MissingArrayItems, pointer,
		"OpenAPI 3.0 requires items when type is array, and this schema writes none, "+
			"so only its other keywords constrain its elements")}
}
