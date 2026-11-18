// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/spelling"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// scalarTypeID maps a scalar (type, format) pair to a TypeID via spelling.Lookup: a
// known pairing interns the shared primitive; a pairing spelling.LookupEncoded
// knows, such as byte, a number carried as text, one neither table lists, and
// the 2020-12 content vocabulary beside a string each hoist a named Scalar
// wrapping the base primitive with an Encoding, so what the position wrote never
// leaks onto the shared primitive every other declaration of that type also
// resolves to.
//
// byte and a number carried as text put the logical primitive in Base and the
// string the value travels as in WireType (docs/compilers/openapi.md#string-formats).
func scalarTypeID(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, kind statement.Kind, pointer jsontext.Pointer,
	fin finisher, st stand,
) (ir.TypeID, keySet, []ir.Diagnostic) {
	shape := scalarShapeOf(s, kind, writtenFormat(s))
	if shape.shared {
		// The type and the format beside it selected the shared primitive.
		return ts.PrimID(shape.base), scalarTypeRead(s) | shape.read, nil
	}
	id, diags := hoistScalar(c, ts, depth, s, shape, typeKinds(kind), st.voices(c, s, pointer), pointer, fin, st)
	return id, 0, diags
}

// writtenFormat returns the format s writes, but for a redaction, which names no
// shape: the annotation readers hold it.
func writtenFormat(s *statement.Schema) string {
	if annotation.DeclaresRedaction(s) {
		return ""
	}
	return s.FormatName()
}

// scalarShape is what a scalar of one type lowers to: the primitive it is over,
// the encoding name and wire type a Scalar over that primitive holds, and
// whether the primitive alone says all of it, so the shared one stands for it.
type scalarShape struct {
	base   ir.PrimKind
	format string
	wire   *ir.PrimKind
	shared bool
	// read is the format the shape was selected by, none where it names none.
	read keySet
}

// scalarShapeOf returns what a scalar of type st that s writes, with format,
// lowers to: a pairing spelling.LookupEncoded knows, such as byte, a Scalar over
// the primitive it names with its encoding and wire type; a pairing neither
// table lists a Scalar over the type's base keeping the format as its encoding
// name; and a pairing spelling.Lookup lists the primitive it selects, shared
// unless s writes the content vocabulary for a type that takes it, a string's
// (keywordKinds).
func scalarShapeOf(s *statement.Schema, st statement.Kind, format string) scalarShape {
	key := string(st)
	var read keySet
	if format != "" {
		key += "/" + format
		read = keyBit("format")
	}
	if row, encoded := spelling.LookupEncoded(key); encoded {
		return scalarShape{base: row.Prim, format: row.Encoding, wire: wireOf(row), read: read}
	}
	prim, known := spelling.Lookup(key)
	if !known {
		if num, carried := numberAsText(s, st, format); carried {
			return scalarShape{base: num, format: numberTextEncoding, wire: new(ir.PrimString), read: read}
		}
		return scalarShape{base: baseForType(st), format: format, read: read}
	}
	return scalarShape{base: prim, shared: !takesContent(typeKinds(st)) || !declaresContentVocabulary(s), read: read}
}

// wireOf returns the primitive row's value travels as, the WireType of the
// Scalar scalarShapeOf gives it: nil where the primitive's own spelling says it.
func wireOf(row spelling.Encoded) *ir.PrimKind {
	if row.Wire == "" {
		return nil
	}
	return &row.Wire
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
func numberAsText(s *statement.Schema, st statement.Kind, format string) (ir.PrimKind, bool) {
	if st != statement.KindString || format == "" || contentOverridesFormat(s, numberTextEncoding) {
		return "", false
	}
	return spelling.Lookup(string(statement.KindNumber) + "/" + format)
}

// formatPrimitive returns the primitive that s's one declared type and its
// format select, when the table has that pairing.
func formatPrimitive(s *statement.Schema) (ir.PrimKind, bool) {
	types := effectiveTypes(s)
	if len(types) != 1 {
		return "", false
	}
	return spelling.Lookup(string(types[0]) + "/" + s.FormatName())
}

// baseForType returns the base primitive for an unknown-format scalar of type st.
func baseForType(st statement.Kind) ir.PrimKind {
	switch st {
	case statement.KindInteger:
		return ir.PrimInteger
	case statement.KindNumber:
		return ir.PrimNumber
	case statement.KindBoolean:
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
//
// fin finishes the Scalar once built.
func hoistScalar(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, shape scalarShape, takes kindSet,
	voices []annotation.Voice, pointer jsontext.Pointer, fin finisher, st stand,
) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, st.pointer(pointer), func(common ir.TypeCommon) (ir.TypeDef, keySet) {
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
			cons, consDiags = nil, append(consDiags, keepTextBounds(c, &sc.Unmodeled, s, declaredConstraints(s), pointer)...)
		}
		diags = append(diags, consDiags...)
		sc.Constraints = cons
		return sc, scalarTypeRead(s) | shape.read | nodeBoundsRead(s, takes, sc.Constraints, sc.Unmodeled)
	}, fin)
	return id, diags
}

