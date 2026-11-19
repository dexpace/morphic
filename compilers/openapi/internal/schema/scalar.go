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
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/compilers/openapi/internal/typeformat"
	"github.com/dexpace/morphic/ir"
)

// scalarTypeID maps a scalar (type, format) pair to a TypeID via
// typeformat.Lookup: a known pairing interns the shared primitive; a pairing
// typeformat.LookupEncoded knows, such as byte, a number carried as text, one
// neither table lists, and the 2020-12 content vocabulary beside a string each
// hoist a named Scalar wrapping the base primitive with an Encoding, so what
// the position wrote never leaks onto the shared primitive every other
// declaration of that type also resolves to.
//
// byte and a number carried as text put the logical primitive in Base and the
// string the value travels as in WireType (docs/compilers/openapi.md#string-formats).
func scalarTypeID(loweringContext lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, kind statement.JSONSchemaTypeName, pointer jsontext.Pointer,
	fin finisher, st stand,
) (ir.TypeID, keySet, []ir.Diagnostic) {
	shape := scalarShapeOf(schema, kind, writtenFormat(schema))
	if shape.shared {
		// The type and the format beside it selected the shared primitive.
		return ts.PrimitiveID(shape.base), scalarTypeRead(schema) | shape.read, nil
	}
	id, diags := hoistScalar(loweringContext, ts, depth, schema, shape, typeKinds(kind), st.foldedEntries(loweringContext, schema, pointer), pointer, fin, st)
	return id, 0, diags
}

// writtenFormat returns the format schema writes, but for a redaction, which
// names no shape: the annotation readers hold it.
func writtenFormat(schema *statement.Schema) string {
	if annotation.DeclaresRedaction(schema) {
		return ""
	}
	return schema.FormatName()
}

// scalarShape is what a scalar of one type lowers to: the primitive it is over,
// the encoding name and wire type a Scalar over that primitive holds, and
// whether the primitive alone says all of it, so the shared one stands for it.
type scalarShape struct {
	base   ir.PrimitiveKind
	format string
	wire   *ir.PrimitiveKind
	shared bool
	// read is the format the shape was selected by, none where it names none.
	read keySet
}

// scalarShapeOf returns what a scalar of type st that schema writes, with
// format, lowers to: a pairing typeformat.LookupEncoded knows, such as byte, a
// Scalar over the primitive it names with its encoding and wire type; a pairing
// neither table lists a Scalar over the type's base keeping the format as its
// encoding name; and a pairing typeformat.Lookup lists the primitive it
// selects, shared unless schema writes the content vocabulary for a type that
// takes it, a string's (keywordKinds).
func scalarShapeOf(schema *statement.Schema, st statement.JSONSchemaTypeName, format string) scalarShape {
	key := string(st)
	var read keySet
	if format != "" {
		key += "/" + format
		read = keyBit("format")
	}
	if row, encoded := typeformat.LookupEncoded(key); encoded {
		return scalarShape{base: row.Primitive, format: row.Encoding, wire: wireOf(row), read: read}
	}
	primitive, known := typeformat.Lookup(key)
	if !known {
		if number, carried := numberAsText(schema, st, format); carried {
			return scalarShape{base: number, format: numberTextEncoding, wire: new(ir.PrimitiveString), read: read}
		}
		return scalarShape{base: baseForType(st), format: format, read: read}
	}
	return scalarShape{base: primitive, shared: !takesContent(typeKinds(st)) || !declaresContentVocabulary(schema), read: read}
}

