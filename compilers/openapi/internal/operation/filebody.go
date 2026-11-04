// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"slices"
	"strings"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// fileReading is what a schema says of a body or part being a file
// (docs/compilers/openapi.md#file-bodies). The readings run from saying the
// value is no file to declaring one, so several schemas read together read as
// the least of them, the most each one allows.
type fileReading uint8

const (
	// readsOther is a schema saying the value is something other than raw
	// bytes: another type, a format, contentEncoding or constraint naming a
	// text, a composition, or false.
	readsOther fileReading = iota
	// readsString is a schema saying no more than that the value is a string,
	// which leaves its media type to say how it travels.
	readsString
	// readsUntyped is a schema writing no type and nothing else that constrains
	// the value: absent, true, or annotations alone.
	readsUntyped
	// readsBinary is a schema declaring raw bytes: format binary on a string
	// or on a schema writing no type.
	readsBinary
)

// open reports whether r leaves the media type to decide whether the value is
// a file.
func (r fileReading) open() bool { return r == readsString || r == readsUntyped }

// readFile reads js, a body's or a part's schema, as a file: through a $ref to
// the schema its chain ends on, and through the branches of a union it writes
// (readUnion), each as readBranch reads it. Raw bytes are read through a
// refinement to the schema deciding it (schema.DecidingSchema). An absent
// schema, and a null written for one, is untyped.
func readFile(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable]) fileReading {
	return readSchema(c, js, readBranch)
}

// readSchema reads js as readFile does, with branch reading each branch of a
// union js writes.
func readSchema(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable], branch branchReader) fileReading {
	s, head, decided := readHead(c, js)
	if decided {
		return head
	}
	if !besideRefsOpen(c.Bindings(), js) {
		return readsOther
	}
	own := readOpen(s, true /* unions */)
	union := slices.Concat(s.GetAnyOf(), s.GetOneOf())
	if !own.open() || len(union) == 0 {
		return own
	}
	// The union holds beside what s says itself, so a string s keeps its
	// branches to strings.
	if read := readUnion(c, union, branch); own != readsString || read != readsUntyped {
		return read
	}
	return readsString
}

// branchReader reads one branch of a union as a file.
type branchReader func(lowering.Ctx, *oas3.JSONSchema[oas3.Referenceable]) fileReading

// readBranch reads js, a union's branch, as readFile does, but a branch that
// is itself a union reads as other, so no reading descends past one union.
func readBranch(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable]) fileReading {
	s, head, decided := readHead(c, js)
	if decided {
		return head
	}
	if !besideRefsOpen(c.Bindings(), js) {
		return readsOther
	}
	return readOpen(s, false /* unions */)
}

// maxFileRefHops bounds the $refs besideRefsOpen follows (styleguide
// bounded-everything rule). A chain of more reads as saying something else, the
// answer a schema leaving its media type to say had before this reading.
const maxFileRefHops = 64

// besideRefsOpen reports whether each $ref on the chain from js writes beside it
// only what a file's schema may (openKeyword), type aside: a keyword beside a
// $ref holds with what the $ref names, beside a 3.0 $ref too as the schema
// lowering reads one, so `{$ref: X, contentEncoding: base64}` says a body is
// text where X says nothing.
func besideRefsOpen(refs *typed.Bindings, js *oas3.JSONSchema[oas3.Referenceable]) bool {
	for range maxFileRefHops + 1 {
		if js == nil || js.IsBool() || js.GetSchema().Ref == nil {
			return true
		}
		if !writesOnly(js.GetSchema(), func(key string) bool {
			return key == "$ref" || key != "type" && openKeyword(key, false /* unions */)
		}) {
			return false
		}
		js = refs.Declared(js)
	}
	return false
}

// readHead reads what decides js, through the chain of $refs it starts, without
// its keywords: its absence, a boolean or a null, or raw bytes it declares.
// Where none does, decided is false and s is the schema whose keywords the
// caller reads, never nil, as a position holding no schema reads as a boolean.
func readHead(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable]) (s *oas3.Schema, r fileReading, decided bool) {
	end := refEnd(c.Bindings(), js)
	switch {
	case !annotation.Written(end):
		return nil, readsUntyped, true
	case annotation.IsFalseSchema(end):
		return nil, readsOther, true
	case end.IsBool():
		return nil, readsUntyped, true
	}
	s = end.GetSchema()
	if declaresBinary(schema.DecidingSchema(c, s)) {
		return nil, readsBinary, true
	}
	return s, readsOther, false
}

// refEnd returns the schema position the chain of $refs from js ends on, as
// refs resolved it, so a $ref to a boolean schema reads as that boolean; js
// itself where it is no $ref or its chain ends nowhere.
func refEnd(refs *typed.Bindings, js *oas3.JSONSchema[oas3.Referenceable]) *oas3.JSONSchema[oas3.Referenceable] {
	if js == nil || js.IsBool() || js.GetSchema().Ref == nil {
		return js
	}
	if end := refs.End(js); end != nil {
		return end
	}
	return js
}

// declaresBinary reports whether s declares raw bytes: format binary on a
// schema whose type includes string or is unwritten.
func declaresBinary(s *oas3.Schema) bool {
	if s == nil || s.GetFormat() != "binary" {
		return false
	}
	types := s.GetType()
	return len(types) == 0 || slices.Contains(types, oas3.SchemaTypeString)
}

