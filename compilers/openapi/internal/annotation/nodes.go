// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// keyNode returns the value of the keyword key s writes, its alias followed,
// as s.Key reads it: nil where s does not write it, or writes a null for a
// keyword whose value is no instance (docs/compilers/openapi.md#null-keywords).
func keyNode(s *statement.Schema, key string) *yaml.Node {
	k, read := s.Key(key)
	if !read {
		return nil
	}
	return k.Node
}