// wireOf returns the primitive row's value travels as, the WireType of the
// Scalar scalarShapeOf gives it: nil where the primitive's own spelling says it.
func wireOf(row typeformat.Encoded) *ir.PrimitiveKind {
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
func numberAsText(schema *statement.Schema, st statement.JSONSchemaTypeName, format string) (ir.PrimitiveKind, bool) {
	if st != statement.KindString || format == "" || contentOverridesFormat(schema, numberTextEncoding) {
		return "", false
	}
	return typeformat.Lookup(string(statement.KindNumber) + "/" + format)
}

// formatPrimitive returns the primitive that schema's one declared type and its
// format select, when the table has that pairing.
func formatPrimitive(schema *statement.Schema) (ir.PrimitiveKind, bool) {
	types := effectiveTypes(schema)
	if len(types) != 1 {
		return "", false
	}
	return typeformat.Lookup(string(types[0]) + "/" + schema.FormatName())
}

// baseForType returns the base primitive for an unknown-format scalar of type st.
func baseForType(st statement.JSONSchemaTypeName) ir.PrimitiveKind {
	switch st {
	case statement.KindInteger:
		return ir.PrimitiveInteger
	case statement.KindNumber:
		return ir.PrimitiveNumber
	case statement.KindBoolean:
		return ir.PrimitiveBool
	default:
		return ir.PrimitiveString
	}
}

// hoistScalar hoists the Scalar shape describes at pointer, giving what the
// position wrote a node of its own: the encoding scalarEncoding builds from the
// shape's format name and the wire type it names, if any.
//
// The node carries the value constraints schema and foldedEntries, the
// position's, write that apply to the kinds takes, because owning a node is
// what stops hoistDeclarationHome hoisting the alias that would otherwise carry
// them: that fallback resolves to whatever node the pointer already owns. A
// scalar hoisted for its format must not lose the bounds written beside it
// (invariant 2).
//
// fin finishes the Scalar once built.
func hoistScalar(loweringContext lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, shape scalarShape, takes kindSet,
	foldedEntries []annotation.FoldedAllOfEntry, pointer jsontext.Pointer, fin finisher, st stand,
) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	var text []string
	if st.of != nil {
		// The text bounds of a principal are kept once its position has written.
		outer := fin
		fin = func(td ir.TypeDef, read keySet, held *principalKept) {
			diags = append(diags, keepAround(loweringContext, ts, st, td, read, held, schema, pointer, textBounds(loweringContext, schema, text, pointer), outer)...)
		}
	}
	id := internNode(loweringContext, ts, st.pointer(pointer), func(common ir.TypeCommon) (ir.TypeDef, keySet) {
		baseRef := ts.PrimitiveRef(shape.base)
		enc, encDiags := scalarEncoding(loweringContext, ts, depth, schema, shape.format, takesContent(takes), &common, pointer)
		diags = append(diags, encDiags...)
		if shape.wire != nil {
			wireRef := ts.PrimitiveRef(*shape.wire)
			enc.WireType = &wireRef
		}
		if *enc == (ir.Encoding{}) {
			// A variant hoisted for a bound alone names no encoding.
			enc = nil
		}
		sc := &ir.Scalar{TypeCommon: common, Base: &baseRef, Encoding: enc}
		cons, consDiags := ConstraintsOf(loweringContext, &sc.Unmodeled, schema, foldedEntries, pointer, takes)
		if carriesNumberAsText(sc) {
			cons = nil
			switch {
			case st.held:
				// A variant of a principal held whole keeps none, nor reads them:
				// its union keeps them unread for the whole entry.
			case st.of != nil:
				text = declaredConstraints(schema)
			default:
				consDiags = append(consDiags, keepTextBounds(loweringContext, &sc.Unmodeled, schema, declaredConstraints(schema), pointer)...)
			}
		}
		diags = append(diags, consDiags...)
		sc.Constraints = cons
		return sc, scalarTypeRead(schema) | shape.read | nodeBoundsRead(schema, takes, sc.Constraints, sc.Unmodeled) | keySetOf(text...)
	}, fin)
	return id, diags
}

// textBounds is the keeping of bounds, the value bounds schema, written at
// pointer, writes on a Scalar carrying a number as text, which holds none of
// them (keepTextBounds).
func textBounds(loweringContext lowering.Ctx, schema *statement.Schema, bounds []string, pointer jsontext.Pointer) keeping {
	return keeping{keys: bounds, reason: ir.ReasonNoIRHome, keep: func(unmodeled *ir.Unmodeled, keys []string) []ir.Diagnostic {
		return keepTextBounds(loweringContext, unmodeled, schema, keys, pointer)
	}}
}

// scalarEncoding builds the Encoding a scalar position declares: the whole
// 2020-12 content vocabulary, where withContent says the scalar's type takes it,
// over the OpenAPI `format` spelling of the encoding name. formatName is the
// encoding name the format contributes — the one typeformat.LookupEncoded names,
// as "base64" for format: byte, an unrecognized format verbatim, "" when the
// pairing is already captured by the primitive kind.
func scalarEncoding(loweringContext lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema,
	formatName string, withContent bool, common *ir.TypeCommon, pointer jsontext.Pointer,
) (*ir.Encoding, []ir.Diagnostic) {
	if !withContent {
		return &ir.Encoding{Name: formatName}, nil
	}
	name, diags := encodingName(loweringContext, schema, formatName, common, pointer)
	content, contentDiags := contentSchemaRef(loweringContext, ts, depth, schema, pointer)
	enc := &ir.Encoding{Name: name, MediaType: schema.TextKeywords().ContentMediaType, Schema: content}
	return enc, append(diags, contentDiags...)
}

