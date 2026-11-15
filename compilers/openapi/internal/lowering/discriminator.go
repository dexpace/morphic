// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import "github.com/dexpace/morphic/ir"

// DiscriminatorInferenceHeuristic is the name a Union's Provenance.Inferred
// carries where the discriminator-inference policy filled its Discriminator,
// and a subtype's where it read its DiscriminatorValue. It is a constant for
// the reason StreamingInferenceHeuristic is: an auditor greps for it.
const DiscriminatorInferenceHeuristic ir.Inferred = "discriminator-inference"

// DiscriminatorInference is the discriminator-inference policy: whether a
// union's tags are read off the strings each variant pins its tag property
// to, where the document declares no mapping or no discriminator, and a
// subtype's off the strings it admits, where it is routed by name.
//
// It is a policy because the reading guesses at intent (architecture principle
// 6): OpenAPI routes an unmapped discriminator by component name, which a
// variant or subtype pinning other values rejects, and an undeclared union
// names its variant only by validation, though a required property its
// variants pin apart tells them apart as a tag would.
type DiscriminatorInference struct {
	// Disabled turns the reading off: an unmapped discriminator then routes by
	// component name, a subtype it routes by name keeping that name as its tag,
	// and an undeclared union stays untagged, as the document says.
	Disabled bool `json:"disabled,omitzero"`
}

// Honours reports whether the policy is on, so that a union's tag is read off
// the values its variants pin, and a subtype's off those it admits.
func (p DiscriminatorInference) Honours() bool { return !p.Disabled }
