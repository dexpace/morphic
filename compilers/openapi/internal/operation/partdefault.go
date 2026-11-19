// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"context"
	"slices"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// contentEncoding is what a part's declarations say of contentEncoding, which
// the IR's Encoding.Name does not tell from a format's name: none writes it,
// each writes it, or some do and some do not.
type contentEncoding uint8

const (
	// unencoded is no declaration writing contentEncoding.
	unencoded contentEncoding = iota
	// encoded is every declaration writing it.
	encoded
	// partlyEncoded is some declarations writing it and some not.
	partlyEncoded
)

// readContentEncoding reads whether schemas, a part's declarations, write
// contentEncoding, each read through its $ref chain and a refinement to the
// schema deciding it (schema.DecidingSchema), as the type lowering reads it.
func readContentEncoding(serviceContext service.Ctx, schemas []*oas3.JSONSchema[oas3.Referenceable]) contentEncoding {
	writes, omits := false, false
	for _, js := range schemas {
		statementSchema := statementOf(serviceContext.Ctx, js)
		if statement.Parsed(statementSchema) == nil {
			continue
		}
		if statement.Parsed(schema.DecidingSchema(serviceContext.Ctx, statementSchema)).GetContentEncoding() != "" {
			writes = true
		} else {
			omits = true
		}
	}
	switch {
	case writes && omits:
		return partlyEncoded
	case writes:
		return encoded
	default:
		return unencoded
	}
}

// partValues returns the types of the values a part's Encoding Object applies
// to, decls declaring the part: each declaration's type, a union's members in
// its place (unionMembers), and a list's or a tuple's elements in place of the
// list, since an encoding applies to each item of an array part (OpenAPI 3.2
// §4.15.1). ok is false where a union's members are not read. A union's values
// are read once a run (partUnions.valuesOf), and a part declared once by a
// union takes them as they are, a list it only reads.
func partValues(unions *partUnions, node func(ir.TypeID) (ir.TypeDef, bool), decls []*ir.Property) (values []ir.TypeRef, ok bool) {
	for _, property := range decls {
		union, isUnion := bodyNode(node, property.Type.Target).(*ir.Union)
		if !isUnion || union == nil {
			values = append(values, itemsOf(node, property.Type)...)
			continue
		}
		held, read := unions.valuesOf(node, union)
		switch {
		case !read:
			return nil, false
		case len(decls) == 1:
			return held, true
		}
		values = append(values, held...)
	}
	return values, true
}

// withDefaultContentType returns pe, the encoding of a part decls declare,
// with the content type OpenAPI gives it by default, encodedBy saying what its
// declarations write of contentEncoding, and decl the one declaration of the
// part. The run's store reads it once for every part so declared
// (partUnions.partDefault).
func withDefaultContentType(unions *partUnions, node func(ir.TypeID) (ir.TypeDef, bool), rules specrules.EncodingRules, decl *ir.Property, encodedBy contentEncoding, pe ir.PartEncoding) ir.PartEncoding {
	if types, ok := unions.partDefault(node, rules, decl, encodedBy); ok {
		pe.ContentTypes = slices.Clone(types)
	}
	return pe
}

// itemsOf returns the element types of ref where it stands for a list or a
// tuple (elementsOf), and ref itself otherwise.
func itemsOf(node func(ir.TypeID) (ir.TypeDef, bool), ref ir.TypeRef) []ir.TypeRef {
	if elems, isArray := elementsOf(bodyNode(node, ref.Target)); isArray {
		return elems
	}
	return []ir.TypeRef{ref}
}

// elementsOf returns the element types of td where it is a list or a tuple, a
// tuple's rest included.
func elementsOf(td ir.TypeDef) (elems []ir.TypeRef, isArray bool) {
	switch t := td.(type) {
	case *ir.List:
		return []ir.TypeRef{t.ElementType}, true
	case *ir.Tuple:
		elems = append(elems, t.ElementTypes...)
		if t.Rest != nil {
			elems = append(elems, *t.Rest)
		}
		return elems, true
	default:
		return nil, false
	}
}

// maxDefaultValues bounds the values defaultContentType reads, a nested
// array's and a nested union's counted in (styleguide bounded-everything
// rule). A part whose values run past it takes no default.
const maxDefaultValues = 256

