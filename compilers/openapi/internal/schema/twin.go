// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

// TwinKeyword returns the keyword JSON Schema 2020-12 writes for key, a key
// s writes, where the dialect s is read in (rulesOf) spells it
// otherwise: draft 4's identifier `id` as `$id`, a draft's `definitions` as
// `$defs`, its `dependencies`, which 2019-09 split in two, as
// `dependentSchemas`, and 2019-09's `$recursiveAnchor` as `$dynamicAnchor`. It
// is key itself otherwise. A reader of a list of 2020-12's keywords asks it, so
// a draft's spelling is read as its twin is.
func TwinKeyword(c lowering.Ctx, s *oas3.Schema, key string) string {
	switch key {
	case "id", "definitions", "dependencies", "$recursiveAnchor":
	default:
		return key
	}
	rules := rulesOf(c, s)
	switch {
	case key == "id" && rules.Identifier() == "id":
		return "$id"
	case key == "definitions" && rules.Takes("definitions", oas.Map):
		return "$defs"
	case key == "dependencies" && rules.Defines("dependencies"):
		return "dependentSchemas"
	case key == "$recursiveAnchor" && rules.Defines("$recursiveAnchor"):
		return "$dynamicAnchor"
	default:
		return key
	}
}

// rulesOf returns the rows s is read with, for a reading that has s and not
// where it is: those of where the bindings place it (placedAt), so a node an
// alias places under two roots reads at each the dialect of that place, and
// those of its node where they place it nowhere (lowering.SchemaRulesOf).
func rulesOf(c lowering.Ctx, s *oas3.Schema) oas.SchemaRules {
	if there, at, placed := placedAt(c, s); placed {
		return lowering.SchemaRulesAt(there, at)
	}
	return lowering.SchemaRulesOf(c, s.GetRootNode())
}

// placedAt returns where the bindings place s (typed.Bindings.SiteOf), in the
// context of its document, whichever c reads, and false where they place it
// nowhere or in a document the compile did not read.
func placedAt(c lowering.Ctx, s *oas3.Schema) (lowering.Ctx, jsontext.Pointer, bool) {
	if at, placed := c.Bindings().SiteOf(s); placed {
		if there, read := lowering.InDocument(c, at.Doc); read {
			return there, at.Pointer, true
		}
	}
	return c, "", false
}
