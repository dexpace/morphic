// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import "github.com/dexpace/morphic/ir"

// KindInferenceHeuristic is the name a type node's Provenance.Inferred carries
// where the kind-inference policy read its kind off the shape keywords its
// schema writes. It is a constant for the reason StreamingInferenceHeuristic
// is: an auditor greps for it.
const KindInferenceHeuristic ir.Inferred = "kind-inference"

// KindInference is the kind-inference policy: whether a schema naming no kind
// lowers as one writing the type its shape keywords speak of
// (docs/compilers/openapi.md#shape-applicator).
//
// It is a policy because the reading guesses at intent (architecture principle
// 6): JSON Schema applies such a keyword to instances of its kind alone and
// passes every other, so `{properties: …}` admits a string, yet a document
// writing one means an object.
type KindInference struct {
	// Disabled turns the reading off: such a schema is then the top type, the
	// keywords that cannot hold it kept verbatim.
	Disabled bool `json:"disabled,omitzero"`
}

// Honours reports whether the policy is on, so that a schema's shape keywords
// name the kind it lowers to where it names none.
func (p KindInference) Honours() bool { return !p.Disabled }
