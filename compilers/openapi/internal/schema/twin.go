// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

// TwinKeyword returns the keyword JSON Schema 2020-12 writes for key, a key
// s writes, where the dialect s is read in (lowering.SchemaRulesOf) spells it
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
	rules := lowering.SchemaRulesOf(c, s.GetRootNode())
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
