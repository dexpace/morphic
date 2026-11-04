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
// of it must read so (readDeclaration), as fileFor judges it. Only a multipart
// part can carry raw bytes.
func readPartFile(c lowering.Ctx, part bodyPart, contentTypes []string, multipart bool) partFile {
	if !multipart || len(part.schemas) == 0 {
		return partFile{}
	}
	least := readsBinary
	for _, js := range part.schemas {
		least = min(least, readDeclaration(c, js))
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

// readDeclaration reads js, one declaration of a part, as a file: an array
// through its elements (readElements); a union with an array among its
// branches as a body's union is read, an array branch through its elements
// (readArrayUnion); else as a body's schema is (readFile).
func readDeclaration(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable]) fileReading {
	s := schemaOf(c.Bindings(), js)
	switch {
	case isArraySchema(s):
		return readElements(c, s)
	case unionOfArrays(c, js, s):
		return readArrayUnion(c, slices.Concat(s.GetAnyOf(), s.GetOneOf()))
	default:
		return readFile(c, js)
	}
}

// isArraySchema reports whether s writes the type array, alone or with null.
func isArraySchema(s *oas3.Schema) bool {
	return s != nil && slices.Equal(withoutNull(s.GetType()), []oas3.SchemaType{oas3.SchemaTypeArray})
}

// readElements reads the elements of the array s as one: its prefixItems and
// the items following them, items false admitting none. An array writing no
// items reads as other, as one admitting no element does: what its elements
// past its prefix are is left unsaid, not said to be anything.
func readElements(c lowering.Ctx, s *oas3.Schema) fileReading {
	items := s.GetItems()
	if !annotation.Written(items) {
		return readsOther
	}
	elems := slices.Clone(s.GetPrefixItems())
	if !annotation.IsFalseSchema(items) {
		elems = append(elems, items)
	}
	if len(elems) == 0 {
		return readsOther
	}
	least := readsBinary
	for _, elem := range elems {
		least = min(least, readFile(c, elem))
	}
	return least
}

// unionOfArrays reports whether s, which js declares, is a union with an array
// among its branches, writing beside it, and beside each $ref to it, only what a
// file's schema may (openKeyword) and no type, which would hold beside every
// branch. Any other union is read as a body's is (readFile).
func unionOfArrays(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable], s *oas3.Schema) bool {
	if s == nil || len(withoutNull(s.GetType())) > 0 || !writesOnlyOpenKeywords(s, true /* unions */) ||
		!besideRefsOpen(c.Bindings(), js) {
		return false
	}
	return slices.ContainsFunc(slices.Concat(s.GetAnyOf(), s.GetOneOf()), func(branch *oas3.JSONSchema[oas3.Referenceable]) bool {
		return isArraySchema(schemaOf(c.Bindings(), branch))
	})
}

// readArrayUnion reads the branches of a union with an array among them as
// readUnion does, the least of them, an array branch through its elements
// (readElements) and any other as one branch (readBranch). A branch admitting
// only null is passed over; an array branch is never one, so some branch is
// read.
func readArrayUnion(c lowering.Ctx, branches []*oas3.JSONSchema[oas3.Referenceable]) fileReading {
	least := readsBinary
	for _, branch := range branches {
		bs := schemaOf(c.Bindings(), branch)
		switch {
		case admitsOnlyNull(bs):
		case isArraySchema(bs):
			least = min(least, readElements(c, bs))
		default:
			least = min(least, readBranch(c, branch))
		}
	}
	return least
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
