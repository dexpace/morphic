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
	return fileFor(c, leastOf(c, part.schemas, readDeclaration), contentTypes)
}

// fileFor judges a multipart part or item whose schemas read as least, with
// contentTypes its encoding entry's: a file where it declares raw bytes; where
// it leaves its media type to say, one where the entry names only binary media
// types (partContentFile), or, naming none, as the media type its encoding
// defaults to (untypedPartFile).
func fileFor(c lowering.Ctx, least fileReading, contentTypes []string) partFile {
	switch {
	case least == readsBinary:
		return partFile{file: true}
	case !least.open():
		return partFile{}
	case len(contentTypes) > 0:
		return partContentFile(c, contentTypes)
	default:
		return untypedPartFile(c, least)
	}
}

// untypedPartFile judges a part read as least whose encoding names no media
// type: one writing no type is judged as naming the contentType the Encoding
// Object's rows default that case to (lowering.EncodingRules), which from
// OpenAPI 3.1 is application/octet-stream, and any other is no file.
func untypedPartFile(c lowering.Ctx, least fileReading) partFile {
	contentType, ok := lowering.EncodingRules(c).UntypedContentType()
	if least != readsUntyped || !ok {
		return partFile{}
	}
	return partContentFile(c, []string{contentType})
}

// readDeclaration reads js, one declaration of a part, as a file: an array
// through its elements (readElements), else as a body's schema is (readFile)
// but for an array branch of a union it writes, read through its elements too
// (readPartBranch).
func readDeclaration(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable]) fileReading {
	if s := schemaOf(c.Bindings(), js); isArraySchema(s) {
		return readElements(c, s)
	}
	return readSchema(c, js, readPartBranch)
}

// readPartBranch reads branch, one of a union a part's declaration writes, as a
// body's union branch is read (readBranch), but an array through its elements
// (readElements), as the part repeats where the branch holds.
func readPartBranch(c lowering.Ctx, branch *oas3.JSONSchema[oas3.Referenceable]) fileReading {
	if s := schemaOf(c.Bindings(), branch); isArraySchema(s) {
		return readElements(c, s)
	}
	return readBranch(c, branch)
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
	return leastOf(c, elems, readFile)
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
