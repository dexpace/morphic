// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import "github.com/dexpace/morphic/ir"

// PaginationInferenceHeuristic is the name an operation's Provenance.Inferred
// carries where the pagination-inference policy filled its Pagination. It is a
// constant for the reason StreamingMediaTypeHeuristic is: an auditor greps for
// it.
const PaginationInferenceHeuristic ir.Inferred = "pagination-inference"

// PaginationInference is the pagination-inference policy: whether an
// operation's pagination is read off the names of its query parameters and of
// the members of its success responses (docs/compilers/openapi.md#pagination-policy).
//
// It is a policy because the reading guesses at intent (architecture principle
// 6): OpenAPI has no keyword for pagination, so a paginated operation says so
// only by naming a parameter `page` or `cursor` and a member `result`, which
// promises nothing about how the API walks its pages.
type PaginationInference struct {
	// Disabled turns the reading off: no operation then carries a Pagination
	// the document does not declare.
	Disabled bool `json:"disabled,omitzero"`
}

// Honours reports whether the policy is on, so that an operation's pagination
// is read off the names it uses.
func (p PaginationInference) Honours() bool { return !p.Disabled }
