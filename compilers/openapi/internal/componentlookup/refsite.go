// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package componentlookup

import (
	"encoding/json/jsontext"

	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/ir"
)

// ComponentRef classifies an internal pointer against the document's component
// schemas (ids.ComponentState), and returns a NamedComponent's stable named ID:
// one the document declares, where a name it does not is an
// UndeclaredComponent, a dangling reference to drop. The ID is rebuilt from the
// component's canonical name, unescaped then re-escaped by ids.Ptr, not from the
// incoming pointer text, so a non-canonically escaped reference (`A~B` for a
// component named "A~B", interned under `A~0B`) still resolves to the interned
// node.
func (s Scope) ComponentRef(pointer jsontext.Pointer) (ir.TypeID, ids.ComponentState) {
	name, state := ids.ComponentSchema(pointer)
	switch {
	case state != ids.NamedComponent:
		return "", state
	case s.Declares.Has(name):
		return ids.DocumentIDNamespace{}.NamedType(ids.Ptr("components", "schemas", name)), state
	default:
		return "", ids.UndeclaredComponent
	}
}
