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
func RawPropertyNode(s *oas3.Schema, key string) *yaml.Node {
	if s == nil {
		return nil
	}
	return nodeview.Child(s.GetRootNode(), key)
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
