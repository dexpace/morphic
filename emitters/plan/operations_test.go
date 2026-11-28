// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package plan_test

import (
	"encoding/csv"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"maps"
	"os"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/emitters/plan"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// operation declares the operation id, shaped by mutate when it is not nil,
// at a provenance of its own.
func operation(id ir.OperationID, mutate func(*ir.Operation)) ir.Operation {
	declared := ir.Operation{ID: id, Provenance: ir.Provenance{Pointer: jsontext.Pointer("/operations").AppendToken(string(id))}}
	if mutate != nil {
		mutate(&declared)
	}
	return declared
}

// over binds an operation to HTTP under each of methods, in order.
func over(methods ...string) func(*ir.Operation) {
	return func(o *ir.Operation) {
		for _, method := range methods {
			o.Bindings.HTTP = append(o.Bindings.HTTP, ir.HTTPBinding{Method: method, URITemplate: "/x"})
		}
	}
}

// group declares a group of id stating tier, holding operations and then
// nested.
func group(id ir.GroupID, tier ir.Stability, operations []ir.Operation, nested ...ir.OperationGroup) ir.OperationGroup {
	return ir.OperationGroup{ID: id, Stability: tier, Operations: operations, Groups: nested}
}

// documentOf returns a document of services.
func documentOf(services ...ir.Service) *ir.Document {
	return &ir.Document{Services: services}
}

// operationIDs returns the IDs ir.ForEachOperation visits in doc, sorted.
func operationIDs(doc *ir.Document) []ir.OperationID {
	var ids []ir.OperationID
	ir.ForEachOperation(doc, func(o ir.Operation) { ids = append(ids, o.ID) })
	slices.Sort(ids)
	return ids
}

// TestBuild_PlansEveryOperation holds Ops to one entry for each operation the
// walk visits, across services and at every depth, and to nil where there is
// none.
func TestBuild_PlansEveryOperation(t *testing.T) {
	t.Parallel()
	doc := documentOf(
		ir.Service{ID: "service/one", Groups: []ir.OperationGroup{
			group("group/a", "", []ir.Operation{operation("operation/a", nil)},
				group("group/a/1", "", []ir.Operation{operation("operation/a/1", nil)},
					group("group/a/1/1", "", []ir.Operation{operation("operation/a/1/1", nil)}))),
			group("group/b", "", []ir.Operation{operation("operation/b", nil)}),
		}},
		ir.Service{ID: "service/two", Groups: []ir.OperationGroup{
			group("group/c", "", []ir.Operation{operation("operation/c", nil), operation("operation/d", nil)}),
		}},
	)

	built := build(t, doc, plan.Policy{})

	want := operationIDs(doc)
	require.Len(t, want, 6, "the fixture's operations")
	assert.Equal(t, want, slices.Sorted(maps.Keys(built.Ops)))

	empty := build(t, documentOf(ir.Service{ID: "service/none", Groups: []ir.OperationGroup{group("group/empty", "", nil)}}), plan.Policy{})
	assert.Nil(t, empty.Ops, "a document with no operation plans none")
}

// TestBuild_KeepsTheFirstOperationOfAnID holds Ops, of two operations sharing
// an ID, to the first, as irverify reads one: the second, declared public,
// would replace the first's own requirement.
func TestBuild_KeepsTheFirstOperationOfAnID(t *testing.T) {
	t.Parallel()
	key := []ir.AuthRequirement{{Schemes: []ir.SchemeUse{{Scheme: "auth/key"}}}}
	doc := documentOf(ir.Service{ID: "service/one", Groups: []ir.OperationGroup{group("group/a", "", []ir.Operation{
		operation("operation/twice", func(o *ir.Operation) { o.Auth = key }),
		operation("operation/twice", func(o *ir.Operation) { o.Auth = []ir.AuthRequirement{} }),
	})}})

	built := build(t, doc, plan.Policy{})

	require.Len(t, built.Ops, 1)
	want := []plan.AuthOption{{Schemes: key[0].Schemes}}
	assert.Empty(t, cmp.Diff(want, built.Ops["operation/twice"].Auth))
}

// TestBuild_ResolvesAnOperationsTier holds Stability to the operation's own
// tier, else the innermost group's that states one, else none (T-44). A group
// declared after a nested one, and a second service, each take only what
// encloses them.
func TestBuild_ResolvesAnOperationsTier(t *testing.T) {
	t.Parallel()
	own := func(tier ir.Stability) func(*ir.Operation) { return func(o *ir.Operation) { o.Stability = tier } }
	doc := documentOf(
		ir.Service{ID: "service/one", Groups: []ir.OperationGroup{
			group("group/outer", ir.StabilityBeta, []ir.Operation{
				operation("operation/own-over-group", own(ir.StabilityAlpha)),
				operation("operation/group-only", nil),
			},
				group("group/outer/states-alpha", ir.StabilityAlpha, []ir.Operation{operation("operation/inner-tier", nil)}),
				group("group/outer/states-none", "", []ir.Operation{
					operation("operation/outer-tier", nil),
					operation("operation/own-stable", own(ir.StabilityStable)),
				}),
			),
			group("group/none", "", []ir.Operation{operation("operation/no-tier", nil)}),
		}},
		ir.Service{ID: "service/two", Groups: []ir.OperationGroup{
			group("group/two", "", []ir.Operation{operation("operation/second-service", nil)}),
		}},
	)

	built := build(t, doc, plan.Policy{})

	want := map[ir.OperationID]ir.Stability{
		"operation/own-over-group": ir.StabilityAlpha,
		"operation/group-only":     ir.StabilityBeta,
		"operation/inner-tier":     ir.StabilityAlpha,
		"operation/outer-tier":     ir.StabilityBeta,
		"operation/own-stable":     ir.StabilityStable,
		"operation/no-tier":        ir.StabilityUnstated,
		"operation/second-service": ir.StabilityUnstated,
	}
	got := map[ir.OperationID]ir.Stability{}
	for id, operationPlan := range built.Ops {
		got[id] = operationPlan.Stability
	}
	assert.Equal(t, want, got)
}

// authDoc returns a document whose operations take their auth every way an
// operation can: their own, stated public, inherited, and not inherited by
// one the API sends. The service's default names two schemes in one option,
// so a write through the plan into either list moves the document.
func authDoc() *ir.Document {
	serviceDefault := []ir.AuthRequirement{{Schemes: []ir.SchemeUse{{Scheme: "auth/key"}, {Scheme: "auth/basic"}}}}
	inboundAs := func(kind ir.HTTPInbound, auth []ir.AuthRequirement) func(*ir.Operation) {
		return func(o *ir.Operation) {
			o.Bindings.HTTP = []ir.HTTPBinding{{Method: "POST", Inbound: kind}}
			o.Auth = auth
		}
	}
	return documentOf(
		ir.Service{ID: "service/secured", Auth: serviceDefault, Groups: []ir.OperationGroup{group("group/secured", "", []ir.Operation{
			operation("operation/own", func(o *ir.Operation) {
				o.Auth = []ir.AuthRequirement{
					{Schemes: []ir.SchemeUse{{Scheme: "auth/oauth", Scopes: []string{"read", "write"}}}},
					{},
					{Schemes: []ir.SchemeUse{{Scheme: "auth/key"}}},
				}
			}),
			operation("operation/public", func(o *ir.Operation) { o.Auth = []ir.AuthRequirement{} }),
			operation("operation/inherits", nil),
			operation("operation/webhook", inboundAs(ir.HTTPInboundWebhook, nil)),
			operation("operation/callback", inboundAs(ir.HTTPInboundCallback, nil)),
			operation("operation/webhook-own", inboundAs(ir.HTTPInboundWebhook, []ir.AuthRequirement{
				{Schemes: []ir.SchemeUse{{Scheme: "auth/signature"}}},
			})),
		})}},
		ir.Service{ID: "service/public", Auth: []ir.AuthRequirement{}, Groups: []ir.OperationGroup{
			group("group/public", "", []ir.Operation{operation("operation/inherits-public", nil)}),
		}},
		ir.Service{ID: "service/unstated", Groups: []ir.OperationGroup{
			group("group/unstated", "", []ir.Operation{operation("operation/inherits-nothing", nil)}),
		}},
	)
}

// TestBuild_ResolvesAnOperationsAuth holds Auth to the operation's own
// options, else its service's unless the API sends it, keeping nil, none
// stated, apart from empty, a stated public requirement, and each option, an
// empty one included, in its place.
func TestBuild_ResolvesAnOperationsAuth(t *testing.T) {
	t.Parallel()

	built := build(t, authDoc(), plan.Policy{})

	key, basic := ir.SchemeUse{Scheme: "auth/key"}, ir.SchemeUse{Scheme: "auth/basic"}
	want := map[ir.OperationID][]plan.AuthOption{
		"operation/own": {
			{Schemes: []ir.SchemeUse{{Scheme: "auth/oauth", Scopes: []string{"read", "write"}}}},
			{},
			{Schemes: []ir.SchemeUse{key}},
		},
		"operation/public":           {},
		"operation/inherits":         {{Schemes: []ir.SchemeUse{key, basic}}},
		"operation/webhook":          nil,
		"operation/callback":         nil,
		"operation/webhook-own":      {{Schemes: []ir.SchemeUse{{Scheme: "auth/signature"}}}},
		"operation/inherits-public":  {},
		"operation/inherits-nothing": nil,
	}
	got := map[ir.OperationID][]plan.AuthOption{}
	for id, operationPlan := range built.Ops {
		got[id] = operationPlan.Auth
	}
	// cmp tells a nil list from an empty one, which this test is about.
	assert.Empty(t, cmp.Diff(want, got))
}

// TestBuild_ADeclaredIdempotencyDecidesTheEffects holds a declared kind to
// deciding an operation's effects whatever its binding defines and whether or
// not ProtocolEffects is on, unmarked (T-35). A kind outside the vocabulary
// is a declaration the plan cannot read, so it decides neither.
func TestBuild_ADeclaredIdempotencyDecidesTheEffects(t *testing.T) {
	t.Parallel()
	declares := func(kind ir.IdempotencyKind, method string) func(*ir.Operation) {
		return func(o *ir.Operation) {
			o.Idempotency.Kind = kind
			over(method)(o)
		}
	}
	doc := documentOf(ir.Service{ID: "service/one", Groups: []ir.OperationGroup{group("group/a", "", []ir.Operation{
		operation("operation/safe-post", declares(ir.IdempotencySafe, "POST")),
		operation("operation/idempotent-get", declares(ir.IdempotencyIdempotent, "GET")),
		operation("operation/token-get", declares(ir.IdempotencyToken, "GET")),
		operation("operation/unknown-kind-get", declares("eventually", "GET")),
	})}})
	want := map[ir.OperationID]plan.Effects{
		"operation/safe-post":        {Safe: true, Idempotent: true},
		"operation/idempotent-get":   {Idempotent: true},
		"operation/token-get":        {Idempotent: true},
		"operation/unknown-kind-get": {},
	}

	for _, pp := range []plan.Policy{{}, {ProtocolEffects: true}} {
		built := build(t, doc, pp)

		got := map[ir.OperationID]plan.Effects{}
		for id, operationPlan := range built.Ops {
			got[id] = operationPlan.Effects
			assert.Nil(t, operationPlan.Inferred, "%s under %+v: a declared fact is not inferred", id, pp)
		}
		assert.Equal(t, want, got, "under %+v", pp)
	}
}

// effectsCase is one operation whose effects its bindings alone decide.
type effectsCase struct {
	id     ir.OperationID
	bind   func(*ir.Operation)
	effect plan.Effects
}

// bindingCases returns operations bound every way ProtocolEffects reads: each
// HTTP method kind, one in the wrong case and one the registry does not list,
// each GraphQL kind, each binding defining nothing, no binding, and bindings
// together, with the effects each takes under the policy.
func bindingCases() []effectsCase {
	both, idempotent := plan.Effects{Safe: true, Idempotent: true}, plan.Effects{Idempotent: true}
	graphQL := func(kind string) func(*ir.Operation) {
		return func(o *ir.Operation) { o.Bindings.GraphQL = &ir.GraphQLBinding{Kind: kind} }
	}
	rpc := func(o *ir.Operation) { o.Bindings.RPC = &ir.RPCBinding{FullMethod: "/pkg.Svc/Do"} }
	return []effectsCase{
		{"operation/get", over("GET"), both},
		{"operation/head", over("HEAD"), both},
		{"operation/options", over("OPTIONS"), both},
		{"operation/trace", over("TRACE"), both},
		{"operation/query", over("QUERY"), both},
		{"operation/put", over("PUT"), idempotent},
		{"operation/delete", over("DELETE"), idempotent},
		{"operation/post", over("POST"), plan.Effects{}},
		{"operation/patch", over("PATCH"), plan.Effects{}},
		{"operation/lower-case-get", over("get"), plan.Effects{}},
		{"operation/unlisted", over("PURGE"), plan.Effects{}},
		{"operation/graphql-query", graphQL("query"), both},
		{"operation/graphql-mutation", graphQL("mutation"), plan.Effects{}},
		{"operation/graphql-subscription", graphQL("subscription"), plan.Effects{}},
		{"operation/rpc", rpc, plan.Effects{}},
		{"operation/message", func(o *ir.Operation) { o.Bindings.Message = &ir.MessageBinding{} }, plan.Effects{}},
		{"operation/otp", func(o *ir.Operation) { o.Bindings.OTP = &ir.OTPBinding{} }, plan.Effects{}},
		{"operation/unbound", nil, plan.Effects{}},
		{"operation/get-and-put", over("GET", "PUT"), idempotent},
		{"operation/get-and-rpc", func(o *ir.Operation) { over("GET")(o); rpc(o) }, plan.Effects{}},
		{"operation/graphql-query-and-get", func(o *ir.Operation) { graphQL("query")(o); over("GET")(o) }, both},
		{"operation/marked-already", func(o *ir.Operation) {
			over("GET")(o)
			o.Provenance.Inferred = "group-path-prefix"
		}, both},
	}
}

// bindingDoc returns a document holding the operations of cases.
func bindingDoc(cases []effectsCase) *ir.Document {
	operations := make([]ir.Operation, 0, len(cases))
	for _, tc := range cases {
		operations = append(operations, operation(tc.id, tc.bind))
	}
	return documentOf(ir.Service{ID: "service/bound", Groups: []ir.OperationGroup{group("group/bound", "", operations)}})
}

// TestBuild_ReadsEffectsOffTheBindings holds ProtocolEffects to what every
// binding of an undeclared operation defines (T-35): RFC 9110's methods, a
// method compared with case, the registry's silence read as neither,
// GraphQL's kinds, and bindings together taking only what each states. Each
// result, neither included, is marked at the operation's provenance, the
// plan's heuristic in place of any the operation's own mark names.
func TestBuild_ReadsEffectsOffTheBindings(t *testing.T) {
	t.Parallel()
	cases := bindingCases()
	doc := bindingDoc(cases)
	require.True(t, ir.Inferred("protocol-effects").IsCanonical())

	built := build(t, doc, plan.Policy{ProtocolEffects: true})

	require.Len(t, built.Ops, len(cases))
	for _, tc := range cases {
		operationPlan := built.Ops[tc.id]
		assert.Equal(t, tc.effect, operationPlan.Effects, "%s", tc.id)

		marked := operation(tc.id, nil).Provenance
		marked.Inferred = "protocol-effects"
		assert.Equal(t, map[string]ir.Provenance{"Effects": marked}, operationPlan.Inferred, "%s", tc.id)
	}
}

// TestBuild_ProtocolEffectsOffLeavesEveryUndeclaredOperationNeither holds the
// zero policy to inferring nothing (T-35): every operation of the bindings
// test, which declares no kind, is neither, and none is marked.
func TestBuild_ProtocolEffectsOffLeavesEveryUndeclaredOperationNeither(t *testing.T) {
	t.Parallel()
	cases := bindingCases()

	built := build(t, bindingDoc(cases), plan.Policy{})

	require.Len(t, built.Ops, len(cases))
	for id, operationPlan := range built.Ops {
		assert.Equal(t, plan.Effects{}, operationPlan.Effects, "%s", id)
		assert.Nil(t, operationPlan.Inferred, "%s", id)
	}
}

// TestBuild_ReadsEveryRegisteredMethodAsTheRegistryRecordsIt holds the plan's
// copy of the IANA HTTP Method Registry to the registry's own CSV, row by row.
func TestBuild_ReadsEveryRegisteredMethodAsTheRegistryRecordsIt(t *testing.T) {
	t.Parallel()
	file, err := os.Open("testdata/http-methods.csv")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	rows, err := csv.NewReader(file).ReadAll()
	require.NoError(t, err)
	require.Equal(t, []string{"Method Name", "Safe", "Idempotent", "Reference"}, rows[0])
	rows = rows[1:]
	require.NotEmpty(t, rows, "the registry lists methods")

	yes := map[string]bool{"yes": true, "no": false}
	cases := make([]effectsCase, 0, len(rows))
	for _, row := range rows {
		safe, safeRead := yes[row[1]]
		idempotent, idempotentRead := yes[row[2]]
		require.True(t, safeRead && idempotentRead, "row %v", row)
		assert.True(t, idempotent || !safe, "%s: the registry records every safe method idempotent", row[0])
		cases = append(cases, effectsCase{
			id: ir.OperationID("operation/" + row[0]), bind: over(row[0]),
			effect: plan.Effects{Safe: safe, Idempotent: idempotent},
		})
	}

	built := build(t, bindingDoc(cases), plan.Policy{ProtocolEffects: true})

	for _, tc := range cases {
		assert.Equal(t, tc.effect, built.Ops[tc.id].Effects, "%s", tc.id)
	}
}

// TestPolicy_ProtocolEffectsMovesTheVersion holds the switch to being part of
// the policy a plan's version hashes, and to round-tripping as data (T-31).
func TestPolicy_ProtocolEffectsMovesTheVersion(t *testing.T) {
	t.Parallel()
	off, on := plan.Policy{}, plan.Policy{ProtocolEffects: true}

	assert.NotEqual(t, build(t, widgetDoc(), off).Version, build(t, widgetDoc(), on).Version)
	for _, pp := range []plan.Policy{off, on} {
		raw, err := json.Marshal(pp, ir.CanonicalOptions())
		require.NoError(t, err)
		var decoded plan.Policy
		require.NoError(t, json.Unmarshal(raw, &decoded))
		assert.Equal(t, pp, decoded, "%s", raw)
	}
}

// operationsDoc returns the auth and bindings documents' services in one
// document.
func operationsDoc() *ir.Document {
	return documentOf(append(authDoc().Services, bindingDoc(bindingCases()).Services...)...)
}

// TestBuild_LeavesADocumentsOperationsAsItFoundThem holds planning operations
// to writing nothing of the document, whose auth lists the plan shares.
func TestBuild_LeavesADocumentsOperationsAsItFoundThem(t *testing.T) {
	t.Parallel()
	doc := operationsDoc()
	before := irtest.Encoded(t, doc)

	build(t, doc, plan.Policy{ProtocolEffects: true})

	assert.Equal(t, before, irtest.Encoded(t, doc))
}

// TestPlan_EncodesItsOperationsAsOneDocument holds a plan's operations to
// decoding to what encoded them: an Auth that is nil, empty or a list, an
// empty option inside one, Effects and Inferred.
func TestPlan_EncodesItsOperationsAsOneDocument(t *testing.T) {
	t.Parallel()
	built := build(t, operationsDoc(), plan.Policy{ProtocolEffects: true})
	require.NotNil(t, built.Ops["operation/public"].Auth, "the fixture holds an empty Auth")
	require.Nil(t, built.Ops["operation/webhook"].Auth, "the fixture holds a nil Auth")

	raw, err := json.Marshal(built)
	require.NoError(t, err)
	var decoded plan.Plan
	require.NoError(t, json.Unmarshal(raw, &decoded))

	assert.Empty(t, cmp.Diff(built, &decoded))
}
