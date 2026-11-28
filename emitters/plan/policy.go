// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package plan

// Policy holds the plan's injectable heuristics
// (docs/emitters/plan/policy.md). Every field is data from a closed
// vocabulary, never a function, so a policy encodes, compares and hashes as
// what it says, and [Plan.Version] hashes it.
//
// The zero Policy turns every switch off. It holds the switches [Build] reads
// so far: each other one lands with the plan field that reads it.
type Policy struct {
	// StrictExclusive makes every one_of union [TypePlan.Strict]: it refuses a
	// value two of its variants admit unless its constraints single one out.
	StrictExclusive bool `json:"strictExclusive"`
	// ProtocolEffects gives an operation that declares no Idempotency the
	// effects every one of its bindings defines, marked Inferred
	// (docs/emitters/plan/policy.md#an-operations-effects-come-from-its-binding).
	ProtocolEffects bool `json:"protocolEffects"`
}