// defaultContentType returns the content type OpenAPI gives a part whose
// Encoding Object names none and the values it applies to are of the types
// values lists, as rules state it at the document's minor
// (docs/compilers/openapi.md#multipart-encoding): the one every value takes,
// a null passed over, and none where two differ, a value takes none, or none
// is left to read. encodedBy is what the part's declarations say of
// contentEncoding.
func defaultContentType(unions *partUnions, node func(ir.TypeID) (ir.TypeDef, bool), rules specrules.EncodingRules, values []ir.TypeRef, encodedBy contentEncoding) []string {
	pending := append([]ir.TypeRef(nil), values...)
	taken := ""
	for range maxDefaultValues {
		if len(pending) == 0 {
			break
		}
		valueReading := readValue(unions, node, rules, pending[0], encodedBy)
		pending = append(pending[1:], valueReading.instead...)
		switch {
		case valueReading.null || len(valueReading.instead) > 0:
		case valueReading.contentType == "" || taken != "" && valueReading.contentType != taken:
			return nil
		default:
			taken = valueReading.contentType
		}
	}
	if len(pending) > 0 || taken == "" {
		return nil
	}
	return []string{taken}
}

// valueReading is what one value says of its default content type: the one
// it takes, "" for none; that it is null, which takes none and needs none; or
// the values whose content types stand for its own, a nested union's members
// or, where the minor gives an array inside an array no row, its items.
type valueReading struct {
	contentType string
	instead     []ir.TypeRef
	null        bool
}

// readValue reads the value of type ref as its default content type is keyed
// (valueReading), through the alias scalars on its chain, noting the encoding
// the first scalar naming one names, and bounded by maxBodyAliasHops.
func readValue(unions *partUnions, node func(ir.TypeID) (ir.TypeDef, bool), rules specrules.EncodingRules, ref ir.TypeRef, encodedBy contentEncoding) valueReading {
	encoding := ""
	id := ref.Target
	for range maxBodyAliasHops {
		td, found := node(id)
		if !found {
			return valueReading{}
		}
		if scalar, alias := td.(*ir.Scalar); alias && scalar != nil && scalar.Base != nil {
			if encoding == "" && scalar.Encoding != nil {
				encoding = scalar.Encoding.Name
			}
			id = scalar.Base.Target
			continue
		}
		return readNode(unions, node, rules, td, encoding, encodedBy)
	}
	return valueReading{}
}

// readNode reads td, the node a value's alias chain ends on, encoding being
// the one a scalar on the chain names: a primitive, an enum and a literal by
// their primitive (primitiveCase), a model and a map as objects, an array
// inside an array by its row or else its items, and a union by its members.
func readNode(unions *partUnions, node func(ir.TypeID) (ir.TypeDef, bool), rules specrules.EncodingRules, td ir.TypeDef, encoding string, encodedBy contentEncoding) valueReading {
	if elems, isArray := elementsOf(td); isArray {
		if ct, ok := rules.ContentType(specrules.DefaultNestedArray); ok {
			return valueReading{contentType: ct}
		}
		return valueReading{instead: elems}
	}
	switch t := td.(type) {
	case *ir.Primitive:
		return byCase(rules, primitiveCase(t.PrimitiveKind, encoding, encodedBy))
	case *ir.Enum:
		return byCase(rules, primitiveCase(t.ValueType, encoding, encodedBy))
	case *ir.Literal:
		return readLiteral(rules, t, encoding, encodedBy)
	case *ir.Model, *ir.MapT:
		return byCase(rules, specrules.DefaultObject)
	case *ir.Union:
		if members, ok := unions.membersOf(node, t); ok && len(members) > 0 {
			return valueReading{instead: members}
		}
		return valueReading{}
	default:
		return valueReading{}
	}
}

// readLiteral reads a literal by its primitive, or, where it has none, by its
// value: null takes no content type, and an object one as an object does.
func readLiteral(rules specrules.EncodingRules, literal *ir.Literal, encoding string, encodedBy contentEncoding) valueReading {
	switch {
	case literal.ValueType != "":
		return byCase(rules, primitiveCase(literal.ValueType, encoding, encodedBy))
	case literal.Value.Kind == ir.ValueNull:
		return valueReading{null: true}
	case literal.Value.Kind == ir.ValueObject:
		return byCase(rules, specrules.DefaultObject)
	default:
		return valueReading{}
	}
}

