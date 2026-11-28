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
	// OpenUnions makes [TypePlan.Open] every closed union and every
	// polymorphic base, a model with a Discriminator or Abstract, leaving
	// closed only those the IR marks InputOnly
	// (docs/emitters/plan/policy.md#a-union-keeps-a-variant-it-does-not-know).
	// A model narrowing a union is not read yet: it lands with
	// TypePlan.Composed, which bounds the reading of its unions.
	OpenUnions bool `json:"openUnions"`
	// OpenEnums makes [TypePlan.Open] every closed enum that lists a member
	// and declares no FallbackMember
	// (docs/emitters/plan/policy.md#an-enum-keeps-a-value-it-does-not-list).
	OpenEnums bool `json:"openEnums"`
	// StrictExclusive makes every one_of union [TypePlan.Strict]: it refuses a
	// value two of its variants admit unless its constraints single one out.
	StrictExclusive bool `json:"strictExclusive"`
}
