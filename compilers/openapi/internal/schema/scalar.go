package schema

import (
	"encoding/json/jsontext"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/spelling"
	"github.com/dexpace/morphic/ir"
)

// scalarTypeID maps a scalar (type, format) pair to a TypeID via spelling.Lookup: a
// known pairing interns the shared primitive; byte, a pairing the table does
// not list, and the 2020-12 content vocabulary each hoist a named Scalar
// wrapping the base primitive with an Encoding, so what the position wrote
// never leaks onto the shared primitive every other declaration of that type
// also resolves to.
func scalarTypeID(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, st oas3.SchemaType, pointer jsontext.Pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	format := s.GetFormat()
	if annotation.DeclaresRedaction(s) {
		format = "" // a redaction, not a shape: the annotation readers hold it
	}
	if st == oas3.SchemaTypeString && format == "byte" {
		return hoistScalar(c, ts, anchors, depth, s, ir.PrimBytes, "base64", new(ir.PrimString), pointer, hint)
	}
	key := string(st)
	if format != "" {
		key += "/" + format
	}
	prim, known := spelling.Lookup(key)
	if !known {
		return hoistScalar(c, ts, anchors, depth, s, baseForType(st), format, nil, pointer, hint)
	}
	if !declaresContentVocabulary(s) {
		return ts.PrimID(prim), nil
	}
	return hoistScalar(c, ts, anchors, depth, s, prim, "", nil, pointer, hint)
}

// baseForType returns the base primitive for an unknown-format scalar of type st.
func baseForType(st oas3.SchemaType) ir.PrimKind {
	switch st {
	case oas3.SchemaTypeInteger:
		return ir.PrimInteger
	case oas3.SchemaTypeNumber:
		return ir.PrimNumber
	case oas3.SchemaTypeBoolean:
		return ir.PrimBool
	default:
		return ir.PrimString
	}
}

// hoistScalar hoists a scalar over base, giving what the position wrote a node
// of its own to sit on: the encoding scalarEncoding builds from formatName and,
// when wire is non-nil, the primitive the encoded value travels as.
//
// The node carries the position's value constraints itself, because owning a
// node is what stops hoistDeclarationHome hoisting the alias that would
// otherwise carry them: that fallback resolves to whatever node the pointer
// already owns and returns early. A scalar that hoisted because it wrote a
// format must not lose the bounds it wrote beside it (invariant 2).
func hoistScalar(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, base ir.PrimKind, formatName string, wire *ir.PrimKind, pointer jsontext.Pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		baseRef := ts.PrimRef(base)
		enc, encDiags := scalarEncoding(c, ts, anchors, depth, s, formatName, &common, pointer, hint)
		diags = append(diags, encDiags...)
		if wire != nil {
			wireRef := ts.PrimRef(*wire)
			enc.WireType = &wireRef
		}
		cons, consDiags := ConstraintsOf(c, &common.Unmodeled, s, pointer)
		diags = append(diags, consDiags...)
		return &ir.Scalar{
			TypeCommon:  common,
			Base:        &baseRef,
			Encoding:    enc,
			Constraints: cons,
		}
	})
	return id, diags
}

// scalarEncoding builds the Encoding a scalar position declares: the whole
// 2020-12 content vocabulary over the OpenAPI `format` spelling of the encoding
// name. formatName is the encoding name the format contributes — "base64" for
// format: byte, an unrecognized format verbatim, "" when the pairing is already
// captured by the primitive kind.
func scalarEncoding(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema,
	formatName string, common *ir.TypeCommon, pointer jsontext.Pointer, hint string,
) (*ir.Encoding, []ir.Diagnostic) {
	name, diags := encodingName(c, s, formatName, common, pointer)
	content, contentDiags := contentSchemaRef(c, ts, anchors, depth, s, pointer, hint)
	enc := &ir.Encoding{Name: name, MediaType: s.GetContentMediaType(), Schema: content}
	return enc, append(diags, contentDiags...)
}

