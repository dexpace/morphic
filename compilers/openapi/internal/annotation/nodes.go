// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// keyNode returns the value of the keyword key schema writes, its alias
// followed, as schema.Key reads it: nil where schema does not write it, or
// writes a null for a keyword whose value is no instance
// (docs/compilers/openapi.md#null-keywords).
func keyNode(schema *statement.Schema, key string) *yaml.Node {
	statementKey, read := schema.Key(key)
	if !read {
		return nil
	}
	return statementKey.Node
}
