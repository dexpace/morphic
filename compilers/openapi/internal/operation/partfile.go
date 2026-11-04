// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"slices"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// partFile is what a part's declarations and encoding entry say of it being a
// file, beside what its type says (partTypeFlags): whether they make it one,
// and the heuristics that read it in, empty where the source declares it.
type partFile struct {
	inferred ir.Inferred
	file     bool
}

// apply returns pe marked a file where f reads it one and its type did not, so
// the marker names the heuristic only where the file rests on it.
func (f partFile) apply(pe ir.PartEncoding) ir.PartEncoding {
	if !f.file || pe.Filename {
		return pe
	}
	pe.Filename, pe.Inferred = true, f.inferred
	return pe
}

// readPartFile reads part, a part of a multipart body or not as multipart says,
// as a file (docs/compilers/openapi.md#multipart-encoding): every declaration
// of it, or each element of an array or tuple it declares (partElements), must
// read so, as fileFor judges it. Only a multipart part can carry raw bytes. A
// union is read as a body's is, so one with a list of files among its branches
// is no file (GitHub #1448).
func readPartFile(c lowering.Ctx, part bodyPart, contentTypes []string, multipart bool) partFile {
	if !multipart || len(part.schemas) == 0 {
		return partFile{}
	}
	least := readsBinary
	for _, js := range part.schemas {
		elems, ok := partElements(c, js)
		if !ok {
			return partFile{}
		}
		for _, elem := range elems {
			least = min(least, readFile(c, elem))
		}
	}
	return fileFor(c, least, contentTypes)
}

// fileFor judges a multipart part or item whose schemas read as least, with
// contentTypes its encoding entry's: a file where it declares raw bytes; where
// it leaves its media type to say, one where the entry names only binary media
// types (partContentFile), or, from OpenAPI 3.1 and naming none, where it
// writes no type, whose default media type is application/octet-stream.
func fileFor(c lowering.Ctx, least fileReading, contentTypes []string) partFile {
	switch {
	case least == readsBinary:
		return partFile{file: true}
	case !least.open():
		return partFile{}
	case len(contentTypes) > 0:
		return partContentFile(c, contentTypes)
	default:
		return partFile{file: least == readsUntyped && !c.Is30()}
	}
}

// partElements returns the schemas the values of a part declared by js have:
// an array's prefixItems and the items following them, items false admitting
// none, else js alone. An array writing no items is not read, so ok is false:
// what the elements past its prefix are is left unsaid, not said to be
// anything. Neither is one admitting no element at all.
func partElements(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable]) ([]*oas3.JSONSchema[oas3.Referenceable], bool) {
	s := schemaOf(c.Bindings(), js)
	if s == nil || !slices.Equal(withoutNull(s.GetType()), []oas3.SchemaType{oas3.SchemaTypeArray}) {
		return []*oas3.JSONSchema[oas3.Referenceable]{js}, true
	}
	items := s.GetItems()
	if !annotation.Written(items) {
		return nil, false
	}
	elems := slices.Clone(s.GetPrefixItems())
	if !annotation.IsFalseSchema(items) {
		elems = append(elems, items)
	}
	return elems, len(elems) > 0
}

// partContentFile reads a part whose schema leaves its media type to say, and
// whose encoding entry names contentTypes, as a file where every type named is
// binary: application/octet-stream as declared, any other the binary policy
// names marked as its reading.
func partContentFile(c lowering.Ctx, contentTypes []string) partFile {
	out := partFile{file: true}
	for _, ct := range contentTypes {
		switch {
		case lowering.NormalizeMediaType(ct) == octetStream:
		case c.MediaTypeBinary(ct):
			out.inferred = lowering.BinaryMediaTypeHeuristic
		default:
			return partFile{}
		}
	}
	return out
}

// rawTuple reports whether every element t admits is raw bytes (rawBytes):
// each of Elems and, where elements may follow them, Rest. A tuple admitting
// no element at all is no file.
func rawTuple(node func(ir.TypeID) (ir.TypeDef, bool), t *ir.Tuple) bool {
	if len(t.Elems) == 0 && t.Rest == nil {
		return false
	}
	for _, elem := range t.Elems {
		if !rawBytes(node, elem) {
			return false
		}
	}
	return t.Rest == nil || rawBytes(node, *t.Rest)
}

// rawBytes reports whether ref ends in bytes that travel raw, through the alias
// scalars a hoisted position puts over the primitive, none of which re-encodes
// it: a `format: byte` element is base64 text. The walk is bounded by
// maxBodyAliasHops.
func rawBytes(node func(ir.TypeID) (ir.TypeDef, bool), ref ir.TypeRef) bool {
	id := ref.Target
	for range maxBodyAliasHops {
		td, found := node(id)
		if !found {
			return false
		}
		switch t := td.(type) {
		case *ir.Primitive:
			return t.Prim == ir.PrimBytes
		case *ir.Scalar:
			if t.Base == nil || t.Encoding != nil && t.Encoding.Name != "" {
				return false
			}
			id = t.Base.Target
		default:
			return false
		}
	}
	return false
}
