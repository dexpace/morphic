// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/ctxtest"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// named is the Naming a compiler gives an entity the source spells src.
func named(src string) ir.Naming {
	return ir.Naming{Source: src, Canonical: ir.CanonicalWords(src)}
}

// hinted is the Naming of an entity the source names nowhere.
func hinted(hint string) ir.Naming { return ir.Naming{Hint: hint} }

// collision is the warning pass.Names reports at node.
func collision(node, message string) ir.Diagnostic {
	return ir.NewDiagnostic(ir.SeverityWarning, "pass/name-collision",
		message+", so a target can tell them apart only by ID", ir.Provenance{Source: ir.NoSource, Node: node})
}

// byPosition is the warning pass.Names reports at node about parties named
// alike that hold no ID.
func byPosition(node, message string) ir.Diagnostic {
	return ir.NewDiagnostic(ir.SeverityWarning, "pass/name-collision",
		message+", so a target can tell them apart only by position", ir.Provenance{Source: ir.NoSource, Node: node})
}

// brokenApart is the warning pass.Names reports at node about names whose
// words differ only where they break.
func brokenApart(node, message string) ir.Diagnostic {
	return ir.NewDiagnostic(ir.SeverityWarning, "pass/name-collision", message+", which differ only where their "+
		"words break, so a target that joins words or ignores letter case can tell them apart only by ID",
		ir.Provenance{Source: ir.NoSource, Node: node})
}

// wordless is the info pass.Names reports at node.
func wordless(node, message string) ir.Diagnostic {
	return ir.NewDiagnostic(ir.SeverityInfo, "pass/name-without-words",
		message+" has no word character, so it has no neutral name, and a target names it from its characters",
		ir.Provenance{Source: ir.NoSource, Node: node})
}

// namesOf is what pass.Names reports about doc, failing t unless it hands
// doc back.
func namesOf(t *testing.T, doc *ir.Document) []ir.Diagnostic {
	t.Helper()
	out, diags := pass.Names{}.Run(t.Context(), doc)
	require.Same(t, doc, out, "an analysis hands back the document it was given")
	return diags
}

// inGroup is a document whose one service holds operationGroup.
func inGroup(operationGroup ir.OperationGroup) *ir.Document {
	return &ir.Document{Services: []ir.Service{{ID: "s", Groups: []ir.OperationGroup{operationGroup}}}}
}

// withTypes is a document holding types.
func withTypes(types ...ir.TypeDef) *ir.Document {
	doc := &ir.Document{Types: ir.TypeRegistry{}}
	for _, td := range types {
		doc.Types[td.Common().ID] = td
	}
	return doc
}