// byCase reads the content type rules give case defaultCase, none for the zero
// case.
func byCase(rules specrules.EncodingRules, defaultCase specrules.DefaultCase) valueReading {
	ct, _ := rules.ContentType(defaultCase)
	return valueReading{contentType: ct}
}

// byteFormatEncoding is the encoding the schema lowering names `format: byte`
// by: of the formats it lowers to bytes sent as text, the one OpenAPI 3.0's
// table gives a row of its own, where `base64url` takes a string's.
const byteFormatEncoding = "base64"

// primitiveCase is the case OpenAPI's table keys a value of primitive kind
// primitiveKind by, encoding being the one a scalar over it names: bytes no
// scalar re-encodes are raw binary, bytes base64 re-encodes a string of format
// byte, bytes another encoding re-encodes, as base64url does, any other string,
// and strings are keyed by whether they write contentEncoding (encodedCase).
// The zero case, which has no row, is a kind of none.
func primitiveCase(primitiveKind ir.PrimitiveKind, encoding string, encodedBy contentEncoding) specrules.DefaultCase {
	switch primitiveKind {
	case ir.PrimitiveAny:
		return specrules.DefaultUntyped
	case ir.PrimitiveBytes:
		switch encoding {
		case "":
			return specrules.DefaultRawBinary
		case byteFormatEncoding:
			return encodedCase(encodedBy, specrules.DefaultByteFormat, specrules.DefaultEncodedByteFormat)
		default:
			return encodedCase(encodedBy, specrules.DefaultString, specrules.DefaultContentEncoded)
		}
	case ir.PrimitiveString, ir.PrimitiveDate, ir.PrimitiveTime, ir.PrimitiveDatetime, ir.PrimitiveDatetimeOffset, ir.PrimitiveDuration,
		ir.PrimitiveURL, ir.PrimitiveUUID:
		return encodedCase(encodedBy, specrules.DefaultString, specrules.DefaultContentEncoded)
	case ir.PrimitiveBool, ir.PrimitiveInt8, ir.PrimitiveInt16, ir.PrimitiveInt32, ir.PrimitiveInt64, ir.PrimitiveUint8, ir.PrimitiveUint16,
		ir.PrimitiveUint32, ir.PrimitiveUint64, ir.PrimitiveInteger, ir.PrimitiveFloat32, ir.PrimitiveFloat64, ir.PrimitiveFloat, ir.PrimitiveNumber,
		ir.PrimitiveDecimal, ir.PrimitiveDecimal128:
		return specrules.DefaultScalar
	default:
		return 0
	}
}

// encodedCase returns plain where a value's declarations write no
// contentEncoding, written where each does, and the zero case, none, where
// they disagree.
func encodedCase(encodedBy contentEncoding, plain, written specrules.DefaultCase) specrules.DefaultCase {
	switch encodedBy {
	case unencoded:
		return plain
	case encoded:
		return written
	default:
		return 0
	}
}

// itemDefault fills the content type of the items of a multipart sequential
// body whose item encoding names none, once the registry is whole
// (compile.Settlement): each item is one value of type item, so a list item is
// an array inside the array the encoding applies to. encoded is what the item
// schema says of contentEncoding, and c the context the content lowered in.
type itemDefault struct {
	c        service.Ctx
	encoding *ir.PartEncoding
	item     ir.TypeRef
	encoded  contentEncoding
}

// Settle writes the default to the item encoding and no node of reg.
func (d itemDefault) Settle(_ context.Context, reg ir.TypeRegistry) []ir.Diagnostic {
	node := func(id ir.TypeID) (ir.TypeDef, bool) {
		td, ok := reg[id]
		return td, ok
	}
	types := partUnionsOf(d.c).itemDefault(node, service.EncodingRules(d.c), d.item, d.encoded)
	d.encoding.ContentTypes = slices.Clone(types)
	return nil
}
