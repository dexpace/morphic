// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// TwinKeyword returns the keyword JSON Schema 2020-12 writes for key, a key
// schema writes, where the dialect schema is read in (rulesOf) spells it
// otherwise: draft 4's identifier `id` as `$id`, a draft's `definitions` as
// `$defs`, its `dependencies`, which 2019-09 split in two, as
// `dependentSchemas`, and 2019-09's `$recursiveAnchor` as `$dynamicAnchor`. It
// is key itself otherwise. A reader of a list of 2020-12's keywords asks it, so
// a draft's spelling is read as its twin is.
func TwinKeyword(loweringContext lowering.Ctx, schema *statement.Schema, key string) string {
	switch key {
	case "id", "definitions", "dependencies", "$recursiveAnchor":
	default:
		return key
	}
	rules := rulesOf(loweringContext, schema)
	switch {
	case key == "id" && rules.Identifier() == "id":
		return "$id"
	case key == "definitions" && rules.Takes("definitions", specrules.Map):
		return "$defs"
	case key == "dependencies" && rules.Defines("dependencies"):
		return "dependentSchemas"
	case key == "$recursiveAnchor" && rules.Defines("$recursiveAnchor"):
		return "$dynamicAnchor"
	default:
		return key
	}
}

// rulesOf returns the rows schema is read with, for a reading that has schema
// and not where it is: those of where the bindings place it (placedAt), so a
// node an alias places under two roots reads at each the dialect of that place,
// and those of its node where they place it nowhere
// (dialect.SchemaDialectRows.Of).
func rulesOf(loweringContext lowering.Ctx, schema *statement.Schema) specrules.SchemaRules {
	if there, at, placed := placedAt(loweringContext, schema); placed {
		return there.Rules().At(there.Position(at))
	}
	return loweringContext.Rules().Of(schema.Node)
}

// placedAt returns where the bindings place schema (statement.Schema.At), in
// the context of its document, whichever loweringContext reads, and false where
// they place it nowhere or in a document the compile did not read.
func placedAt(loweringContext lowering.Ctx, schema *statement.Schema) (lowering.Ctx, jsontext.Pointer, bool) {
	if at, placed := siteOf(schema); placed {
		if there, read := lowering.InDocument(loweringContext, at.Doc); read {
			return there, at.Pointer, true
		}
	}
	return loweringContext, "", false
}
