// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package plan

import (
	"slices"

	"github.com/dexpace/morphic/ir"
)

// OpPlan holds what the plan derives of one operation
// (docs/emitters/plan/operations.md). It holds the fields built so far: each
// other field of that document lands with the change deriving it, so a field
// missing here is one not built, never an operation's lack of it.
type OpPlan struct {
	// Inferred maps each field a heuristic of the plan decided, by its path,
	// "Effects" so far, to the operation's provenance marked with that
	// heuristic; the SDK and the docs say the field was inferred.
	Inferred map[string]ir.Provenance `json:"inferred,omitempty"`
	// Stability is the operation's own tier, else its nearest group's that
	// states one; "" = none states one (docs/emitters/operations/stability.md).
	// The SDK documents the tier, and may have a caller acknowledge it, and
	// the docs badge it.
	Stability ir.Stability `json:"stability,omitempty"`
	// Auth is the operation's options, priority-ordered: its own, else its
	// service's unless the API sends it (docs/emitters/operations/auth.md).
	// Empty is a stated public requirement and nil none stated. The SDK's auth
	// selector and the docs read it.
	Auth []AuthOption `json:"auth,omitzero"`
	// Effects says whether calling the operation is safe and idempotent
	// (docs/emitters/plan/policy.md#an-operations-effects-come-from-its-binding).
	// The runtime's retries and the docs read it.
	Effects Effects `json:"effects,omitzero"`
}

// AuthOption is one option of an operation's OR-of-ANDs: every scheme it
// names must be satisfied together.
type AuthOption struct {
	// Schemes are the requirement's own, shared with the document, and nil
	// where it names none, so a plan decodes to what encoded it.
	Schemes []ir.SchemeUse `json:"schemes,omitempty"`
}

// operationWalk is one walk of a document's operations: the plans recorded so
// far, the groups the tier scans have read, and the policy the walk plans
// under.
type operationWalk struct {
	plans  map[ir.OperationID]OpPlan
	steps  int
	policy Policy
}

// operationPlans returns the plan of every operation [ir.ForEachOperationAt]
// reaches in doc under policy, or nil where it reaches none. A walk the bounds
// cut leaves out what it did not reach, which pass.Validate reports as
// ir/walk-truncated, so the plan reports nothing of its own.
func operationPlans(doc *ir.Document, policy Policy) map[ir.OperationID]OpPlan {
	walk := operationWalk{policy: policy}
	ir.ForEachOperationAt(doc, walk.plan)
	return walk.plans
}

// plan records the plan of operation, found at place, unless one is recorded
// at its ID already: of two operations sharing an ID the first is kept, as
// irverify reads one (ir/duplicate-operation-id).
func (w *operationWalk) plan(operation ir.Operation, place ir.OperationPlace) {
	if _, seen := w.plans[operation.ID]; seen {
		return
	}
	operationPlan := OpPlan{
		Stability: w.stability(operation, place.Groups),
		Auth:      authOptions(operation, place.Service),
	}
	effects, inferred := effectsOf(operation, w.policy)
	operationPlan.Effects = effects
	if inferred {
		marked := operation.Provenance
		marked.Inferred = inferredProtocolEffects
		operationPlan.Inferred = map[string]ir.Provenance{effectsPath: marked}
	}
	if w.plans == nil {
		w.plans = map[ir.OperationID]OpPlan{}
	}
	w.plans[operation.ID] = operationPlan
}

// stability returns operation's own tier where it states one, else that of
// the innermost of groups, those enclosing it outermost first, that states
// one, else "". A tier outside the vocabulary is taken as stated, and irverify
// reports it (ir/unknown-stability). It reads at most every group enclosing
// the operation, which the walk holds to the levels 0 through
// [ir.MaxGroupDepth].
func (w *operationWalk) stability(operation ir.Operation, groups []ir.OperationGroup) ir.Stability {
	if operation.Stability != ir.StabilityUnstated {
		return operation.Stability
	}
	for _, operationGroup := range slices.Backward(groups) {
		w.steps++
		if operationGroup.Stability != ir.StabilityUnstated {
			return operationGroup.Stability
		}
	}
	return ir.StabilityUnstated
}

// authOptions returns operation's auth options: its own where it states them,
// an empty list included, else none for an operation the API sends, which
// inherits no requirement (docs/ir/auth.md#inbound-operations), else
// service's. Each keeps nil apart from empty, and its options their order.
// Its service is the one whose groups hold it, never one extending that
// service, and Server.Auth, which a messaging target reads
// (docs/emitters/operations/client.md), is not read.
func authOptions(operation ir.Operation, service ir.Service) []AuthOption {
	switch {
	case operation.Auth != nil:
		return asOptions(operation.Auth)
	case operation.Bindings.Inbound():
		return nil
	default:
		return asOptions(service.Auth)
	}
}

// asOptions returns requirements as auth options in their order, nil for nil
// and empty for empty.
func asOptions(requirements []ir.AuthRequirement) []AuthOption {
	if requirements == nil {
		return nil
	}
	options := make([]AuthOption, len(requirements))
	for i, requirement := range requirements {
		if len(requirement.Schemes) > 0 {
			options[i].Schemes = requirement.Schemes
		}
	}
	return options
}
