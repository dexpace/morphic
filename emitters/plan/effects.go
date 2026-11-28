// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package plan

import "github.com/dexpace/morphic/ir"

// Effects says what calling an operation may change
// (docs/emitters/plan/policy.md#an-operations-effects-come-from-its-binding).
type Effects struct {
	// Safe reports that the operation asks for no change on the server (RFC
	// 9110 §9.2.1).
	Safe bool `json:"safe,omitzero"`
	// Idempotent reports that a second call changes nothing the first did not
	// (RFC 9110 §9.2.2); it is true of every safe operation.
	Idempotent bool `json:"idempotent,omitzero"`
}

const (
	// effectsPath is the path [OpPlan.Inferred] marks Effects at.
	effectsPath = "Effects"
	// inferredProtocolEffects names [Policy.ProtocolEffects] in the mark it
	// sets.
	inferredProtocolEffects ir.Inferred = "protocol-effects"
)

// effectsOf returns operation's effects under policy, and whether a heuristic
// decided them. A declared Idempotency decides, unmarked. Where it declares
// none, ProtocolEffects takes what every binding defines, marked whatever it
// gives, and with it off the operation is neither.
func effectsOf(operation ir.Operation, policy Policy) (effects Effects, inferred bool) {
	if declared, ok := declaredEffects(operation.Idempotency.Kind); ok {
		return declared, false
	}
	if !policy.ProtocolEffects {
		return Effects{}, false
	}
	return bindingEffects(operation.Bindings), true
}

// declaredEffects returns the effects kind declares, and false for a kind
// declaring none. A token kind is idempotent, since a retry sends the token
// it first sent. A kind outside the vocabulary is a declaration the plan
// cannot read, which irverify reports (ir/unknown-idempotency-kind): it
// decides neither, as precedence has a declared fact win over a heuristic.
func declaredEffects(kind ir.IdempotencyKind) (Effects, bool) {
	switch kind {
	case ir.IdempotencyUnknown:
		return Effects{}, false
	case ir.IdempotencySafe:
		return Effects{Safe: true, Idempotent: true}, true
	case ir.IdempotencyIdempotent, ir.IdempotencyToken:
		return Effects{Idempotent: true}, true
	default:
		return Effects{}, true
	}
}

// bindingEffects returns what every one of bindings defines: the effects each
// states, and no more. An RPC, message or OTP binding defines neither, as
// does an operation with no binding at all.
func bindingEffects(bindings ir.OperationBindings) Effects {
	if bindings.RPC != nil || bindings.Message != nil || bindings.OTP != nil {
		return Effects{}
	}
	if len(bindings.HTTP) == 0 && bindings.GraphQL == nil {
		return Effects{}
	}
	met := Effects{Safe: true, Idempotent: true}
	for _, binding := range bindings.HTTP {
		met = met.and(methodEffects(binding.Method))
	}
	if bindings.GraphQL != nil {
		met = met.and(graphQLEffects(bindings.GraphQL.Kind))
	}
	return met
}

// and returns what e and other both state.
func (e Effects) and(other Effects) Effects {
	return Effects{Safe: e.Safe && other.Safe, Idempotent: e.Idempotent && other.Idempotent}
}

// graphQLEffects returns what a GraphQL operation of kind defines: a query is
// a read-only fetch (GraphQL §2.3), and a mutation or a subscription neither.
func graphQLEffects(kind string) Effects {
	if kind == "query" {
		return Effects{Safe: true, Idempotent: true}
	}
	return Effects{}
}

// methodEffects returns what the IANA HTTP Method Registry, updated
// 2026-06-17 (https://www.iana.org/assignments/http-methods), records for
// method, compared with case as HTTP compares a method (RFC 9110 §9.1). Each
// of its rows has an arm, and a method it does not list is neither.
func methodEffects(method string) Effects {
	switch method {
	case "GET", "HEAD", "OPTIONS", "PRI", "PROPFIND", "QUERY", "REPORT", "SEARCH", "TRACE":
		return Effects{Safe: true, Idempotent: true}
	case "ACL", "BASELINE-CONTROL", "BIND", "CHECKIN", "CHECKOUT", "COPY", "DELETE", "LABEL", "LINK",
		"MERGE", "MKACTIVITY", "MKCALENDAR", "MKCOL", "MKREDIRECTREF", "MKWORKSPACE", "MOVE",
		"ORDERPATCH", "PROPPATCH", "PUT", "REBIND", "UNBIND", "UNCHECKOUT", "UNLINK", "UNLOCK", "UPDATE",
		"UPDATEREDIRECTREF", "VERSION-CONTROL":
		return Effects{Idempotent: true}
	case "CONNECT", "LOCK", "PATCH", "POST", "*":
		return Effects{}
	default:
		return Effects{}
	}
}
