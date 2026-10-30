// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	yaml "go.yaml.in/yaml/v3"
)

// rootNoder is a walked model's view of the mapping it was built from.
type rootNoder interface{ GetRootNode() *yaml.Node }

// referenceWrapper is a model that may be written as a $ref, whose root node is
// then the target's, read where the target is written. A schema is one too.
type referenceWrapper interface {
	rootNoder
	IsReference() bool
}

// ownRoot is the mapping a $ref is written on, which the reference's core holds:
// the model's own GetRootNode answers with the target's. Anything with no core
// holding one has none.
func ownRoot(w any) *yaml.Node {
	holder, ok := w.(interface{ GetCoreAny() any })
	if !ok {
		return nil
	}
	core, ok := holder.GetCoreAny().(rootNoder)
	if !ok {
		return nil
	}
	return core.GetRootNode()
}
