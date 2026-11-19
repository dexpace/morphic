// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package componentlookup

import (
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
)

// MappingTarget returns where the lowering places what the mapping value of
// the discriminator read at at names, read as a $ref written there is
// (reftable.Mapping). ok is false for a value that lands on no schema.
func (s Scope) MappingTarget(at reftable.DocumentPointer, value string) (reftable.DocumentPointer, bool) {
	mapping, ok := s.Bindings.Mapping(at, value)
	if !ok || mapping.Hop.Status != reftable.Resolved {
		return reftable.DocumentPointer{}, false
	}
	return mapping.Hop.Target, true
}
