// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"fmt"
	"slices"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// keySet is a set of keys of censusDomain, those whose home depends on what a
// position lowered to: a build says which of them it read of the schema it was
// built from, typing it, deciding its shape by it, or finding it says nothing
// the node does not, and the finishing of its node keeps the rest.
type keySet uint32

// censusDomain lists the keys a keySet holds, each at its bit: the keywords a
// node's kind decides the home of, then the value bounds.
var censusDomain = [...]string{
	"additionalItems", "additionalProperties", "const", "discriminator", "enum", "format", "items",
	"maxItems", "minItems", "patternProperties", "prefixItems", "properties", "required", "type", "uniqueItems",
	"exclusiveMaximum", "exclusiveMinimum", "maxLength", "maxProperties", "maximum", "minLength",
	"minProperties", "minimum", "multipleOf", "pattern",
}

// keyBit returns the bit of name, and none for a key outside censusDomain.
func keyBit(name string) keySet {
	i := slices.Index(censusDomain[:], name)
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

// declaredReads returns what a value set's typing reads of s: the type and
// format declaredPrim selects a primitive by, where fits says the values are of
// it, the type alone where it selects by the type alone.
func declaredReads(s *statement.Schema, fits func(ir.PrimKind) bool) keySet {
	if prim, paired := formatPrimitive(s); paired && fits(prim) {
		return keySetOf("type", "format")
	}
	if _, ok := declaredPrim(s, fits); ok {
		return keyBit("type")
	}
	return 0
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

// readElsewhere returns the keys of censusDomain s writes that a reader outside
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

// referenceReads returns what the alias or carrier of a position s lowered to
// a reference reads (referenceConstraintsOf, CarrierConstraintsOf): the value
// bounds the types it admits take, and where they admit an array the
// collection bounds a field states, uniqueItems only where it holds it, true
// (referenceHolds).
func referenceReads(c lowering.Ctx, s *statement.Schema) keySet {
	takes := boundKinds(c, s)
	out := boundsRead(s, takes, nil)
	if takes&kindArray != 0 {
		out |= collectionReads(s) &^ keyBit("uniqueItems")
		if referenceHolds(s, "uniqueItems") {
			out |= keyBit("uniqueItems")
		}
	}
	return out
}

// readCheck records, for a test that asks for it before lowering, each key of
// censusDomain a census site keeps where the builds it judges read it, or
// homes where none read it.
type readCheck struct {
	disagreements []string
}

// readCheckOf keys a run's readCheck.
type readCheckOf struct{}

// readCheckIn returns the readCheck a test seeded c's run with, nil where none
// did.
func readCheckIn(c lowering.Ctx) *readCheck {
	return lowering.Remembered(c, readCheckOf{}, func() *readCheck { return nil })
}

// compare records, for s written at at and its node of kind, each key it
// writes but handled where kept, the census's verdict, and read disagree.
func (r *readCheck) compare(site, kind string, s *statement.Schema, read keySet, handled, kept []string, at string) {
	for _, k := range censusDomain {
		if !hasKey(s, k) || slices.Contains(handled, k) {
			continue
		}
		isKept, isRead := slices.Contains(kept, k), read.has(k)
		if isKept == !isRead {
			continue
		}
		verdict := "kept, read"
		if !isKept {
			verdict = "homed, unread"
		}
		r.disagreements = append(r.disagreements, fmt.Sprintf("%s\t%s\t%s\t%s\t%s", site, kind, k, verdict, at))
	}
}