// encodingName elects the one name ir.Encoding holds from the two keywords that
// can name an encoding at a scalar position.
//
// contentEncoding wins: it is the standard keyword, where a format the IR could
// not place is only parked there. Encoding holds one name, so a format that
// named a *different* encoding is kept verbatim on the node rather than
// overwritten away.
func encodingName(c lowering.Ctx, s *oas3.Schema, formatName string, common *ir.TypeCommon, pointer jsontext.Pointer) (string, []ir.Diagnostic) {
	content := s.GetContentEncoding()
	if content == "" || content == formatName {
		return formatName, nil
	}
	if formatName == "" {
		return content, nil
	}
	at := pointer + ids.Ptr("format")
	kept, diags := annotation.PreserveSchemaKeyword(&common.Unmodeled, s, "format", ir.ReasonNoIRHome, pointer, c.ProvenanceAt)
	if kept {
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"format and contentEncoding both name an encoding and ir.Encoding holds one; "+
				"contentEncoding %q is lowered and format is kept verbatim under Unmodeled", content))
	}
	return content, diags
}

// contentSchemaRef lowers contentSchema — the shape the encoded value has once
// decoded — to Encoding.Schema. Its value is a schema, so it lowers like every
// other sub-schema position (fillAdditional, patternProps): hoisted at its own
// pointer and referenced by ID, never carried beside the encoding as a raw blob
// a consumer would have to re-parse.
//
// The pointer it hoists at is the one the source wrote it at, which only this
// declaration can name, so the node needs no namespace of its own (§4.3).
func contentSchemaRef(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer jsontext.Pointer, hint string) (*ir.TypeRef, []ir.Diagnostic) {
	cs := s.GetContentSchema()
	if !annotation.Written(cs) {
		return nil, nil
	}
	ref, diags := Ref(c, ts, anchors, depth, cs, pointer+ids.Ptr("contentSchema"), compile.SubHint(hint, "content"))
	return &ref, diags
}

// contentKeywords are the 2020-12 content-vocabulary keywords, all three of
// which lower into ir.Encoding: contentEncoding names Encoding.Name,
// contentMediaType names Encoding.MediaType, and contentSchema names
// Encoding.Schema (ir/constraints.go, ir-design §5.3). One list because they
// share one home — a position that reached no Encoding keeps all three, and one
// that reached an Encoding keeps none.
var contentKeywords = []string{"contentEncoding", "contentMediaType", "contentSchema"}

// declaresContentVocabulary reports whether s writes any content-vocabulary
// keyword, so a position that wrote one owns a node to keep it on.
func declaresContentVocabulary(s *oas3.Schema) bool {
	return s.GetContentEncoding() != "" || s.GetContentMediaType() != "" ||
		annotation.Written(s.GetContentSchema())
}

// recordUnplacedContent keeps each content keyword verbatim on p, for a position
// whose lowering produced no ir.Encoding to hold it.
//
// A string position lowers them (scalarEncoding). An object, an array, a union,
// an alias over a $ref, or a schema with no declared type at all has no Encoding
// field — yet JSON Schema Validation 2020-12 §8 gives these keywords meaning on
// whatever instance turns out to be a string, so they are kept rather than
// dropped. It asks the node the position actually lowered to instead of
// re-deriving lower()'s dispatch, so the two cannot drift apart.
func recordUnplacedContent(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, td ir.TypeDef, pointer jsontext.Pointer) []ir.Diagnostic {
	if !declaresContentVocabulary(s) || scalarHasEncoding(td) {
		return nil
	}
	var diags []ir.Diagnostic
	for _, keyword := range contentKeywords {
		kept, keptDiags := annotation.PreserveSchemaKeyword(p, s, keyword, ir.ReasonNoIRHome, pointer, c.ProvenanceAt)
		diags = append(diags, keptDiags...)
		if !kept {
			continue
		}
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer+ids.Ptr(keyword),
			"%s is content-vocabulary data the IR holds in ir.Encoding, and this position "+
				"lowered to a shape with no Encoding field; kept verbatim under Unmodeled", keyword))
	}
	return diags
}

// scalarHasEncoding reports whether td is a Scalar already carrying the
// position's Encoding. A nil td is the property-carrier case — no node, so no
// Encoding.
func scalarHasEncoding(td ir.TypeDef) bool {
	sc, ok := td.(*ir.Scalar)
	return ok && sc.Encoding != nil
}
