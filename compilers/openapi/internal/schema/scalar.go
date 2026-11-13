// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/spelling"
	"github.com/dexpace/morphic/ir"
)

// scalarTypeID maps a scalar (type, format) pair to a TypeID via spelling.Lookup: a
// known pairing interns the shared primitive; byte, a number carried as text,
// a pairing the table does not list, and the 2020-12 content vocabulary beside a
// string each hoist a named Scalar wrapping the base primitive with an Encoding,
// so what the position wrote never leaks onto the shared primitive every other
// declaration of that type also resolves to.
//
// byte and a number carried as text put the logical primitive in Base and the
// string the value travels as in WireType (docs/compilers/openapi.md#string-formats).
func scalarTypeID(c lowering.Ctx, ts *compile.Types, depth int, s *oas3.Schema, st oas3.SchemaType, pointer jsontext.Pointer) (ir.TypeID, []ir.Diagnostic) {
	shape := scalarShapeOf(s, st, writtenFormat(s))
	if shape.shared {
		return ts.PrimID(shape.base), nil
	}
	return hoistScalar(c, ts, depth, s, shape, typeKinds(st), positionVoices(c, ts, s, pointer, ir.TypeRef{}), pointer)
}

// writtenFormat returns the format s writes, but for a redaction, which names no
// shape: the annotation readers hold it.
func writtenFormat(s *oas3.Schema) string {
	if annotation.DeclaresRedaction(s) {
		return ""
	}
	return s.GetFormat()
}

// scalarShape is what a scalar of one type lowers to: the primitive it is over,
// the encoding name and wire type a Scalar over that primitive holds, and
// whether the primitive alone says all of it, so the shared one stands for it.
type scalarShape struct {
	base   ir.PrimKind
	format string
	wire   *ir.PrimKind
	shared bool
}

// scalarShapeOf returns what a scalar of type st that s writes, with format,
// lowers to: byte beside a string a Scalar over bytes encoded base64, carried
// as a string; a pairing the table does not list a Scalar over the type's base
// keeping the format as its encoding name; and a pairing it lists the primitive
// it selects, shared unless s writes the content vocabulary for a type that
// takes it, a string's (keywordKinds).
func scalarShapeOf(s *oas3.Schema, st oas3.SchemaType, format string) scalarShape {
	if st == oas3.SchemaTypeString && format == "byte" {
		return scalarShape{base: ir.PrimBytes, format: "base64", wire: new(ir.PrimString)}
	}
	key := string(st)
	if format != "" {
		key += "/" + format
	}
	prim, known := spelling.Lookup(key)
	if !known {
		if num, carried := numberAsText(s, st, format); carried {
			return scalarShape{base: num, format: numberTextEncoding, wire: new(ir.PrimString)}
		}
		return scalarShape{base: baseForType(st), format: format}
	}
	return scalarShape{base: prim, shared: !takesContent(typeKinds(st)) || !declaresContentVocabulary(s)}
}

// numberTextEncoding is the Encoding.Name of a number written as the text of
// its value in a string (docs/ir/constraints.md#encodings).
const numberTextEncoding = "string"

// numberAsText returns the numeric primitive format selects under
// `type: number`, where a `type: string` writes it: the string is the text of
// a number of that primitive, as an int64 or a decimal wider than a JSON
// number's readers hold may travel. A contentEncoding naming another encoding
// says the string is that encoding's text instead, so the format, which
// encodingName then keeps verbatim, selects nothing.
func numberAsText(s *oas3.Schema, st oas3.SchemaType, format string) (ir.PrimKind, bool) {
	if st != oas3.SchemaTypeString || format == "" || contentOverridesFormat(s, numberTextEncoding) {
		return "", false
	}
	return spelling.Lookup(string(oas3.SchemaTypeNumber) + "/" + format)
}

// formatPrimitive returns the primitive that s's one declared type and its
// format select, when the table has that pairing.
func formatPrimitive(s *oas3.Schema) (ir.PrimKind, bool) {
	types := effectiveTypes(s)
	if len(types) != 1 {
		return "", false
	}
	return spelling.Lookup(string(types[0]) + "/" + s.GetFormat())
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

// hoistScalar hoists the Scalar shape describes at pointer, giving what the
// position wrote a node of its own: the encoding scalarEncoding builds from the
// shape's format name and the wire type it names, if any.
//
// The node carries the value constraints s and voices, the position's, write
// that apply to the kinds takes, because owning a node is what stops
// hoistDeclarationHome hoisting the alias that would otherwise carry them: that
// fallback resolves to whatever node the pointer already owns. A scalar hoisted
// for its format must not lose the bounds written beside it (invariant 2).
func hoistScalar(c lowering.Ctx, ts *compile.Types, depth int, s *oas3.Schema, shape scalarShape, takes kindSet, voices []annotation.Voice, pointer jsontext.Pointer) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, func(common ir.TypeCommon) ir.TypeDef {
		baseRef := ts.PrimRef(shape.base)
		enc, encDiags := scalarEncoding(c, ts, depth, s, shape.format, takesContent(takes), &common, pointer)
		diags = append(diags, encDiags...)
		if shape.wire != nil {
			wireRef := ts.PrimRef(*shape.wire)
			enc.WireType = &wireRef
		}
		if *enc == (ir.Encoding{}) {
			// A variant hoisted for a bound alone names no encoding.
			enc = nil
		}
		sc := &ir.Scalar{TypeCommon: common, Base: &baseRef, Encoding: enc}
		cons, consDiags := ConstraintsOf(c, &sc.Unmodeled, s, voices, pointer, takes)
		if carriesNumberAsText(sc) {
			cons, consDiags = textBounds(c, &sc.Unmodeled, s, pointer, consDiags)
		}
		diags = append(diags, consDiags...)
		sc.Constraints = cons
		return sc
	})
	return id, diags
}