// encodingName elects the one name ir.Encoding holds from the two keywords that
// can name an encoding at a scalar position.
//
// contentEncoding wins: it is the standard keyword, where a format the IR could
// not place is only parked there. Encoding holds one name, so a format that
// named a *different* encoding is kept verbatim on the node rather than
// overwritten away.
func encodingName(loweringContext lowering.Ctx, schema *statement.Schema, formatName string, common *ir.TypeCommon, pointer jsontext.Pointer) (string, []ir.Diagnostic) {
	content := schema.TextKeywords().ContentEncoding
	if content == "" || content == formatName {
		return formatName, nil
	}
	if !contentOverridesFormat(schema, formatName) {
		return content, nil
	}
	at := pointer + ids.Ptr("format")
	kept, diags := annotation.PreserveSchemaKeyword(&common.Unmodeled, schema, "format", ir.ReasonNoIRHome, pointer, loweringContext.Place())
	if kept {
		diags = append(diags, loweringContext.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"format and contentEncoding both name an encoding and ir.Encoding holds one; "+
				"contentEncoding %q is lowered and format is kept verbatim under Unmodeled", content))
	}
	return content, diags
}

// contentOverridesFormat reports whether schema's contentEncoding names an
// encoding other than formatName, the one its format names, which encodingName
// lowers in the format's place.
func contentOverridesFormat(schema *statement.Schema, formatName string) bool {
	content := schema.TextKeywords().ContentEncoding
	return formatName != "" && content != "" && content != formatName
}

