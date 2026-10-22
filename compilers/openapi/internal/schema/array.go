package schema

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// lowerArray hoists an array schema as a Tuple when prefixItems is present, else
// a List over its item schema, each with the collection bounds it declares.
func lowerArray(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer jsontext.Pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		if prefix := s.GetPrefixItems(); len(prefix) > 0 {
			t, tupleDiags := buildTuple(c, ts, anchors, depth, s, common, pointer, hint, prefix)
			diags = append(diags, tupleDiags...)
			return t
		}
		elem, elemDiags := Ref(c, ts, anchors, depth, s.GetItems(), pointer+ids.Ptr("items"), compile.SubHint(hint, "item"))
		diags = append(diags, elemDiags...)
		return &ir.List{
			TypeCommon:  common,
			Elem:        elem,
			Constraints: collectionConstraints(s),
		}
	})
	return id, diags
}

// buildTuple lowers prefixItems into a Tuple's Elems, the items beside them
// into its Rest, and the collection bounds into its Constraints.
//
// 2020-12 applies items to every element past prefixItems, so `items: false`
// leaves no Rest. Any other lowers as a List's items does, under the same hint,
// since positionHint names /items alike whichever node holds it. An absent one
// lowers to the top type as `items: true` does, though only the latter makes an
// unevaluatedItems beside it inert (#862). No field is derived from another:
// Elems is never trimmed to maxItems, and a closed tuple gains no maxItems.
func buildTuple(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, common ir.TypeCommon, pointer jsontext.Pointer, hint string, prefix []*oas3.JSONSchema[oas3.Referenceable]) (ir.TypeDef, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	elems := make([]ir.TypeRef, 0, len(prefix))
	for i, ps := range prefix {
		ref, refDiags := Ref(c, ts, anchors, depth, ps, pointer+ids.Ptr("prefixItems", strconv.Itoa(i)), compile.SubHint(hint, strconv.Itoa(i)))
		diags = append(diags, refDiags...)
		elems = append(elems, ref)
	}
	t := &ir.Tuple{TypeCommon: common, Elems: elems, Constraints: collectionConstraints(s)}
	if items := s.GetItems(); !annotation.IsFalseSchema(items) {
		rest, restDiags := Ref(c, ts, anchors, depth, items, pointer+ids.Ptr("items"), compile.SubHint(hint, "item"))
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
// prefixItems lowers at all is GitHub #820. `items: {}` is silent. Neither a
// $ref site, whose siblings 3.0 ignores, nor a subschema kept verbatim, such as
// one under `not`, reaches it.
func missingArrayItems(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	if annotation.Written(s.GetItems()) || !slices.Contains(s.GetType(), oas3.SchemaTypeArray) || !c.Is30() {
		return nil
	}
	return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.MissingArrayItems, pointer,
		"OpenAPI 3.0 requires items when type is array, and this schema writes none, "+
			"so only its other keywords constrain its elements")}
}