// scalarEncoding builds the Encoding a scalar position declares: the whole
// 2020-12 content vocabulary, where withContent says the scalar's type takes it,
// over the OpenAPI `format` spelling of the encoding name. formatName is the
// encoding name the format contributes — "base64" for format: byte, an
// unrecognized format verbatim, "" when the pairing is already captured by the
// primitive kind.
func scalarEncoding(c lowering.Ctx, ts *compile.Types, depth int, s *oas3.Schema,
	formatName string, withContent bool, common *ir.TypeCommon, pointer jsontext.Pointer,
) (*ir.Encoding, []ir.Diagnostic) {
	if !withContent {
		return &ir.Encoding{Name: formatName}, nil
	}
	name, diags := encodingName(c, s, formatName, common, pointer)
	content, contentDiags := contentSchemaRef(c, ts, depth, s, pointer)
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
	if !contentOverridesFormat(s, formatName) {
		return content, nil
	}
	at := pointer + ids.Ptr("format")
	kept, diags := annotation.PreserveSchemaKeyword(&common.Unmodeled, s, "format", ir.ReasonNoIRHome, pointer, c.Place())
	if kept {
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"format and contentEncoding both name an encoding and ir.Encoding holds one; "+
				"contentEncoding %q is lowered and format is kept verbatim under Unmodeled", content))
	}
	return content, diags
}

// contentOverridesFormat reports whether s's contentEncoding names an encoding
// other than formatName, the one its format names, which encodingName lowers
// in the format's place.
func contentOverridesFormat(s *oas3.Schema, formatName string) bool {
	content := s.GetContentEncoding()
	return formatName != "" && content != "" && content != formatName
}

// contentSchemaRef lowers contentSchema — the shape the encoded value has once
// decoded — to Encoding.Schema. Its value is a schema, so it lowers like every
// other sub-schema position (fillAdditional, patternProps): hoisted at its own
// pointer and referenced by ID, never carried beside the encoding as a raw blob
// a consumer would have to re-parse.
//
// The pointer it hoists at is the one the source wrote it at, which only this
// declaration can name, so the node needs no namespace of its own
// (docs/ir/identity.md#the-id-grammar).
func contentSchemaRef(c lowering.Ctx, ts *compile.Types, depth int, s *oas3.Schema, pointer jsontext.Pointer) (*ir.TypeRef, []ir.Diagnostic) {
	cs := s.GetContentSchema()
	if !annotation.Written(cs) {
		return nil, nil
	}
	ref, diags := Ref(c, ts, depth, cs, pointer+ids.Ptr("contentSchema"))
	return &ref, diags
}

// contentKeywords are the 2020-12 content-vocabulary keywords, all three of
// which lower into ir.Encoding: contentEncoding names Encoding.Name,
// contentMediaType names Encoding.MediaType, and contentSchema names
// Encoding.Schema (ir/constraints.go,
// docs/compilers/openapi.md#content-vocabulary). One list because they share
// one home — a position that reached no Encoding keeps all three, and one that
// reached an Encoding keeps none.
var contentKeywords = []string{"contentEncoding", "contentMediaType", "contentSchema"}

// takesContent reports whether a reader of values of the kinds takes reads the
// content vocabulary, which applies to a string alone (keywordKinds).
func takesContent(takes kindSet) bool {
	return taken(takes, "contentEncoding")
}

// declaresContentVocabulary reports whether s writes any content-vocabulary
// keyword, so a position that wrote one owns a node to keep it on.
func declaresContentVocabulary(s *oas3.Schema) bool {
	return s.GetContentEncoding() != "" || s.GetContentMediaType() != "" ||
		annotation.Written(s.GetContentSchema())
}

// recordUnplacedContent keeps each content keyword verbatim on p, for a position
// whose lowering produced no ir.Encoding to hold it.
//
// A string position lowers them (scalarEncoding), and a type array listing a
// string on its string variant. Any other type, an alias over a $ref, or a
// schema with no declared type reaches no Encoding holding them — yet JSON
// Schema Validation 2020-12 §8 gives these keywords meaning on whatever instance
// turns out to be a string, so they are kept rather than dropped. It asks the
// node the position lowered to (holdsContent), not lower()'s dispatch.
func recordUnplacedContent(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, td ir.TypeDef, pointer jsontext.Pointer) []ir.Diagnostic {
	if !declaresContentVocabulary(s) || holdsContent(c, s, td, pointer) {
		return nil
	}
	var diags []ir.Diagnostic
	for _, keyword := range contentKeywords {
		kept, keptDiags := annotation.PreserveSchemaKeyword(p, s, keyword, ir.ReasonNoIRHome, pointer, c.Place())
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

// holdsContent reports whether td, the node s lowered to at pointer, holds the
// content vocabulary s writes: a Scalar carrying an Encoding for a type that
// takes it, or the Union of a type array listing a string, whose string variant
// carries one (lowerTypeVariant). A nil td is the property-carrier case — no
// node, so no Encoding.
func holdsContent(c lowering.Ctx, s *oas3.Schema, td ir.TypeDef, pointer jsontext.Pointer) bool {
	if !takesContent(declaredInstanceTypes(s)) {
		return false
	}
	switch n := td.(type) {
	case *ir.Scalar:
		return n.Encoding != nil
	case *ir.Union:
		return lowersTypeArray(c, s, pointer)
	default:
		return false
	}
}