// TestNames_ReportsEachScope runs each case many times: a scope read from a map
// in the map's order rather than by ID differs only in some runs.
func TestNames_ReportsEachScope(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		doc  *ir.Document
		want []ir.Diagnostic
	}{
		{"services", &ir.Document{Services: []ir.Service{{ID: "service/a", Name: named("Billing")}, {ID: "service/b", Name: named("billing")}}},
			[]ir.Diagnostic{collision("document", `services service/a ("Billing") and service/b ("billing") share the neutral name "billing"`)}},
		{"a hint beside the source spelling it", &ir.Document{Services: []ir.Service{{ID: "s", Groups: []ir.OperationGroup{
			{ID: "operation-group/tags/default", Name: named("default")}, {ID: "operation-group/default", Name: hinted("default")}}}}},
			[]ir.Diagnostic{collision("s",
				`in service s, operation groups operation-group/tags/default ("default") and operation-group/default share the neutral name "default"`)}},
		{"a group's groups", inGroup(ir.OperationGroup{ID: "operation-group/p", Groups: []ir.OperationGroup{
			{ID: "operation-group/p/1", Name: named("Admin")}, {ID: "operation-group/p/2", Name: named("admin")}}}),
			[]ir.Diagnostic{collision("operation-group/p",
				`in operation group operation-group/p, operation groups operation-group/p/1 ("Admin") and operation-group/p/2 ("admin") share the neutral name "admin"`)}},
		{"a group's operations", inGroup(ir.OperationGroup{ID: "g", Operations: []ir.Operation{
			{ID: "operation/1", Name: named("getUser")}, {ID: "operation/2", Name: named("get_user")}, {ID: "operation/3", Name: named("GetUser")}}}),
			[]ir.Diagnostic{collision("g", `in operation group g, operations operation/1 ("getUser"), operation/2 ("get_user") `+
				`and operation/3 ("GetUser") share the neutral name "get_user"`)}},
		{"an operation's parameters", inGroup(ir.OperationGroup{ID: "g", Operations: []ir.Operation{{ID: "op", Params: []ir.Parameter{
			{ID: "parameter/a", Name: named("account_id")}, {ID: "parameter/b", Name: named("limit")}, {ID: "parameter/c", Name: named("accountId")}}}}}),
			[]ir.Diagnostic{collision("op",
				`in operation op, parameters parameter/a ("account_id") and parameter/c ("accountId") share the neutral name "account_id"`)}},
		{"declared types, past those left out", withTypes(
			&ir.Model{ID: "type/b", Name: named("pet")},
			&ir.Enum{ID: "type/a", Name: named("Pet")},
			&ir.Model{ID: "type/0", Anonymous: true, Name: hinted("pet")},
			&ir.Primitive{ID: "type/1", Name: named("pet"), PrimitiveKind: ir.PrimitiveString}),
			[]ir.Diagnostic{collision("document", `types type/a ("Pet") and type/b ("pet") share the neutral name "pet"`)}},
		{"a model's properties", withTypes(&ir.Model{ID: "type/m", Anonymous: true, Name: hinted("m"),
			Properties: []ir.Property{{ID: "property/a", Name: named("userId")}, {ID: "property/b", Name: named("user_id")}}}),
			[]ir.Diagnostic{collision("type/m",
				`in model type/m, properties 0 ("userId") and 1 ("user_id") share the neutral name "user_id"`)}},
		{"an enum's members", withTypes(&ir.Enum{ID: "type/e", Name: named("Sign"),
			Members: []ir.EnumMember{{ID: "enum-member/-1", Name: named("-1")}, {ID: "enum-member/1", Name: named("1")}}}),
			[]ir.Diagnostic{collision("type/e", `in enum type/e, members 0 ("-1") and 1 ("1") share the neutral name "1"`)}},
		{"members whose values hold a control character, which their IDs repeat", withTypes(&ir.Enum{ID: "type/e",
			Name: named("E"), Members: []ir.EnumMember{{ID: "enum-member/s:a\nb", Name: named("a\nb")}, {ID: "enum-member/s:A\nb", Name: named("A\nb")}}}),
			[]ir.Diagnostic{collision("type/e", `in enum type/e, members 0 ("a\nb") and 1 ("A\nb") share the neutral name "a_b"`)}},
		{"a union's variants", withTypes(&ir.Union{ID: "type/u", Name: named("U"),
			Variants: []ir.Variant{{Name: hinted("a_model"), Type: ir.TypeRef{Target: "type/a"}},
				{Name: hinted("a_model"), Type: ir.TypeRef{Target: "type/b"}}}}),
			[]ir.Diagnostic{collision("type/u", `in union type/u, variants type/a and type/b share the neutral name "a_model"`)}},
		{"a service's groups whose words break apart", &ir.Document{Services: []ir.Service{{ID: "s", Groups: []ir.OperationGroup{
			{ID: "operation-group/1", Name: named("custom-hostname")}, {ID: "operation-group/2", Name: named("customhostname")}}}}},
			[]ir.Diagnostic{brokenApart("s", `in service s, operation groups operation-group/1 ("custom-hostname") and operation-group/2 ("customhostname") `+
				`have the neutral names "custom_hostname" and "customhostname"`)}},
		{"a group's groups whose words break apart", inGroup(ir.OperationGroup{ID: "operation-group/p", Groups: []ir.OperationGroup{
			{ID: "operation-group/p/1", Name: named("SetUp")}, {ID: "operation-group/p/2", Name: named("Setup")}}}),
			[]ir.Diagnostic{brokenApart("operation-group/p", `in operation group operation-group/p, operation groups operation-group/p/1 ("SetUp") and operation-group/p/2 ("Setup") `+
				`have the neutral names "set_up" and "setup"`)}},
		{"declared types whose words break apart, a hint and digits among them", withTypes(
			&ir.Model{ID: "type/a", Name: named("Setup")},
			&ir.Model{ID: "type/b", Name: named("v1.2")},
			&ir.Model{ID: "type/c", Name: named("SetUp")},
			&ir.Model{ID: "type/d", Name: named("v12")},
			&ir.Model{ID: "type/e", Name: hinted("set_up")}),
			[]ir.Diagnostic{
				brokenApart("document", `types type/a ("Setup"), type/c ("SetUp") and type/e have the neutral names "setup" and "set_up"`),
				brokenApart("document", `types type/b ("v1.2") and type/d ("v12") have the neutral names "v_1_2" and "v_12"`),
			}},
		{"an operation's parameter choices, and a choice's variants", inGroup(ir.OperationGroup{ID: "g",
			Operations: []ir.Operation{{ID: "op", ParamChoices: []ir.ParamChoice{
				{Name: named("byDate"), Variants: []ir.ParamVariant{{Name: named("fromTo")}, {Name: named("from_to")}}},
				{Name: named("by_date"), Variants: []ir.ParamVariant{{Name: named("a")}, {Name: named("b")}}},
			}}}}),
			[]ir.Diagnostic{
				byPosition("op", `in operation op, parameter choices 0 ("byDate") and 1 ("by_date") share the neutral name "by_date"`),
				byPosition("op", `in parameter choice 0 of operation op, variants 0 ("fromTo") and 1 ("from_to") share the `+
					`neutral name "from_to"`),
			}},
		{"a union's variants whose words break apart", withTypes(&ir.Union{ID: "type/u", Name: named("U"),
			Variants: []ir.Variant{{Name: hinted("credit_card"), Type: ir.TypeRef{Target: "type/a"}},
				{Name: hinted("creditcard"), Type: ir.TypeRef{Target: "type/b"}}}}),
			[]ir.Diagnostic{brokenApart("type/u",
				`in union type/u, variants type/a and type/b have the neutral names "credit_card" and "creditcard"`)}},
		{"spellings with no word character, compared as they are where words are joined", withTypes(
			&ir.Model{ID: "type/1", Name: named("_-")}, &ir.Model{ID: "type/2", Name: named("-")}),
			[]ir.Diagnostic{wordless("document", `type type/1 ("_-")`), wordless("document", `type type/2 ("-")`)}},
		{"auth schemes, in ID order", &ir.Document{Auth: map[ir.AuthID]ir.AuthScheme{
			"auth/b": {ID: "auth/b", Name: named("APIToken")}, "auth/a": {ID: "auth/a", Name: named("api_token")}}},
			[]ir.Diagnostic{collision("document",
				`auth schemes auth/a ("api_token") and auth/b ("APIToken") share the neutral name "api_token"`)}},
		{"a name with no word character", withTypes(
			&ir.Model{ID: "type/***", Name: named("***")},
			&ir.Enum{ID: "type/ops", Name: named("Ops"),
				Members: []ir.EnumMember{{ID: "enum-member/a", Name: named("a")}, {ID: "enum-member/<", Name: named("<")}, {ID: "enum-member/A", Name: named("A")}}}),
			[]ir.Diagnostic{
				collision("type/ops", `in enum type/ops, members 0 ("a") and 2 ("A") share the neutral name "a"`),
				wordless("type/ops", `in enum type/ops, member 1 ("<")`),
				wordless("document", `type type/*** ("***")`),
			}},
		{"a name with no word character, repeated", withTypes(&ir.Enum{ID: "type/ops", Name: named("Ops"), Members: []ir.EnumMember{
			{ID: "enum-member/>=", Name: named(">=")}, {ID: "enum-member/=", Name: named("=")}, {ID: "enum-member/>=/2", Name: named(">=")}}}),
			[]ir.Diagnostic{
				collision("type/ops", `in enum type/ops, members 0 (">=") and 2 (">=") share the spelling ">=", which has no word character`),
				wordless("type/ops", `in enum type/ops, member 0 (">=")`),
				wordless("type/ops", `in enum type/ops, member 1 ("=")`),
				wordless("type/ops", `in enum type/ops, member 2 (">=")`),
			}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for range 16 {
				if !assert.Empty(t, cmp.Diff(tt.want, namesOf(t, tt.doc))) {
					break
				}
			}
		})
	}
}

