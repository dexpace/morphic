// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package resolve

import (
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
)

// MappingTarget returns where the lowering places what the mapping value of
// the discriminator read at at names, read as a $ref written there is
// (bind.Mapping). ok is false for a value that lands on no schema.
func (s Scope) MappingTarget(at bind.Position, value string) (bind.Position, bool) {
	m, ok := s.Bindings.Mapping(at, value)
	if !ok || m.Hop.Status != bind.Resolved {
		return bind.Position{}, false
	}
	return m.Hop.Target, true
}
