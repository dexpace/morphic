// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
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
func readContentEncoding(c lowering.Ctx, schemas []*oas3.JSONSchema[oas3.Referenceable]) contentEncoding {
	writes, omits := false, false
	for _, js := range schemas {
		s := schemaOf(c.Bindings(), js)
		if s == nil {
			continue
		}
		if schema.DecidingSchema(c, s).GetContentEncoding() != "" {
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
// §4.15.1). ok is false where a union's members are not read.
func partValues(node func(ir.TypeID) (ir.TypeDef, bool), decls []*ir.Property) (values []ir.TypeRef, ok bool) {
	for _, d := range decls {
		members := []ir.TypeRef{d.Type}
		if u, isUnion := bodyNode(node, d.Type.Target).(*ir.Union); isUnion && u != nil {
			if members, ok = unionMembers(node, u); !ok {
				return nil, false
			}
		}
		for _, m := range members {
			values = append(values, itemsOf(node, m)...)
		}
	}
	return values, true
}

// withDefaultContentType returns pe, the encoding of p, a part decls declare,
// with the content type OpenAPI gives it by default where it takes one
// (keyedPart.defaulted).
func withDefaultContentType(node func(ir.TypeID) (ir.TypeDef, bool), rules oas.EncodingRules, decls []*ir.Property, p keyedPart, pe ir.PartEncoding) ir.PartEncoding {
	if !p.defaulted {
		return pe
	}
	if values, ok := partValues(node, decls); ok {
		pe.ContentTypes = defaultContentType(node, rules, values, p.encoded)
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
		return []ir.TypeRef{t.Elem}, true
	case *ir.Tuple:
		elems = append(elems, t.Elems...)
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
func defaultContentType(node func(ir.TypeID) (ir.TypeDef, bool), rules oas.EncodingRules, values []ir.TypeRef, encodedBy contentEncoding) []string {
	pending := append([]ir.TypeRef(nil), values...)
	taken := ""
	for range maxDefaultValues {
		if len(pending) == 0 {
			break
		}
		r := readValue(node, rules, pending[0], encodedBy)
		pending = append(pending[1:], r.instead...)
		switch {
		case r.null || len(r.instead) > 0:
		case r.contentType == "" || taken != "" && r.contentType != taken:
			return nil
		default:
			taken = r.contentType
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
// (valueReading), through the alias scalars on its chain, noting a scalar that
// names an encoding, and bounded by maxBodyAliasHops.
func readValue(node func(ir.TypeID) (ir.TypeDef, bool), rules oas.EncodingRules, ref ir.TypeRef, encodedBy contentEncoding) valueReading {
	named := false
	id := ref.Target
	for range maxBodyAliasHops {
		td, found := node(id)
		if !found {
			return valueReading{}
		}
		if s, alias := td.(*ir.Scalar); alias && s != nil && s.Base != nil {
			named = named || s.Encoding != nil && s.Encoding.Name != ""
			id = s.Base.Target
			continue
		}
		return readNode(node, rules, td, named, encodedBy)
	}
	return valueReading{}
}

// readNode reads td, the node a value's alias chain ends on, named saying a
// scalar on the chain names an encoding: a primitive, an enum and a literal by
// their primitive (primitiveCase), a model and a map as objects, an array
// inside an array by its row or else its items, and a union by its members.
func readNode(node func(ir.TypeID) (ir.TypeDef, bool), rules oas.EncodingRules, td ir.TypeDef, named bool, encodedBy contentEncoding) valueReading {
	if elems, isArray := elementsOf(td); isArray {
		if ct, ok := rules.ContentType(oas.DefaultNestedArray); ok {
			return valueReading{contentType: ct}
		}
		return valueReading{instead: elems}
	}
	switch t := td.(type) {
	case *ir.Primitive:
		return byCase(rules, primitiveCase(t.Prim, named, encodedBy))
	case *ir.Enum:
		return byCase(rules, primitiveCase(t.ValueType, named, encodedBy))
	case *ir.Literal:
		return readLiteral(rules, t, named, encodedBy)
	case *ir.Model, *ir.MapT:
		return byCase(rules, oas.DefaultObject)
	case *ir.Union:
		if members, ok := unionMembers(node, t); ok && len(members) > 0 {
			return valueReading{instead: members}
		}
		return valueReading{}
	default:
		return valueReading{}
	}
}

// readLiteral reads a literal by its primitive, or, where it has none, by its
// value: null takes no content type, and an object one as an object does.
func readLiteral(rules oas.EncodingRules, l *ir.Literal, named bool, encodedBy contentEncoding) valueReading {
	switch {
	case l.ValueType != "":
		return byCase(rules, primitiveCase(l.ValueType, named, encodedBy))
	case l.Value.Kind == ir.ValueNull:
		return valueReading{null: true}
	case l.Value.Kind == ir.ValueObject:
		return byCase(rules, oas.DefaultObject)
	default:
		return valueReading{}
	}
}

// byCase reads the content type rules give case k, none for the zero case.
func byCase(rules oas.EncodingRules, k oas.DefaultCase) valueReading {
	ct, _ := rules.ContentType(k)
	return valueReading{contentType: ct}
}

// primitiveCase is the case OpenAPI's table keys a value of primitive kind p
// by, named saying a scalar over it names an encoding: bytes no scalar
// re-encodes are raw binary, bytes one does a string of format byte, and those
// and other strings are keyed by whether they write contentEncoding
// (encodedCase). The zero case, which has no row, is a kind of none.
func primitiveCase(p ir.PrimKind, named bool, encodedBy contentEncoding) oas.DefaultCase {
	switch {
	case p == ir.PrimAny:
		return oas.DefaultUntyped
	case p == ir.PrimBytes && !named:
		return oas.DefaultRawBinary
	case p == ir.PrimBytes:
		return encodedCase(encodedBy, oas.DefaultByteFormat, oas.DefaultEncodedByteFormat)
	case textPrimitive(p):
		return encodedCase(encodedBy, oas.DefaultString, oas.DefaultContentEncoded)
	case p.Valid():
		return oas.DefaultScalar
	default:
		return 0
	}
}

// encodedCase returns plain where a value's declarations write no
// contentEncoding, written where each does, and the zero case, none, where
// they disagree.
func encodedCase(encodedBy contentEncoding, plain, written oas.DefaultCase) oas.DefaultCase {
	switch encodedBy {
	case unencoded:
		return plain
	case encoded:
		return written
	default:
		return 0
	}
}

// textPrimitive reports whether a value of kind p travels as a JSON string:
// a string, or a date, a time, a duration, a URL or a UUID a format names.
func textPrimitive(p ir.PrimKind) bool {
	switch p {
	case ir.PrimString, ir.PrimDate, ir.PrimTime, ir.PrimDatetime, ir.PrimDatetimeOffset, ir.PrimDuration,
		ir.PrimURL, ir.PrimUUID:
		return true
	default:
		return false
	}
}

// itemDefault fills the content type of the items of a multipart sequential
// body whose item encoding names none, once the registry is whole
// (compile.Settlement): each item is one value of type item, so a list item is
// an array inside the array the encoding applies to. encoded is what the item
// schema says of contentEncoding, and c the context the content lowered in.
type itemDefault struct {
	c        lowering.Ctx
	encoding *ir.PartEncoding
	item     ir.TypeRef
	encoded  contentEncoding
}

// Settle writes the default to the item encoding and no node of reg.
func (d itemDefault) Settle(reg ir.TypeRegistry) []ir.Diagnostic {
	node := func(id ir.TypeID) (ir.TypeDef, bool) {
		td, ok := reg[id]
		return td, ok
	}
	d.encoding.ContentTypes = defaultContentType(node, lowering.EncodingRules(d.c), []ir.TypeRef{d.item}, d.encoded)
	return nil
}