// TestNames_ReportsAScopesSharedNamesInTheOrderEachIsRepeated holds the order
// of a scope's findings to where each name is first repeated: the members are
// declared so that the name read first is repeated last, which an order by
// first party would report first.
func TestNames_ReportsAScopesSharedNamesInTheOrderEachIsRepeated(t *testing.T) {
	t.Parallel()
	doc := withTypes(&ir.Enum{ID: "type/e", Name: named("E"), Members: []ir.EnumMember{
		{ID: "enum-member/0", Name: named("a")}, {ID: "enum-member/1", Name: named("b")}, {ID: "enum-member/2", Name: named("B")},
		{ID: "enum-member/3", Name: named("A")}, {ID: "enum-member/4", Name: named("b_")},
	}})

	assert.Empty(t, cmp.Diff([]ir.Diagnostic{
		collision("type/e", `in enum type/e, members 1 ("b"), 2 ("B") and 4 ("b_") share the neutral name "b"`),
		collision("type/e", `in enum type/e, members 0 ("a") and 3 ("A") share the neutral name "a"`),
	}, namesOf(t, doc)))
}

func TestNames_LeavesWhatNoTargetRendersAlikeAlone(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		doc  *ir.Document
	}{
		{"no document", nil},
		{"names that differ", inGroup(ir.OperationGroup{ID: "g", Name: named("users"), Operations: []ir.Operation{
			{ID: "operation/1", Name: named("getUser"), Params: []ir.Parameter{{ID: "parameter/1", Name: named("id")}}},
			{ID: "operation/2", Name: named("getUsers"), Params: []ir.Parameter{{ID: "parameter/2", Name: named("id")}}},
		}})},
		{"one name in two scopes", &ir.Document{Services: []ir.Service{{ID: "s", Groups: []ir.OperationGroup{
			{ID: "operation-group/1", Operations: []ir.Operation{{ID: "operation/1", Name: named("list")}}},
			{ID: "operation-group/2", Operations: []ir.Operation{{ID: "operation/2", Name: named("list")}},
				Groups: []ir.OperationGroup{{ID: "operation-group/2/1", Operations: []ir.Operation{{ID: "operation/3", Name: named("list")}}}}},
		}}}, Types: ir.TypeRegistry{
			"type/a": &ir.Model{ID: "type/a", Name: named("A"), Properties: []ir.Property{{ID: "property/a", Name: named("id")}}},
			"type/b": &ir.Model{ID: "type/b", Name: named("B"), Properties: []ir.Property{{ID: "property/b", Name: named("id")}}},
		}}},
		{"words that break apart where a scope's targets keep the breaks", &ir.Document{
			Services: []ir.Service{{ID: "service/1", Name: named("set_up"), Groups: []ir.OperationGroup{{ID: "g",
				Operations: []ir.Operation{
					{ID: "operation/1", Name: named("setUp"), Params: []ir.Parameter{
						{ID: "parameter/1", Name: named("page_size")}, {ID: "parameter/2", Name: named("pagesize")}}},
					{ID: "operation/2", Name: named("setup")},
				}}}}, {ID: "service/2", Name: named("setup")}},
			Types: ir.TypeRegistry{
				"type/m": &ir.Model{ID: "type/m", Name: named("M"), Properties: []ir.Property{
					{ID: "property/1", Name: named("user_name")}, {ID: "property/2", Name: named("username")}}},
				"type/e": &ir.Enum{ID: "type/e", Name: named("E"), Members: []ir.EnumMember{
					{ID: "enum-member/1", Name: named("SET_UP")}, {ID: "enum-member/2", Name: named("SETUP")}}},
			},
			Auth: map[ir.AuthID]ir.AuthScheme{
				"auth/1": {ID: "auth/1", Name: named("api_key")}, "auth/2": {ID: "auth/2", Name: named("apikey")}},
		}},
		{"names whose joined words differ, where words are joined", &ir.Document{
			Services: []ir.Service{{ID: "s", Groups: []ir.OperationGroup{
				{ID: "operation-group/1", Name: named("custom_host"), Groups: []ir.OperationGroup{
					{ID: "operation-group/1/1", Name: named("set_up")}, {ID: "operation-group/1/2", Name: named("setups")}}},
				{ID: "operation-group/2", Name: named("customhostname")}}}},
			Types: ir.TypeRegistry{
				"type/a": &ir.Model{ID: "type/a", Name: named("SetUp")},
				"type/b": &ir.Model{ID: "type/b", Name: named("Setups")},
				"type/u": &ir.Union{ID: "type/u", Name: named("U"), Variants: []ir.Variant{
					{Name: hinted("credit_card"), Type: ir.TypeRef{Target: "type/a"}},
					{Name: hinted("credit_cards"), Type: ir.TypeRef{Target: "type/b"}}}},
			},
		}},
		{"anonymous types sharing a hint, or a declared type's name", withTypes(
			&ir.Model{ID: "type/1", Anonymous: true, Name: hinted("direction")},
			&ir.Enum{ID: "type/2", Anonymous: true, Name: hinted("direction")},
			&ir.Enum{ID: "type/3", Name: named("Direction")})},
		{"a type the type table resolves, named as a declared one is", withTypes(
			&ir.Primitive{ID: "type/primitive/string", Name: named("string"), PrimitiveKind: ir.PrimitiveString},
			&ir.Any{ID: "type/any", Name: named("String")},
			&ir.External{ID: "type/external", Name: named("STRING"), Identity: "bytes:string"},
			&ir.Scalar{ID: "type/string", Name: named("string_")})},
		{"names naming nothing, which irverify reports", inGroup(ir.OperationGroup{ID: "g",
			Operations: []ir.Operation{{ID: "operation/1"}, {ID: "operation/2"}}})},
		{"a nil entry of the registry", &ir.Document{Types: ir.TypeRegistry{"type/nil": (*ir.Model)(nil)}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, diags := pass.Names{}.Run(t.Context(), tt.doc)
			assert.Empty(t, diags)
		})
	}
}

