// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"slices"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// keySet is a set of keys of readDomain, those whose home depends on what a
// position lowered to: a build says which of them it read of the schema it was
// built from, typing it, deciding its shape by it, or finding it says nothing
// the node does not, and the finishing of its node keeps the rest.
type keySet uint32

// nodeKeywords are the keywords whose home a node's kind decides.
var nodeKeywords = []string{
	"additionalItems", "additionalProperties", "const", "discriminator", "enum", "format", "items",
	"maxItems", "minItems", "patternProperties", "prefixItems", "properties", "required", "type", "uniqueItems",
}

// readDomain lists the keys a keySet holds, each at its bit: nodeKeywords,
// then the value bounds (valueConstraintKeywords).
var readDomain = slices.Concat(nodeKeywords, valueConstraintKeywords)

// keyBit returns the bit of name, and none for a key outside readDomain.
func keyBit(name string) keySet {
	i := slices.Index(readDomain, name)
	if i < 0 {
		return 0
	}
	return 1 << i
}

// keySetOf returns the set of names.
func keySetOf(names ...string) keySet {
	var out keySet
	for _, n := range names {
		out |= keyBit(n)
	}
	return out
}

// has reports whether k holds name.
func (k keySet) has(name string) bool {
	b := keyBit(name)
	return b != 0 && k&b != 0
}

// objectKeys are what an object's build reads of the schema it is built from:
// its members, their catch-alls and which are required.
var objectKeys = keySetOf("properties", "patternProperties", "additionalProperties", "required")

// typeReadAs returns the type s writes where a node of kind reads it: every
// type s names is kind, as its shape says.
func typeReadAs(s *statement.Schema, kind statement.Kind) keySet {
	types := effectiveTypes(s)
	if len(types) == 0 || slices.ContainsFunc(types, func(t statement.Kind) bool { return t != kind }) {
		return 0
	}
	return keyBit("type")
}

// scalarTypeRead returns the type s writes where a scalar's build reads it:
// every type s names is one of a primitive's.
func scalarTypeRead(s *statement.Schema) keySet {
	types := effectiveTypes(s)
	if len(types) == 0 || slices.ContainsFunc(types, func(t statement.Kind) bool {
		return t == statement.KindObject || t == statement.KindArray
	}) {
		return 0
	}
	return keyBit("type")
}

// boundsRead returns the value bounds s writes that a reader of the kinds held
// reads: those of its kinds, a count the load phase refuses, which no reader
// holds, and one the reader kept on its node, p (keepCounts, keepTextBounds).
func boundsRead(s *statement.Schema, held kindSet, p ir.Unmodeled) keySet {
	var out keySet
	for _, k := range declaredConstraints(s) {
		_, kept := p[annotation.EntryKey("", k)]
		if taken(held, k) || refusedCount(s, k) || kept {
			out |= keyBit(k)
		}
	}
	return out
}

// nodeBoundsRead is boundsRead for the reader of a node's Constraints, cons:
// where it holds none, it read none of its kinds, as no field holds what it
// took, such as an empty pattern, and the node keeps the bound verbatim.
func nodeBoundsRead(s *statement.Schema, held kindSet, cons *ir.Constraints, p ir.Unmodeled) keySet {
	if cons == nil {
		held = 0
	}
	return boundsRead(s, held, p)
}

// collectionReads returns the collection bounds s writes that a List or a Tuple
// reads into its Constraints (collectionConstraints): uniqueItems, and a count
// a field states (keptCount).
func collectionReads(s *statement.Schema) keySet {
	out := keyBit("uniqueItems")
	for _, k := range []string{"minItems", "maxItems"} {
		if !keptCount(s, k, countOf(s, k)) {
			out |= keyBit(k)
		}
	}
	return out
}

// readElsewhere returns the keys of readDomain s writes that a reader outside
// the build of the node it lowers to reads: a redaction format, the annotation
// readers'; an additionalItems beside an items that is no list, the rest of
// nothing, the unknown keywords' keeper's (PreserveUnknownKeywords); and a type
// naming null alone, which the nulls reader reads as TypeRef.Nullable
// (docs/compilers/openapi.md#null-alone).
func readElsewhere(s *statement.Schema) keySet {
	var out keySet
	if annotation.DeclaresRedaction(s) {
		out |= keyBit("format")
	}
	if _, listed := listedItems(s); !listed {
		out |= keyBit("additionalItems")
	}
	if writesType(s) && len(effectiveTypes(s)) == 0 {
		out |= keyBit("type")
	}
	return out
}

// readBeside is readElsewhere for td, the node a position lowered to: the
// type of an Enum or a Literal is its own typing's to read (valueSetReads),
// which the nulls reader leaves.
func readBeside(s *statement.Schema, td ir.TypeDef) keySet {
	read := readElsewhere(s)
	if isKind(td, ir.KindEnum) || isKind(td, ir.KindLiteral) {
		read &^= keyBit("type")
	}
	return read
}

// referenceReads returns what the alias or carrier of a position s lowered to
// a reference reads (referenceConstraintsOf, CarrierConstraintsOf): the value
// bounds the types it admits take, and where they admit an array its
// collection bounds (boundHomes.reads).
func referenceReads(c lowering.Ctx, s *statement.Schema) keySet {
	takes := boundKinds(c, s)
	return boundHomes{value: takes, collection: takes&kindArray != 0}.reads(s)
}
