// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"slices"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

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
// of it, or of each element of an array it declares, must read so. A part
// declaring raw bytes is one; a part leaving its media type to say is one where
// its encoding entry names only binary media types (partContentFile), or, from
// OpenAPI 3.1 and naming none, where it writes no type, whose default media
// type is application/octet-stream. Only a multipart part can carry raw bytes.
func readPartFile(c lowering.Ctx, part bodyPart, contentTypes []string, multipart bool) partFile {
	if !multipart || len(part.schemas) == 0 {
		return partFile{}
	}
	least := readsBinary
	for _, js := range part.schemas {
		elem, ok := partElement(c, js)
		if !ok {
			return partFile{}
		}
		least = min(least, readFile(c, elem))
	}
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

// partElement returns the schema each value of a part declared by js has: the
// items of an array, else js. An array writing no items is not read, so ok is
// false: what its elements are is left unsaid, not said to be anything.
func partElement(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable]) (*oas3.JSONSchema[oas3.Referenceable], bool) {
	s := schemaOf(c.Bindings(), js)
	if s == nil {
		return js, true
	}
	if !slices.Equal(withoutNull(s.GetType()), []oas3.SchemaType{oas3.SchemaTypeArray}) {
		return js, true
	}
	items := s.GetItems()
	return items, items != nil
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
