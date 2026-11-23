// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"bytes"
	"encoding/json/v2"
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
)

// noteGroups are the keywords writing each note an allOf's fold settles for a
// member: one title, description, external link list and deprecation it keeps,
// and the examples it adopts, written under either keyword.
var noteGroups = [][]string{{"title"}, {"description"}, {"externalDocs"}, {"deprecated"}, {"example", "examples"}}

// notesGivenApart reports whether another declaration of the member schema
// declares, which the same model writes itself (ownRedeclarations), gives a
// note schema writes otherwise. The fold then keeps one on the field and the
// other on the node its declaration's schema builds, so a use of a $ref writing
// it keeps that node (usesOneRef). Values compare as their JSON encodings, so
// two that only spell one value apart read as apart, which keeps a node.
func notesGivenApart(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	for _, other := range ownRedeclarations(loweringContext, schema) {
		for _, group := range noteGroups {
			if writesAnyOf(schema, group) && writesAnyOf(other, group) && !sameNotes(schema, other, group) {
				return true
			}
		}
	}
	return false
}

// ownRedeclarations returns the other declarations of the property schema is
// the schema of that the model holding it writes itself: those of its name
// that model and each of its inline allOf entries list, the model read up
// through the entries schema's holder is nested in (composingModel). It is nil
// where schema is no property.
func ownRedeclarations(loweringContext lowering.Ctx, schema *statement.Schema) []*statement.Schema {
	at, _ := siteOf(schema)
	if at.Pointer.Parent().LastToken() != "properties" {
		return nil
	}
	statements := loweringContext.Statements()
	model := composingModel(loweringContext, reftable.DocumentPointer{Doc: at.Doc, Pointer: at.Pointer.Parent().Parent()})
	listing := []*statement.Schema{model.Schema}
	for _, entry := range statements.Read(model.Schema).Entries {
		if entry.Ref == nil {
			listing = append(listing, entry.Schema)
		}
	}
	var others []*statement.Schema
	for _, lister := range listing {
		subschema, ok := lister.Property(at.Pointer.LastToken())
		if !ok {
			continue
		}
		if declared := statements.Subschema(subschema).Schema; declared != schema {
			others = append(others, declared)
		}
	}
	return others
}

// composingModel returns the statement of the schema whose allOf lists the
// schema at holder as an inline entry, read up through every entry it is
// nested in to the outermost, or holder's own where no allOf lists it: the
// model whose fold meets the members those entries declare. It climbs at most
// statement.MaxFlattenDepth, past which the entries are not flattened.
func composingModel(loweringContext lowering.Ctx, holder reftable.DocumentPointer) *statement.Statement {
	statements := loweringContext.Statements()
	current := statements.At(holder)
	for range statement.MaxFlattenDepth {
		if holder.Pointer.Parent().LastToken() != "allOf" {
			break
		}
		parentAt := reftable.DocumentPointer{Doc: holder.Doc, Pointer: holder.Pointer.Parent().Parent()}
		parent := statements.At(parentAt)
		if !slices.ContainsFunc(parent.AllOf, func(sub statement.Subschema) bool { return statements.Subschema(sub).Schema == current.Schema }) {
			break
		}
		holder, current = parentAt, parent
	}
	return current
}

// writesAnyOf reports whether schema writes one of keys.
func writesAnyOf(schema *statement.Schema, keys []string) bool {
	return slices.ContainsFunc(keys, func(name string) bool {
		_, written := schema.Key(name)
		return written
	})
}

// sameNotes reports whether a and b write each of keys alike: neither, or
// both, as one value (sameWritten).
func sameNotes(a, b *statement.Schema, keys []string) bool {
	return !slices.ContainsFunc(keys, func(name string) bool {
		aKey, aWrites := a.Key(name)
		bKey, bWrites := b.Key(name)
		return aWrites != bWrites || aWrites && !sameWritten(aKey.Node, bKey.Node)
	})
}

// sameWritten reports whether two nodes write one value: each converts, and
// the two encode alike. One that does not convert writes none to compare.
func sameWritten(a, b *yaml.Node) bool {
	aValue, aErr := value.FromNode(a)
	bValue, bErr := value.FromNode(b)
	if aErr != nil || bErr != nil {
		return false
	}
	aJSON, aErr := json.Marshal(aValue, json.Deterministic(true))
	bJSON, bErr := json.Marshal(bValue, json.Deterministic(true))
	return aErr == nil && bErr == nil && bytes.Equal(aJSON, bJSON)
}