// TestNames_RunReportsNothingOnceItsContextIsDone cancels a Run at every check
// of its context a whole one makes and requires the document back with no
// finding and no ask after the refusal, so a cancel waits out one family of
// scopes at most and a family cut short is never reported.
func TestNames_RunReportsNothingOnceItsContextIsDone(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Auth: map[ir.AuthID]ir.AuthScheme{"auth/a": {ID: "auth/a", Name: named("a")},
		"auth/b": {ID: "auth/b", Name: named("A")}}}
	whole := &ctxtest.Countdown{Context: t.Context(), Left: math.MaxInt}
	_, diags := pass.Names{}.Run(whole, doc)
	require.NotEmpty(t, diags, "the last family has a finding a cut run could leak")
	require.Equal(t, pass.NameFamilies()+1, whole.Calls, "a check before each family and after the last")

	for left := range whole.Calls {
		ctx := &ctxtest.Countdown{Context: t.Context(), Left: left}

		out, diags := pass.Names{}.Run(ctx, doc)

		assert.Same(t, doc, out)
		require.Nil(t, diags, "check %d", left+1)
		require.Equal(t, left+1, ctx.Calls, "check %d: asked on after the refusal", left+1)
	}
}

func TestNames_NameIsStable(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "names", pass.Names{}.Name())
	assert.Equal(t, pass.NamesName, pass.Names{}.Name())
}

