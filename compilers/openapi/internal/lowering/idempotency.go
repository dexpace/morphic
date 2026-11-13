// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import "github.com/dexpace/morphic/ir"

// IdempotencyInferenceHeuristic is the name an Operation's
// Provenance.Inferred carries where the idempotency policy read its
// Idempotency off a header parameter. It is a constant for the reason
// StreamingMediaTypeHeuristic is: an auditor greps for it.
const IdempotencyInferenceHeuristic ir.Inferred = "idempotency-inference"

// IdempotencyInference is the idempotency policy: whether an operation
// declaring an `Idempotency-Key` header parameter, and no idempotency, takes
// that parameter as its idempotency token.
//
// It is a policy because the reading guesses at intent (architecture principle
// 6): OpenAPI has no field for a token, and a header's name is all that says
// the server reads one.
type IdempotencyInference struct {
	// Disabled turns the reading off: the header is then a parameter like any
	// other, and the operation's idempotency what the document declares.
	Disabled bool `json:"disabled,omitzero"`
}

// Honours reports whether the policy is on, so that the header names the
// operation's idempotency token.
func (p IdempotencyInference) Honours() bool { return !p.Disabled }
