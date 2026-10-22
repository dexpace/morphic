package schema

import (
	"encoding/json/jsontext"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// Keeping a construct verbatim is annotation's job, and this package calls it
// directly with c.ProvenanceAt. A preservation function is defined here only
// when its decision needs what the lowering holds: PreserveUnknownKeywords reads
// the dialect a schema is read in, which annotation sits below and cannot ask.
// One that needs nothing of the lowering but a provenance belongs in
// annotation, or the same call gains a second spelling.

// PreserveUnknownKeywords records every keyword s writes that no field of the
// schema model names, or that the model names and the dialect does not define
// (undefinedKeywords), and no reader above it already kept (GitHub #297).
//
// It is the last thing an attachment does, as annotation.UnknownKeywordsIn
// requires, so it is called here rather than inside annotation.Read:
// $dynamicRef has no field in the schema model either and is decided by
// recordUnexpandedDynamicRef, which runs after Read and keeps nothing once the
// reference has expanded. A census running before it could not tell that from
// an unread keyword.
func PreserveUnknownKeywords(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	diags := annotation.UnknownKeywordsIn(p, s, pointer, c.ProvenanceAt)
	return append(diags, annotation.UndefinedKeywordsIn(p, s, undefinedKeywords(c, s), pointer, c.ProvenanceAt)...)
}

// undefinedKeywords returns the keywords s writes that the schema model names
// but nothing reads, because the dialect s is read in does not define them:
// `nullable` while the nullable-widening policy is off, in a schema read as
// JSON Schema 2020-12, which is every 3.1 or 3.2 schema and the keywords
// beside a 3.0 $ref. A 3.0 schema's own nullable is read by its declared
// effect (docs/compilers/openapi.md#nullable), and the policy reads it
// everywhere.
func undefinedKeywords(c lowering.Ctx, s *oas3.Schema) []string {
	if c.NullableWidening.Honours() || (c.Is30() && s.Ref == nil) || annotation.RawPropertyNode(s, "nullable") == nil {
		return nil
	}
	return []string{"nullable"}
}

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
