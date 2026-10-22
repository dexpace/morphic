package annotation

import (
	"slices"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "gopkg.in/yaml.v3"
)

// RawPropertyNode returns the raw YAML value node of a top-level schema
// keyword, or nil when absent. The library's GetPropertyNode resolves Go core
// field names and returns key nodes; this scans the schema's root mapping for
// the on-wire keyword and returns its value node, which is where exact literals
// live.
func RawPropertyNode(s *oas3.Schema, key string) *yaml.Node {
	if s == nil {
		return nil
	}
	return RawChildNode(s.GetRootNode(), key)
}

// RawChildNode returns the raw YAML value node of a mapping child keyed by the
// on-wire name, unwrapping a document node first; nil when absent. It reads
// exact literals the high-level model does not preserve (links, servers,
// content maps).
//
// The last pair spelling the key wins, as for the parser, which skips every
// earlier occurrence (docs/parser-library.md#duplicate-key). An explicit pair
// and an aliased one are one key to the parser and two nodes here, so the first
// would describe a mapping by a value nothing else in the compiler uses.
func RawChildNode(root *yaml.Node, key string) *yaml.Node {
	if root == nil {
		return nil
	}
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil
	}
	var found *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if keyName(root.Content[i]) == key {
			found = root.Content[i+1]
		}
	}
	return found
}

// RawMappingKeys returns the on-wire names a raw mapping writes, each once, in
// first-written order, unwrapping a document node first; nil for a node that is
// not a mapping or writes no key. Each name resolves through RawChildNode.
//
// It exists for a Path Item Object, whose unmarshaller skips a key with an
// anchored value (GitHub #412). Anchors are cleared first (GitHub #459, #501,
// #538), so it is needed only past #538's hop bound.
//
// A `<<` merge key is left out and its merged pairs are not read (GitHub #395).
// A repeated key is returned once, so a census does not report a collision the
// document lacks.
func RawMappingKeys(root *yaml.Node) []string {
	if root == nil {
		return nil
	}
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil
	}
	var keys []string
	seen := make(map[string]bool, len(root.Content)/2)
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i]
		if isMergeKey(k) {
			continue
		}
		name := keyName(k)
		if seen[name] {
			continue
		}
		seen[name] = true
		keys = append(keys, name)
	}
	return keys
}

// keyName is the on-wire name a mapping key node spells, following an alias to
// the scalar it stands for.
//
// yaml.v3 leaves an alias node's own Value as the anchor name, so a key written
// as an alias matches nothing when read raw — while the parser reads that key
// under the name it resolves to, which is the name every caller here looks up.
// Comparing the two spellings is what let a key the model reported as
// undeclared reach no Unmodeled entry at all (GitHub #297).
func keyName(n *yaml.Node) string {
	if n.Kind == yaml.AliasNode && n.Alias != nil {
		return n.Alias.Value
	}
	return n.Value
}

// RawChildNodes is RawChildNode for every key of a mapping at once: each name
// the mapping writes, to the node RawChildNode returns for it. A caller looking
// up many keys of one large mapping reads its pairs once rather than once per
// key. It is nil for a missing node and for one that is not a mapping.
func RawChildNodes(root *yaml.Node) map[string]*yaml.Node {
	if root == nil {
		return nil
	}
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil
	}
	children := make(map[string]*yaml.Node, len(root.Content)/2)
	for i := 0; i+1 < len(root.Content); i += 2 {
		children[keyName(root.Content[i])] = root.Content[i+1]
	}
	return children
}

// DeclaresAny reports whether s writes any of keywords. It reads the raw nodes
// rather than the model fields because the recorders reading the same keyword
// lists read them there (recordResidue, dialectAt, declaredConstraints), and a
// predicate consulting a different source could disagree with them in either
// direction.
func DeclaresAny(s *oas3.Schema, keywords []string) bool {
	return slices.ContainsFunc(keywords, func(keyword string) bool { return RawPropertyNode(s, keyword) != nil })
}
