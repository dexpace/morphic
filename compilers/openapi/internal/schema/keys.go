// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// hasKey reports whether schema writes key and it is read: a null is the keyword
// left out unless the keyword holds an instance (statement.Schema.Key).
func hasKey(schema *statement.Schema, key string) bool {
	_, read := schema.Key(key)
	return read
}

// hasAnyKey reports whether schema writes any of keys (hasKey). It reads the keys
// as written rather than the model's fields because the recorders reading the
// same keyword lists read them there (recordResidue, declaredConstraints and
// annotation's dialectAt), and a predicate consulting a different source could
// disagree with them in either direction.
func hasAnyKey(schema *statement.Schema, keys []string) bool {
	return slices.ContainsFunc(keys, func(key string) bool { return hasKey(schema, key) })
}

// keyNode returns the value schema writes for key, its alias followed, nil where
// it writes none that is read (hasKey).
func keyNode(schema *statement.Schema, key string) *yaml.Node {
	statementKey, read := schema.Key(key)
	if !read {
		return nil
	}
	return statementKey.Node
}