// scalarEncoding builds the Encoding a scalar position declares: the whole
// 2020-12 content vocabulary, where withContent says the scalar's type takes it,
// over the OpenAPI `format` spelling of the encoding name. formatName is the
// encoding name the format contributes — the one spelling.LookupEncoded names,
// as "base64" for format: byte, an unrecognized format verbatim, "" when the
// pairing is already captured by the primitive kind.
func scalarEncoding(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema,
	formatName string, withContent bool, common *ir.TypeCommon, pointer jsontext.Pointer,
) (*ir.Encoding, []ir.Diagnostic) {
	if !withContent {
		return &ir.Encoding{Name: formatName}, nil
	}
	name, diags := encodingName(c, s, formatName, common, pointer)
	content, contentDiags := contentSchemaRef(c, ts, depth, s, pointer)
	enc := &ir.Encoding{Name: name, MediaType: s.TextKeywords().ContentMediaType, Schema: content}
	return enc, append(diags, contentDiags...)
}

// encodingName elects the one name ir.Encoding holds from the two keywords that
// can name an encoding at a scalar position.
//
// contentEncoding wins: it is the standard keyword, where a format the IR could
// not place is only parked there. Encoding holds one name, so a format that
// named a *different* encoding is kept verbatim on the node rather than
// overwritten away.
func encodingName(c lowering.Ctx, s *statement.Schema, formatName string, common *ir.TypeCommon, pointer jsontext.Pointer) (string, []ir.Diagnostic) {
	content := s.TextKeywords().ContentEncoding
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
func contentOverridesFormat(s *statement.Schema, formatName string) bool {
	content := s.TextKeywords().ContentEncoding
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
func contentSchemaRef(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer) (*ir.TypeRef, []ir.Diagnostic) {
	cs := c.Statements().Sub(s.TextKeywords().ContentSchema)
	if cs.Truth == statement.Unwritten {
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
func declaresContentVocabulary(s *statement.Schema) bool {
	text := s.TextKeywords()
	return text.ContentEncoding != "" || text.ContentMediaType != "" || written(text.ContentSchema)
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
func recordUnplacedContent(c lowering.Ctx, p *ir.Unmodeled, s *statement.Schema, td ir.TypeDef, pointer jsontext.Pointer) []ir.Diagnostic {
	if !declaresContentVocabulary(s) || holdsContent(c, s, td, pointer) {
		return nil
	}
	var diags []ir.Diagnostic
	for _, keyword := range contentKeywords {
		diags = append(diags, keepContentKeyword(c, p, s, keyword, pointer)...)
	}
	return diags
}

// keepContentKeyword keeps the content keyword s, the schema at pointer,
// writes under keyword verbatim on p, as no_ir_home, and reports it at the
// keyword, where no ir.Encoding holds it.
func keepContentKeyword(c lowering.Ctx, p *ir.Unmodeled, s *statement.Schema, keyword string, pointer jsontext.Pointer) []ir.Diagnostic {
	kept, diags := annotation.PreserveSchemaKeyword(p, s, keyword, ir.ReasonNoIRHome, pointer, c.Place())
	if !kept {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer+ids.Ptr(keyword),
		"%s is content-vocabulary data the IR holds in ir.Encoding, and this position "+
			"lowered to a shape with no Encoding field; kept verbatim under Unmodeled", keyword))
}

// holdsContent reports whether td, the node s lowered to at pointer, holds the
// content vocabulary s writes: a Scalar carrying an Encoding for a type that
// takes it, or the Union of a type array listing a string, whose string variant
// carries one (lowerTypeVariant). A nil td is the property-carrier case — no
// node, so no Encoding.
func holdsContent(c lowering.Ctx, s *statement.Schema, td ir.TypeDef, pointer jsontext.Pointer) bool {
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
