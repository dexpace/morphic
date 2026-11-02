// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	"slices"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/yml"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// RawPropertyNode returns the raw YAML value node of a top-level schema
// keyword, its alias followed, or nil when absent. The library's
// GetPropertyNode resolves Go core field names and returns key nodes; this
// reads the schema's root mapping for the on-wire keyword as the parser reads
// it (nodeview.Child), through its `<<` merges, and returns its value node,
// which is where exact literals live.
//
// A null reads as absent, since no keyword admits one, unless the keyword's
// value is an instance (holdsInstance), so every reader sees one keyword left
// out (docs/compilers/openapi.md#null-keywords).
func RawPropertyNode(s *oas3.Schema, key string) *yaml.Node {
	if s == nil {
		return nil
	}
	n := nodeview.Child(s.GetRootNode(), key)
	if IsNullNode(n) && !holdsInstance(key) {
		return nil
	}
	return n
}

// holdsInstance reports whether keyword's value is a JSON instance rather than
// a value of the keyword's own type, so that a null is that value.
func holdsInstance(keyword string) bool {
	return keyword == "const" || keyword == "default" || keyword == "example"
}

// DeclaresAny reports whether s writes any of keywords. It reads the raw nodes
// rather than the model fields because the recorders reading the same keyword
// lists read them there (recordResidue, dialectAt, declaredConstraints), and a
// predicate consulting a different source could disagree with them in either
// direction.
func DeclaresAny(s *oas3.Schema, keywords []string) bool {
	return slices.ContainsFunc(keywords, func(keyword string) bool { return RawPropertyNode(s, keyword) != nil })
}

// IsNullNode reports whether n, read through an alias, is a YAML null.
func IsNullNode(n *yaml.Node) bool {
	n = yml.ResolveAlias(n)
	return n != nil && n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null"
}
