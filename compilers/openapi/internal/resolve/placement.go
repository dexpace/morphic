// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package resolve

import (
	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
)

// Target returns where the lowering places what the $ref at js names, as the
// compiler resolved it: the position its hop lands on (bind.Hop.Target), read
// through each reference its pointer passes, so a schema reached through an
// alias is placed where it is declared. ok is false for a $ref that lands on
// no schema, and one on a loop of $refs, which lands nowhere a type can be
// read.
func (s Scope) Target(js *oas3.JSONSchema[oas3.Referenceable]) (bind.Position, bool) {
	e, ok := s.Bindings.Entry(js)
	if !ok || e.Hop.Status != bind.Resolved || e.Chain.Member {
		return bind.Position{}, false
	}
	return e.Hop.Target, true
}

// MappingTarget returns where the lowering places what the mapping value of
// the discriminator d names, read as a $ref written at d is (bind.Mapping),
// and the schema written there. ok is false for a value that lands on no
// schema.
func (s Scope) MappingTarget(d *oas3.Discriminator, value string,
) (bind.Position, *oas3.JSONSchema[oas3.Referenceable], bool) {
	m, ok := s.Bindings.Mapping(d, value)
	if !ok || m.Hop.Status != bind.Resolved {
		return bind.Position{}, nil, false
	}
	return m.Hop.Target, s.Bindings.At(m.Hop.Target), true
}