// readOpen reads s, a schema declaring no raw bytes, as a string, untyped or
// other: other where it writes a keyword no file leaves open (openKeyword), or
// a type other than string, alone or with null.
func readOpen(s *oas3.Schema, unions bool) fileReading {
	if !writesOnlyOpenKeywords(s, unions) {
		return readsOther
	}
	types := withoutNull(s.GetType())
	switch {
	case len(s.GetType()) == 0:
		return readsUntyped
	case len(types) == 1 && types[0] == oas3.SchemaTypeString:
		return readsString
	default:
		return readsOther
	}
}

// writesOnlyOpenKeywords reports whether every keyword s writes is one a file's
// schema may write (openKeyword).
func writesOnlyOpenKeywords(s *oas3.Schema, unions bool) bool {
	return writesOnly(s, func(key string) bool { return openKeyword(key, unions) })
}

// writesOnly reports whether every keyword s writes is one allowed takes, a
// null standing for a keyword left out passing whatever its key.
func writesOnly(s *oas3.Schema, allowed func(string) bool) bool {
	for _, p := range nodeview.Pairs(s.GetRootNode()) {
		if !allowed(p.Key) && !annotation.WritesKeywordLeftOut(s, p.Key) {
			return false
		}
	}
	return true
}

// openKeyword reports whether a schema writing key can still leave its media
// type to say how its value travels: an annotation, a length bound, which
// OpenAPI applies to a binary payload's octets, contentMediaType, which a media
// type set by the content's key or an encoding entry overrides, its own
// extensions, type, which readOpen reads, and a union, where unions says so.
func openKeyword(key string, unions bool) bool {
	switch key {
	case "type", "$comment", "contentMediaType", "default", "deprecated", "description", "example", "examples",
		"externalDocs", "maxLength", "minLength", "nullable", "readOnly", "title", "writeOnly", "xml":
		return true
	case "anyOf", "oneOf":
		return unions
	default:
		return strings.HasPrefix(key, "x-")
	}
}

// readUnion reads the branches of an anyOf or oneOf as one, each as read reads
// it: the least of them, since a file must be every value any branch admits. A
// branch admitting no value but null, or none at all, says nothing of the
// bytes, so it is passed over; a union of nothing else is other.
func readUnion(c lowering.Ctx, branches []*oas3.JSONSchema[oas3.Referenceable], read branchReader) fileReading {
	refs := c.Bindings()
	if !slices.ContainsFunc(branches, func(branch *oas3.JSONSchema[oas3.Referenceable]) bool {
		return !admitsNoBytes(refs, branch)
	}) {
		return readsOther
	}
	return leastOf(c, branches, func(c lowering.Ctx, branch *oas3.JSONSchema[oas3.Referenceable]) fileReading {
		if admitsNoBytes(c.Bindings(), branch) {
			return readsBinary // the top, which leaves the least of the others
		}
		return read(c, branch)
	})
}

// leastOf returns the least reading read gives any of items, readsBinary for
// none. It stops at readsOther, which no item read later can lower, so a wide
// union that one branch decides costs each use that branch alone; one no
// branch decides is read whole at each use (GitHub #1460).
func leastOf[T any](c lowering.Ctx, items []T, read func(lowering.Ctx, T) fileReading) fileReading {
	least := readsBinary
	for _, item := range items {
		if least = min(least, read(c, item)); least == readsOther {
			return readsOther
		}
	}
	return least
}

// admitsNoBytes reports whether branch, one of a union's, admits no value but
// null, or none at all, read through the $refs it starts: a false schema, or a
// type null writing nothing else a reader of a file looks at.
func admitsNoBytes(refs *typed.Bindings, branch *oas3.JSONSchema[oas3.Referenceable]) bool {
	if annotation.IsFalseSchema(refEnd(refs, branch)) {
		return true
	}
	s := schemaOf(refs, branch)
	return s != nil && slices.Equal(s.GetType(), []oas3.SchemaType{oas3.SchemaTypeNull}) &&
		writesOnlyOpenKeywords(s, false /* unions */)
}

// withoutNull returns types with null removed, a copy so the schema's own
// list is left as written.
func withoutNull(types []oas3.SchemaType) []oas3.SchemaType {
	return slices.DeleteFunc(slices.Clone(types), func(t oas3.SchemaType) bool {
		return t == oas3.SchemaTypeNull
	})
}

// bodyFile returns the FileInfo of a content entry under mt whose schema reads
// as r, or nil where it is no file body: declared where the schema declares raw
// bytes, or where it leaves the media type to say and that is
// application/octet-stream, which RFC 2046 defines as arbitrary octets; marked
// where it leaves it to a media type the binary policy names.
func bodyFile(c lowering.Ctx, mt string, r fileReading) *ir.FileInfo {
	file := &ir.FileInfo{IsText: false, ContentTypes: []string{mt}}
	switch {
	case r == readsBinary:
		return file
	case !r.open():
		return nil
	case lowering.NormalizeMediaType(mt) == octetStream:
		return file
	case c.MediaTypeBinary(mt):
		file.Inferred = lowering.BinaryMediaTypeHeuristic
		return file
	default:
		return nil
	}
}

// octetStream is the media type RFC 2046 defines as arbitrary octets, which
// OpenAPI's multipart defaults also name.
const octetStream = "application/octet-stream"
