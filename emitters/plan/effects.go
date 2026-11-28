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
// decided them. A declared Idempotency decides, unmarked even where a compiler
// inferred it, its operation's own provenance naming the heuristic (GitHub
// #2314). Where it declares none, ProtocolEffects takes what every binding
// defines, reading an HTTP method in methodEffects, marked whatever it gives,
// and with it off the operation is neither.
func effectsOf(operation ir.Operation, policy Policy, methodEffects map[string]Effects) (effects Effects, inferred bool) {
	if declared, ok := declaredEffects(operation.Idempotency.Kind); ok {
		return declared, false
	}
	if !policy.ProtocolEffects {
		return Effects{}, false
	}
	return bindingEffects(operation.Bindings, methodEffects), true
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
// states, and no more, an HTTP binding stating methodEffects' entry for its
// method and one it holds no entry for neither. An RPC, message or OTP binding
// defines neither, as does an operation with no binding at all.
func bindingEffects(bindings ir.OperationBindings, methodEffects map[string]Effects) Effects {
	if bindings.RPC != nil || bindings.Message != nil || bindings.OTP != nil {
		return Effects{}
	}
	if len(bindings.HTTP) == 0 && bindings.GraphQL == nil {
		return Effects{}
	}
	stated := Effects{Safe: true, Idempotent: true}
	for _, binding := range bindings.HTTP {
		stated = stated.and(methodEffects[binding.Method])
	}
	if bindings.GraphQL != nil {
		stated = stated.and(graphQLEffects(bindings.GraphQL.Kind))
	}
	return stated
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

// methodRegistry returns what the IANA HTTP Method Registry, updated
// 2026-06-17 (https://www.iana.org/assignments/http-methods), records for each
// method it lists, keyed by the method's name as the registry writes it, since
// HTTP compares a method with case (RFC 9110 §9.1). It holds every row and no
// other method, so a method it holds no entry for is one the registry does not
// list.
func methodRegistry() map[string]Effects {
	safe, idempotent, neither := Effects{Safe: true, Idempotent: true}, Effects{Idempotent: true}, Effects{}
	return map[string]Effects{
		"GET": safe, "HEAD": safe, "OPTIONS": safe, "PRI": safe, "PROPFIND": safe, "QUERY": safe,
		"REPORT": safe, "SEARCH": safe, "TRACE": safe,

		"ACL": idempotent, "BASELINE-CONTROL": idempotent, "BIND": idempotent, "CHECKIN": idempotent,
		"CHECKOUT": idempotent, "COPY": idempotent, "DELETE": idempotent, "LABEL": idempotent,
		"LINK": idempotent, "MERGE": idempotent, "MKACTIVITY": idempotent, "MKCALENDAR": idempotent,
		"MKCOL": idempotent, "MKREDIRECTREF": idempotent, "MKWORKSPACE": idempotent, "MOVE": idempotent,
		"ORDERPATCH": idempotent, "PROPPATCH": idempotent, "PUT": idempotent, "REBIND": idempotent,
		"UNBIND": idempotent, "UNCHECKOUT": idempotent, "UNLINK": idempotent, "UNLOCK": idempotent,
		"UPDATE": idempotent, "UPDATEREDIRECTREF": idempotent, "VERSION-CONTROL": idempotent,

		"CONNECT": neither, "LOCK": neither, "PATCH": neither, "POST": neither, "*": neither,
	}
}
