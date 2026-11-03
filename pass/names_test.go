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

// inGroup is a document whose one service holds g.
func inGroup(g ir.OperationGroup) *ir.Document {
	return &ir.Document{Services: []ir.Service{{ID: "s", Groups: []ir.OperationGroup{g}}}}
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
		{"services", &ir.Document{Services: []ir.Service{{ID: "s/a", Name: named("Billing")}, {ID: "s/b", Name: named("billing")}}},
			[]ir.Diagnostic{collision("doc", `services s/a ("Billing") and s/b ("billing") share the neutral name "billing"`)}},
		{"a hint beside the source spelling it", &ir.Document{Services: []ir.Service{{ID: "s", Groups: []ir.OperationGroup{
			{ID: "g/tags/default", Name: named("default")}, {ID: "g/default", Name: hinted("default")}}}}},
			[]ir.Diagnostic{collision("s",
				`in service s, operation groups g/tags/default ("default") and g/default share the neutral name "default"`)}},
		{"a group's groups", inGroup(ir.OperationGroup{ID: "g/p", Groups: []ir.OperationGroup{
			{ID: "g/p/1", Name: named("Admin")}, {ID: "g/p/2", Name: named("admin")}}}),
			[]ir.Diagnostic{collision("g/p",
				`in operation group g/p, operation groups g/p/1 ("Admin") and g/p/2 ("admin") share the neutral name "admin"`)}},
		{"a group's operations", inGroup(ir.OperationGroup{ID: "g", Operations: []ir.Operation{
			{ID: "op/1", Name: named("getUser")}, {ID: "op/2", Name: named("get_user")}, {ID: "op/3", Name: named("GetUser")}}}),
			[]ir.Diagnostic{collision("g", `in operation group g, operations op/1 ("getUser"), op/2 ("get_user") `+
				`and op/3 ("GetUser") share the neutral name "get_user"`)}},
		{"an operation's parameters", inGroup(ir.OperationGroup{ID: "g", Operations: []ir.Operation{{ID: "op", Params: []ir.Parameter{
			{ID: "param/a", Name: named("account_id")}, {ID: "param/b", Name: named("limit")}, {ID: "param/c", Name: named("accountId")}}}}}),
			[]ir.Diagnostic{collision("op",
				`in operation op, parameters param/a ("account_id") and param/c ("accountId") share the neutral name "account_id"`)}},
		{"declared types, past those left out", withTypes(
			&ir.Model{ID: "t/b", Name: named("pet")},
			&ir.Enum{ID: "t/a", Name: named("Pet")},
			&ir.Model{ID: "t/0", Anonymous: true, Name: hinted("pet")},
			&ir.Primitive{ID: "t/1", Name: named("pet"), Prim: ir.PrimString}),
			[]ir.Diagnostic{collision("doc", `types t/a ("Pet") and t/b ("pet") share the neutral name "pet"`)}},
		{"a model's properties", withTypes(&ir.Model{ID: "t/m", Anonymous: true, Name: hinted("m"),
			Properties: []ir.Property{{ID: "p/a", Name: named("userId")}, {ID: "p/b", Name: named("user_id")}}}),
			[]ir.Diagnostic{collision("t/m",
				`in model t/m, properties p/a ("userId") and p/b ("user_id") share the neutral name "user_id"`)}},
		{"an enum's members", withTypes(&ir.Enum{ID: "t/e", Name: named("Sign"),
			Members: []ir.EnumMember{{ID: "e/-1", Name: named("-1")}, {ID: "e/1", Name: named("1")}}}),
			[]ir.Diagnostic{collision("t/e", `in enum t/e, members e/-1 ("-1") and e/1 ("1") share the neutral name "1"`)}},
		{"a union's variants", withTypes(&ir.Union{ID: "t/u", Name: named("U"),
			Variants: []ir.Variant{{Name: hinted("a_model"), Type: ir.TypeRef{Target: "t/a"}},
				{Name: hinted("a_model"), Type: ir.TypeRef{Target: "t/b"}}}}),
			[]ir.Diagnostic{collision("t/u", `in union t/u, variants t/a and t/b share the neutral name "a_model"`)}},
		{"auth schemes, in ID order", &ir.Document{Auth: map[ir.AuthID]ir.AuthScheme{
			"auth/b": {ID: "auth/b", Name: named("APIToken")}, "auth/a": {ID: "auth/a", Name: named("api_token")}}},
			[]ir.Diagnostic{collision("doc",
				`auth schemes auth/a ("api_token") and auth/b ("APIToken") share the neutral name "api_token"`)}},
		{"a name with no word character", withTypes(
			&ir.Model{ID: "t/***", Name: named("***")},
			&ir.Enum{ID: "t/ops", Name: named("Ops"),
				Members: []ir.EnumMember{{ID: "e/a", Name: named("a")}, {ID: "e/<", Name: named("<")}, {ID: "e/A", Name: named("A")}}}),
			[]ir.Diagnostic{
				collision("t/ops", `in enum t/ops, members e/a ("a") and e/A ("A") share the neutral name "a"`),
				wordless("t/ops", `in enum t/ops, member e/< ("<")`),
				wordless("doc", `type t/*** ("***")`),
			}},
		{"a name with no word character, repeated", withTypes(&ir.Enum{ID: "t/ops", Name: named("Ops"), Members: []ir.EnumMember{
			{ID: "e/>=", Name: named(">=")}, {ID: "e/=", Name: named("=")}, {ID: "e/>=/2", Name: named(">=")}}}),
			[]ir.Diagnostic{
				collision("t/ops", `in enum t/ops, members e/>= (">=") and e/>=/2 (">=") share the spelling ">=", `+
					`which has no word character`),
				wordless("t/ops", `in enum t/ops, member e/>= (">=")`),
				wordless("t/ops", `in enum t/ops, member e/= ("=")`),
				wordless("t/ops", `in enum t/ops, member e/>=/2 (">=")`),
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
	doc := withTypes(&ir.Enum{ID: "t/e", Name: named("E"), Members: []ir.EnumMember{
		{ID: "e/0", Name: named("a")}, {ID: "e/1", Name: named("b")}, {ID: "e/2", Name: named("B")},
		{ID: "e/3", Name: named("A")}, {ID: "e/4", Name: named("b_")},
	}})

	assert.Empty(t, cmp.Diff([]ir.Diagnostic{
		collision("t/e", `in enum t/e, members e/1 ("b"), e/2 ("B") and e/4 ("b_") share the neutral name "b"`),
		collision("t/e", `in enum t/e, members e/0 ("a") and e/3 ("A") share the neutral name "a"`),
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
			{ID: "op/1", Name: named("getUser"), Params: []ir.Parameter{{ID: "param/1", Name: named("id")}}},
			{ID: "op/2", Name: named("getUsers"), Params: []ir.Parameter{{ID: "param/2", Name: named("id")}}},
		}})},
		{"one name in two scopes", &ir.Document{Services: []ir.Service{{ID: "s", Groups: []ir.OperationGroup{
			{ID: "g/1", Operations: []ir.Operation{{ID: "op/1", Name: named("list")}}},
			{ID: "g/2", Operations: []ir.Operation{{ID: "op/2", Name: named("list")}},
				Groups: []ir.OperationGroup{{ID: "g/2/1", Operations: []ir.Operation{{ID: "op/3", Name: named("list")}}}}},
		}}}, Types: ir.TypeRegistry{
			"t/a": &ir.Model{ID: "t/a", Name: named("A"), Properties: []ir.Property{{ID: "p/a", Name: named("id")}}},
			"t/b": &ir.Model{ID: "t/b", Name: named("B"), Properties: []ir.Property{{ID: "p/b", Name: named("id")}}},
		}}},
		{"anonymous types sharing a hint, or a declared type's name", withTypes(
			&ir.Model{ID: "t/1", Anonymous: true, Name: hinted("direction")},
			&ir.Enum{ID: "t/2", Anonymous: true, Name: hinted("direction")},
			&ir.Enum{ID: "t/3", Name: named("Direction")})},
		{"a type the type table resolves, named as a declared one is", withTypes(
			&ir.Primitive{ID: "t/prim/string", Name: named("string"), Prim: ir.PrimString},
			&ir.Any{ID: "t/any", Name: named("String")},
			&ir.External{ID: "t/external", Name: named("STRING"), Identity: "x:string"},
			&ir.Scalar{ID: "t/string", Name: named("string_")})},
		{"names naming nothing, which irverify reports", inGroup(ir.OperationGroup{ID: "g",
			Operations: []ir.Operation{{ID: "op/1"}, {ID: "op/2"}}})},
		{"a nil entry of the registry", &ir.Document{Types: ir.TypeRegistry{"t/nil": (*ir.Model)(nil)}}},
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
		doc.Services = append(doc.Services, ir.Service{ID: ir.ServiceID("s/" + word), Name: named(word),
			Groups: []ir.OperationGroup{{ID: ir.GroupID("g/" + word), Name: named(word),
				Operations: []ir.Operation{{ID: ir.OpID("op/" + word), Name: named(word),
					Params: []ir.Parameter{{ID: ir.ParamID("param/" + word), Name: named(word)}}}}}}})
		for _, td := range []ir.TypeDef{
			&ir.Model{ID: ir.TypeID("t/m/" + word), Name: named("m" + word),
				Properties: []ir.Property{{ID: ir.PropID("p/" + word), Name: named(word)}}},
			&ir.Enum{ID: ir.TypeID("t/e/" + word), Name: named("e" + word),
				Members: []ir.EnumMember{{ID: ir.EnumMemberID("e/" + word), Name: named(word)}}},
			&ir.Union{ID: ir.TypeID("t/u/" + word), Name: named("u" + word),
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