// soundNames is a document of n units of eight named entities: a service, its
// group, the group's operation and the operation's parameter, a model with a
// property, an enum with a member, a union with a variant, and an auth scheme,
// no two names of a scope alike.
func soundNames(n int) *ir.Document {
	doc := &ir.Document{Types: ir.TypeRegistry{}, Auth: map[ir.AuthID]ir.AuthScheme{}}
	for i := range n {
		word := fmt.Sprintf("n%d", i)
		doc.Services = append(doc.Services, ir.Service{ID: ir.ServiceID("service/" + word), Name: named(word),
			Groups: []ir.OperationGroup{{ID: ir.GroupID("operation-group/" + word), Name: named(word),
				Operations: []ir.Operation{{ID: ir.OperationID("operation/" + word), Name: named(word),
					Params: []ir.Parameter{{ID: ir.ParameterID("parameter/" + word), Name: named(word)}}}}}}})
		for _, td := range []ir.TypeDef{
			&ir.Model{ID: ir.TypeID("type/m/" + word), Name: named("m" + word),
				Properties: []ir.Property{{ID: ir.PropertyID("property/" + word), Name: named(word)}}},
			&ir.Enum{ID: ir.TypeID("type/e/" + word), Name: named("e" + word),
				Members: []ir.EnumMember{{ID: ir.EnumMemberID("enum-member/" + word), Name: named(word)}}},
			&ir.Union{ID: ir.TypeID("type/u/" + word), Name: named("u" + word),
				Variants: []ir.Variant{{Name: hinted(word)}}},
		} {
			doc.Types[td.Common().ID] = td
		}
		doc.Auth[ir.AuthID("auth/"+word)] = ir.AuthScheme{ID: ir.AuthID("auth/" + word), Name: named(word)}
	}
	return doc
}

// TestNames_ASoundDocumentCostsNoAllocationPerEntity holds a Run over a
// document with nothing to report to fewer allocations more, when the document
// gains n units of soundNames, than n, so no scope allocates for a name it
// reads: a message or a list of IDs is made only for a finding. What the run
// sorts, and the buffers and map it reuses, grow in steps. It is not run in
// parallel: AllocsPerRun measures the process.
func TestNames_ASoundDocumentCostsNoAllocationPerEntity(t *testing.T) {
	const units = 50
	few, many := soundNames(units), soundNames(2*units)
	require.Empty(t, namesOf(t, many), "the document is sound, or what is measured is a report")

	cost := func(doc *ir.Document) float64 {
		fewest := math.Inf(1)
		for range 20 {
			fewest = min(fewest, testing.AllocsPerRun(1, func() { pass.Names{}.Run(t.Context(), doc) }))
		}
		return fewest
	}
	grown := cost(many) - cost(few)
	assert.Less(t, grown, float64(units), "%v allocations more for %d units more", grown, units)
}