// contentSchemaRef lowers contentSchema — the shape the encoded value has once
// decoded — to Encoding.Schema. Its value is a schema, so it lowers like every
// other sub-schema position (fillAdditional, patternProperties): hoisted at its
// own pointer and referenced by ID, never carried beside the encoding as a raw
// blob a consumer would have to re-parse.
//
// The pointer it hoists at is the one the source wrote it at, which only this
// declaration can name, so the node needs no namespace of its own
// (docs/ir/identity.md#the-id-grammar).
func contentSchemaRef(loweringContext lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, pointer jsontext.Pointer) (*ir.TypeRef, []ir.Diagnostic) {
	cs := loweringContext.Statements().Subschema(schema.TextKeywords().ContentSchema)
	if cs.Truth == statement.Unwritten {
		return nil, nil
	}
	ref, diags := Ref(loweringContext, ts, depth, cs, pointer+ids.Ptr("contentSchema"))
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

// declaresContentVocabulary reports whether schema writes any
// content-vocabulary keyword, so a position that wrote one owns a node to keep
// it on.
func declaresContentVocabulary(schema *statement.Schema) bool {
	text := schema.TextKeywords()
	return text.ContentEncoding != "" || text.ContentMediaType != "" || written(text.ContentSchema)
}

// recordUnplacedContent keeps each content keyword verbatim on unmodeled, for a
// position whose lowering produced no ir.Encoding to hold it.
//
// A string position lowers them (scalarEncoding), and a type array listing a
// string on its string variant. Any other type, an alias over a $ref, or a
// schema with no declared type reaches no Encoding holding them — yet JSON
// Schema Validation 2020-12 §8 gives these keywords meaning on whatever instance
// turns out to be a string, so they are kept rather than dropped. It asks the
// node the position lowered to (holdsContent), not lower()'s dispatch.
func recordUnplacedContent(loweringContext lowering.Ctx, unmodeled *ir.Unmodeled, schema *statement.Schema, td ir.TypeDef, pointer jsontext.Pointer) []ir.Diagnostic {
	// Called directly, never through a keeping: unmodeled is a carrier's field
	// on most calls, which a call through a function value would move to the
	// heap.
	keys, why := unplacedContentKeys(loweringContext, schema, td, pointer)
	return keepContentKeywords(loweringContext, unmodeled, schema, keys, pointer, why)
}

// unplacedContent is the keeping of the content keywords schema, written at
// pointer, writes where td, the node it lowered to, holds no ir.Encoding for
// them (unplacedContentKeys): each as no_ir_home, reported at the keyword.
func unplacedContent(loweringContext lowering.Ctx, schema *statement.Schema, td ir.TypeDef, pointer jsontext.Pointer) keeping {
	keys, why := unplacedContentKeys(loweringContext, schema, td, pointer)
	return keeping{keys: keys, reason: ir.ReasonNoIRHome, keep: func(unmodeled *ir.Unmodeled, keys []string) []ir.Diagnostic {
		return keepContentKeywords(loweringContext, unmodeled, schema, keys, pointer, why)
	}}
}

// unplacedContentKeys returns the content keywords schema, written at pointer,
// writes that td, the node it lowered to, holds no ir.Encoding for
// (holdsContent), and why, for their report. Where td holds the Encoding of the
// inline principal schema lowers from (principalEncoding), a keyword schema
// writes as that principal does is read there, and any other is kept beside it,
// the report saying whose the Encoding is.
func unplacedContentKeys(loweringContext lowering.Ctx, schema *statement.Schema, td ir.TypeDef, pointer jsontext.Pointer) (keys []string, why string) {
	if !declaresContentVocabulary(schema) || holdsContent(loweringContext, schema, td, pointer) {
		return nil, ""
	}
	why = noEncodingField
	bs := principalEncoding(loweringContext, schema, td, pointer)
	if bs != nil {
		why = encodingOfEntry
	}
	for _, keyword := range contentKeywords {
		if hasKey(schema, keyword) && (bs == nil || !keyWrittenAlike(schema, bs, keyword)) {
			keys = append(keys, keyword)
		}
	}
	return keys, why
}

// keepContentKeywords keeps each of keys by keepContentKeyword.
func keepContentKeywords(loweringContext lowering.Ctx, unmodeled *ir.Unmodeled, schema *statement.Schema, keys []string, pointer jsontext.Pointer, why string,
) []ir.Diagnostic {
	diags := make([]ir.Diagnostic, 0, len(keys))
	for _, keyword := range keys {
		diags = append(diags, keepContentKeyword(loweringContext, unmodeled, schema, keyword, pointer, why)...)
	}
	return diags
}

// Why a content keyword reaches no ir.Encoding, for the report
// keepContentKeyword draws: the node has no such field, or its field holds what
// the inline allOf entry the position lowered from writes.
const (
	noEncodingField = "this position lowered to a shape with no Encoding field"
	encodingOfEntry = "the Encoding this position lowered to holds what its inline allOf entry writes"
)

// principalEncoding returns the inline principal of the allOf schema writes at
// pointer where td, the node it built for the position, holds the content
// vocabulary it writes (holdsContent): a Scalar's Encoding, or that of a type
// list's string variant. It is nil where schema lowers from no inline principal
// or td holds none.
func principalEncoding(loweringContext lowering.Ctx, schema *statement.Schema, td ir.TypeDef, pointer jsontext.Pointer) *statement.Schema {
	refinement, refined := refinementOf(loweringContext, schema, pointer)
	if !refined || refinement.principal == nil || refinement.principal.schema.Ref != nil {
		return nil
	}
	if entry := refinement.principal; holdsContent(loweringContext, entry.schema.Schema, td, entry.pointer) {
		return entry.schema.Schema
	}
	return nil
}

// keepContentKeyword keeps the content keyword schema, the schema at pointer,
// writes under keyword verbatim on unmodeled, as no_ir_home, and reports it at
// the keyword, where no ir.Encoding holds it, for why (noEncodingField).
func keepContentKeyword(loweringContext lowering.Ctx, unmodeled *ir.Unmodeled, schema *statement.Schema, keyword string, pointer jsontext.Pointer, why string,
) []ir.Diagnostic {
	kept, diags := annotation.PreserveSchemaKeyword(unmodeled, schema, keyword, ir.ReasonNoIRHome, pointer, loweringContext.Place())
	if !kept {
		return diags
	}
	return append(diags, loweringContext.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer+ids.Ptr(keyword),
		"%s is content-vocabulary data the IR holds in ir.Encoding, and %s; kept verbatim under Unmodeled", keyword, why))
}

// holdsContent reports whether td, the node schema lowered to at pointer, holds
// the content vocabulary schema writes: a Scalar carrying an Encoding for a
// type that takes it, or the Union of a type array listing a string, whose
// string variant carries one (lowerTypeVariant). A nil td is the
// property-carrier case — no node, so no Encoding.
func holdsContent(loweringContext lowering.Ctx, schema *statement.Schema, td ir.TypeDef, pointer jsontext.Pointer) bool {
	if !takesContent(declaredInstanceTypes(schema)) {
		return false
	}
	switch n := td.(type) {
	case *ir.Scalar:
		// An inline principal's node reads the Encoding of its entry alone, so
		// it holds none that s writes beside its allOf.
		refinement, refined := refinementOf(loweringContext, schema, pointer)
		return n.Encoding != nil && (!refined || refinement.principal == nil)
	case *ir.Union:
		return lowersTypeArray(loweringContext, schema, pointer)
	default:
		return false
	}
}
