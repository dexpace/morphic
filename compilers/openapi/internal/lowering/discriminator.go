// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import "github.com/dexpace/morphic/ir"

// DiscriminatorInferenceHeuristic is the name a Union's Provenance.Inferred
// carries where the discriminator-inference policy filled its Discriminator. It
// is a constant for the reason StreamingMediaTypeHeuristic is: an auditor greps
// for it.
const DiscriminatorInferenceHeuristic ir.Inferred = "discriminator-inference"

// DiscriminatorInference is the discriminator-inference policy: whether a
// union's tag is read off the value each variant pins its tag property to,
// where the document declares no mapping for it, or no discriminator at all.
//
// It is a policy because the reading guesses at intent (architecture principle
// 6): OpenAPI routes an unmapped discriminator by component name, a value a
// variant pinning another one rejects, and an undeclared union names its
// variant only by validation, though a required property pinned to a distinct
// value by each variant tells them apart as a declared tag would.
type DiscriminatorInference struct {
	// Disabled turns the reading off: an unmapped discriminator then routes by
	// component name and an undeclared union stays untagged, as the document
	// says.
	Disabled bool `json:"disabled,omitzero"`
}

// Honours reports whether the policy is on, so that a union's tag is read off
// the values its variants pin.
func (p DiscriminatorInference) Honours() bool { return !p.Disabled }
