// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"fmt"
	"math"
	"runtime"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// TestUnionHeld_AReadCutShortSaysNo covers unionHeld's own contract rather than
// its callers', whose refinement declines first wherever a union's read is cut
// short: a branch restating what the position holds says the union only while
// every branch's read finishes.
func TestUnionHeld_AReadCutShortSaysNo(t *testing.T) {
	t.Parallel()
	js := &oas3.JSONSchema[oas3.Referenceable]{}
	valErrs, err := marshaller.Unmarshal(t.Context(), strings.NewReader("{anyOf: [{type: string}, {type: integer}]}"), js)
	require.NoError(t, err)
	require.Empty(t, valErrs, "the fixture parses cleanly")
	lowerer, _ := LoweredFor(t, openapitest.ComponentSpecVer("3.1.0", "    A: {type: string}\n"))
	refinementValue := refinement{held: admitted{kinds: kindString}}

	whole := newAdmissionReader(lowerer.Ctx)
	assert.True(t, unionHeld(&whole, refinementValue, StandaloneSchema(js.GetSchema()), "anyOf"), "read whole, the string branch says the union")
	cut := newAdmissionReader(lowerer.Ctx)
	require.True(t, cut.reads.Spend(maxAdmissionReads), "the budget holds what is spent")
	assert.False(t, unionHeld(&cut, refinementValue, StandaloneSchema(js.GetSchema()), "anyOf"), "the integer branch's read runs past it")
}

// TestRecordFoldedBranchResidue_KeepsWhatItsFoldedBranchesWrite pins the
// residue a position's foldedBranches write, kept on the node in hand and
// reported, and nothing where no folded branch speaks.
func TestRecordFoldedBranchResidue_KeepsWhatItsFoldedBranchesWrite(t *testing.T) {
	t.Parallel()
	lowerer, schema, at := lowererDeclaringS(t)
	foldedBranches := []annotation.FoldedAllOfBranch{{Node: schema, At: at}}
	beside := StandaloneSchema(&oas3.Schema{})
	assert.Empty(t, recordFoldedBranchResidue(lowerer.Ctx, &ir.Model{}, beside, nil), "no voice keeps nothing")
	node := &ir.Model{}
	assert.NotEmpty(t, recordFoldedBranchResidue(lowerer.Ctx, node, beside, foldedBranches))
	assert.Contains(t, node.Unmodeled, "openapi:readOnly", "the node in hand keeps what its voice writes")
	assert.Empty(t, recordFoldedBranchResidue(lowerer.Ctx, &ir.Model{}, schema, foldedBranches), "what the position writes too is the position's")
}

// componentSchema returns the schema the component name declares.
func componentSchema(t *testing.T, lowerer *Lowerer, name string) *oas3.Schema {
	t.Helper()
	js, ok := lowerer.Doc.Components.Schemas.Get(name)
	require.True(t, ok, name)
	return js.GetSchema()
}

// TestRefinementOf_ReadsASchemaOnce pins that a schema's decision is read once
// a run, however many of its readers ask and from wherever: its own type
// changes after the first read, which no lowering does, so only a remembered
// answer stays. Where it is read from only places its branches, so another
// pointer, and a reader asking with none, read the same answer, the branches
// at the pointer asked. The note beside the allOf makes it more than a use of
// its one $ref, which is the reference whatever its kinds.
func TestRefinementOf_ReadsASchemaOnce(t *testing.T) {
	t.Parallel()
	lowerer, _ := LoweredFor(t, openapitest.ComponentSpec(`    Port: {type: integer}
    T: {allOf: [{$ref: '#/components/schemas/Port'}], description: d}
`))
	const at = "/components/schemas/T"
	s := componentSchema(t, lowerer, "T")
	refinement, refined := refinementOf(lowerer.Ctx, StatedIn(lowerer, s), at)
	require.True(t, refined)
	require.NotNil(t, refinement.principal)
	require.Equal(t, jsontext.Pointer(at+"/allOf/0"), refinement.principal.pointer)

	StatedIn(lowerer, s).Kinds = &statement.Kinds{Types: []statement.JSONSchemaTypeName{statement.KindObject}}
	_, refined = refinementOf(lowerer.Ctx, StatedIn(lowerer, s), at)
	assert.True(t, refined, "the second ask reads what the first read")

	refinement, refined = refinementOf(lowerer.Ctx, StatedIn(lowerer, s), "/elsewhere")
	require.True(t, refined, "another pointer reads the same answer")
	require.NotNil(t, refinement.principal)
	assert.Equal(t, jsontext.Pointer("/elsewhere/allOf/0"), refinement.principal.pointer, "its branches stand at the pointer asked")
	assert.Same(t, refinement.principal, refinement.ref, "the principal is the $ref read through")
	assert.True(t, decisionOf(lowerer.Ctx, StatedIn(lowerer, s)).refines, "a reader with no pointer reads it too")
}

// TestAdmissionReader_ReadsAValueSetOnce pins that an enum is read once a run however
// many positions read it, which is what keeps positions times members from being
// the cost of a shared enum: a member changes after the first read, which no
// lowering does, so only a remembered set stays.
func TestAdmissionReader_ReadsAValueSetOnce(t *testing.T) {
	t.Parallel()
	lowerer, _ := LoweredFor(t, openapitest.ComponentSpec("    E: {type: string, enum: [a, b]}\n"))
	s := componentSchema(t, lowerer, "E")
	first := newAdmissionReader(lowerer.Ctx)
	require.Contains(t, first.written(StatedIn(lowerer, s), first.nullable).values, "string:a")

	s.GetEnum()[0].Value = "z"
	second := newAdmissionReader(lowerer.Ctx)
	read := second.written(StatedIn(lowerer, s), second.nullable)
	assert.Contains(t, read.values, "string:a", "the second read takes what the first remembered")
	assert.NotContains(t, read.values, "string:z")
}

// TestFoldingAllOfBranches_ReadsASchemaOnce pins that what folds is read once a
// run however many uses of a schema ask, which keeps a schema's uses times its
// branches from being the cost of a wide composition: its allOf goes after the
// first read, which no lowering does, so only a remembered answer stays.
func TestFoldingAllOfBranches_ReadsASchemaOnce(t *testing.T) {
	t.Parallel()
	lowerer, _ := LoweredFor(t, openapitest.ComponentSpec(`    Str: {type: string}
    T: {allOf: [{$ref: '#/components/schemas/Str'}, {description: d}]}
`))
	s := componentSchema(t, lowerer, "T")
	require.Equal(t, []bool{false, true}, foldingAllOfBranches(lowerer.Ctx, StatedIn(lowerer, s)))

	st := lowerer.Ctx.Statements().Read(StatedIn(lowerer, s))
	st.AllOf, st.Entries = nil, nil
	assert.Equal(t, []bool{false, true}, foldingAllOfBranches(lowerer.Ctx, StatedIn(lowerer, s)), "the second ask reads what the first read")
}

// TestNullableFoldingBranches_ReadsASchemaOnce pins that the branches widening
// a schema are read once a run, as its folding is, since every use of the
// schema asks: its allOf goes after the first read, so only a remembered answer
// stays.
func TestNullableFoldingBranches_ReadsASchemaOnce(t *testing.T) {
	t.Parallel()
	lowerer, _ := LoweredFor(t, openapitest.ComponentSpec(`    Str: {type: string}
    T: {allOf: [{$ref: '#/components/schemas/Str'}, {nullable: true}]}
`))
	s := componentSchema(t, lowerer, "T")
	first := nullableFoldingBranches(lowerer.Ctx, StatedIn(lowerer, s))
	require.Len(t, first, 1)
	assert.Equal(t, jsontext.Pointer("/allOf/1"), first[0].pointer, "each branch at its pointer under the schema's")

	st := lowerer.Ctx.Statements().Read(StatedIn(lowerer, s))
	st.AllOf, st.Entries = nil, nil
	assert.Equal(t, first, nullableFoldingBranches(lowerer.Ctx, StatedIn(lowerer, s)), "the second ask reads what the first read")
}

// TestValueNulls_ReadsASchemaOnce pins that what a schema's const, enum and union
// say of null is read once a run, since every $ref to the schema asks and the enum
// and union scan each member: they go after the first read, which no lowering
// does, so only a remembered answer stays.
func TestValueNulls_ReadsASchemaOnce(t *testing.T) {
	t.Parallel()
	lowerer, _ := LoweredFor(t, openapitest.ComponentSpec(`    V: {const: null, enum: [a, null]}
    U: {oneOf: [{type: string}, {type: 'null'}]}
`))
	v, u := componentSchema(t, lowerer, "V"), componentSchema(t, lowerer, "U")
	values := valueVerdicts{constant: nullAdmitted, enum: nullAdmitted}
	union := valueVerdicts{union: nullReadingAlikeUnderBothPolicies(nullAdmitted)}
	require.Equal(t, values, valueNulls(lowerer.Ctx, StatedIn(lowerer, v)))
	require.Equal(t, union, valueNulls(lowerer.Ctx, StatedIn(lowerer, u)))

	StatedIn(lowerer, v).Values = nil
	StatedIn(lowerer, u).Choice = nil
	assert.Equal(t, values, valueNulls(lowerer.Ctx, StatedIn(lowerer, v)), "the second ask reads what the first read")
	assert.Equal(t, union, valueNulls(lowerer.Ctx, StatedIn(lowerer, u)), "the second ask reads what the first read")
}

// TestFallbackAllOfBranches_FallsBackToEachEntryOnce pins what a use of a wide
// composition reads: a branch repeating what s or a branch before it writes
// lends nothing more, so a use pays for each entry once, while one writing a
// new entry lends it.
func TestFallbackAllOfBranches_FallsBackToEachEntryOnce(t *testing.T) {
	t.Parallel()
	lowerer, _ := LoweredFor(t, openapitest.ComponentSpec(`    Obj: {type: object}
    T:
      description: d
      allOf:
        - {$ref: '#/components/schemas/Obj'}
        - {description: d}
        - {description: d, readOnly: true}
        - {readOnly: true}
        - {description: d}
`))
	got := fallbackAllOfBranches(lowerer.Ctx, StatedIn(lowerer, componentSchema(t, lowerer, "T")))
	require.Len(t, got, 1, "only the branch first writing readOnly lends what T does not")
	assert.Equal(t, jsontext.Pointer("/allOf/2"), got[0].pointer)
	b := statement.ParsedAt(got[0].schema).GetSchema()
	require.NotNil(t, b.ReadOnly)
	assert.True(t, *b.ReadOnly)
	assert.Equal(t, "d", b.GetDescription())

	split, _ := LoweredFor(t, openapitest.ComponentSpec(`    Obj: {type: object}
    T: {allOf: [{$ref: '#/components/schemas/Obj'}, {title: a, description: d}, {title: b}, {description: d}]}
`))
	got = fallbackAllOfBranches(split.Ctx, StatedIn(split, componentSchema(t, split, "T")))
	require.Len(t, got, 1, "a branch kept for disagreeing lends nothing, nor marks what it writes as lent")
	assert.Empty(t, statement.ParsedAt(got[0].schema).GetSchema().GetTitle())
	assert.Equal(t, "d", statement.ParsedAt(got[0].schema).GetSchema().GetDescription())
}

// TestAnnotationsAUseFallsBackTo_ReadsATargetOnce pins that what a schema lends
// is read once a run however many $refs name it, for the reason
// foldingAllOfBranches's answer is: its allOf goes after the first read, so
// only a remembered chain stays.
func TestAnnotationsAUseFallsBackTo_ReadsATargetOnce(t *testing.T) {
	t.Parallel()
	lowerer, _ := LoweredFor(t, openapitest.ComponentSpec(`    Str: {type: string}
    T: {allOf: [{$ref: '#/components/schemas/Str'}, {description: d}]}
`))
	js, ok := lowerer.Doc.Components.Schemas.Get("T")
	require.True(t, ok)
	first := fallbackAnnotationsOf(lowerer.Ctx, lowerer.Ctx.Statements().Of(js)).Referents
	require.Len(t, first, 4, "T, the branch that folds, the $ref and what it names")

	st := lowerer.Ctx.Statements().Of(js)
	st.AllOf, st.Entries = nil, nil
	assert.Equal(t, first, fallbackAnnotationsOf(lowerer.Ctx, lowerer.Ctx.Statements().Of(js)).Referents, "the second ask reads what the first read")
}

// TestWrittenText_RendersASchemaOnce pins that a schema's text is rendered once a run
// however many positions restate it: the node changes after the first render, which
// no lowering does, so only remembered text stays.
func TestWrittenText_RendersASchemaOnce(t *testing.T) {
	t.Parallel()
	lowerer, _ := LoweredFor(t, openapitest.ComponentSpec("    T: {type: string}\n"))
	js, ok := lowerer.Doc.Components.Schemas.Get("T")
	require.True(t, ok)
	text := writtenText(lowerer.Ctx, lowerer.Ctx.Statements().Of(js))
	require.NotNil(t, text)

	*js.GetCore().GetRootNode() = yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "other"}
	assert.Equal(t, text, writtenText(lowerer.Ctx, lowerer.Ctx.Statements().Of(js)), "the second ask reads what the first rendered")
}

// TestReadAllOfBranchFolding_RendersEachValueOnce pins what a first writer
// costs: every later conjunct compares with it, so rendering it for each
// comparison made a document's branches times its longest value the cost of one
// composition. It is not parallel, as the count is the process's, and takes the
// least of three counts, since anything else allocating only adds to one.
func TestReadAllOfBranchFolding_RendersEachValueOnce(t *testing.T) {
	big := strings.Repeat("x", 1<<20)
	cost := func(later int) uint64 {
		var src strings.Builder
		src.WriteString("    T:\n      allOf:\n        - {description: " + big + "}\n")
		for i := range later {
			fmt.Fprintf(&src, "        - {description: d%d}\n", i)
		}
		lowerer, _ := LoweredFor(t, openapitest.ComponentSpec(src.String()))
		s := componentSchema(t, lowerer, "T")
		var stats runtime.MemStats
		least := uint64(math.MaxUint64)
		for range 3 {
			runtime.ReadMemStats(&stats)
			before := stats.TotalAlloc
			read := readAllOfBranchFolding(lowerer.Ctx, StatedIn(lowerer, s))
			runtime.ReadMemStats(&stats)
			least = min(least, stats.TotalAlloc-before)
			require.Len(t, read.branchFoldsIntoPosition, later+1)
		}
		return least
	}
	assert.Less(t, cost(256), 2*cost(64), "four times the branches, not four times the value")
}

// TestRead_AFoldedBranchCostsItsOwnEntries pins what a site's folded branches
// cost: each adds its own entries, where overlaying what came before on each
// made the cost of a composition its branches squared. It is not parallel, as
// the count is the process's, and takes the least of three counts for the same
// reason as TestReadAllOfBranchFolding_RendersEachValueOnce.
func TestRead_AFoldedBranchCostsItsOwnEntries(t *testing.T) {
	const at = "/components/schemas/T"
	cost := func(voices int) uint64 {
		var src strings.Builder
		src.WriteString("    O: {type: object}\n    T:\n      allOf:\n        - {$ref: '#/components/schemas/O'}\n")
		for i := range voices {
			fmt.Fprintf(&src, "        - {x-k%d: 1}\n", i)
		}
		lowerer, _ := LoweredFor(t, openapitest.ComponentSpec(src.String()))
		s := componentSchema(t, lowerer, "T")
		site := Use{FoldedAllOfBranches: positionFoldedBranches(lowerer.Ctx, StatedIn(lowerer, s), at, ir.TypeRef{})}.Site(StatedIn(lowerer, s))
		require.Len(t, site.FoldedAllOfBranches, voices)
		var stats runtime.MemStats
		least := uint64(math.MaxUint64)
		for range 3 {
			runtime.ReadMemStats(&stats)
			before := stats.TotalAlloc
			_, _ = annotation.Read(site, at, lowerer.Ctx.Place())
			runtime.ReadMemStats(&stats)
			least = min(least, stats.TotalAlloc-before)
		}
		return least
	}
	one, two, four := cost(500), cost(1000), cost(2000)
	assert.LessOrEqual(t, four-two, (two-one)*5/2, "linear growth gives 2, quadratic 4")
}

// TestKindOfValue_NamesEachValuesKind pins each kind of value to the JSON kind an
// instance of it is, and what is no instance to none.
func TestKindOfValue_NamesEachValuesKind(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		v    ir.Value
		want kindSet
	}{
		{ir.Value{Kind: ir.ValueString}, kindString},
		{ir.Value{Kind: ir.ValueSymbol}, kindString},
		{ir.Value{Kind: ir.ValueBytes}, kindString},
		{ir.Value{Kind: ir.ValueBool}, kindBoolean},
		{ir.Value{Kind: ir.ValueNull}, kindNull},
		{ir.Value{Kind: ir.ValueList}, kindArray},
		{ir.Value{Kind: ir.ValueObject}, kindObject},
		{ir.Value{Kind: ir.ValueNumber, Number: "1.0"}, kindInteger},
		{ir.Value{Kind: ir.ValueNumber, Number: "1.5"}, kindFraction},
		{ir.Value{Kind: ir.ValueNumber, Number: "1e2"}, kindNumber},
		{ir.Value{Kind: ir.ValueRefKind}, 0},
		{ir.Value{Kind: ir.ValueConstructor}, 0},
		{ir.Value{Kind: ir.ValueInfinity}, 0},
		{ir.Value{Kind: ir.ValueNegativeInfinity}, 0},
		{ir.Value{Kind: ir.ValueNaN}, 0},
		{ir.Value{Kind: "no kind"}, 0},
	} {
		assert.Equal(t, tc.want, kindOfValue(tc.v), tc.v.Kind)
	}
}

// TestAdmissionReader_AnEnumAndAConstAreTwoSets pins that one schema's two value sets are
// remembered apart: each is read and conjoined, so an instance is held to both.
func TestAdmissionReader_AnEnumAndAConstAreTwoSets(t *testing.T) {
	t.Parallel()
	lowerer, _ := LoweredFor(t, openapitest.ComponentSpecVer("3.1.0", "    E: {type: string, enum: [a, b], const: a}\n"))
	admissionReader := newAdmissionReader(lowerer.Ctx)
	assert.Equal(t, map[string]kindSet{"string:a": kindString}, admissionReader.written(StatedIn(lowerer, componentSchema(t, lowerer, "E")), admissionReader.nullable).values)
}

// TestAdmissionReader_ANullableBesideARefIsReadInItsDialect pins a $ref's siblings:
// applied as 3.1 applies them and read as their document's dialect defines
// them, so a 3.0 `nullable` there adds null to the type beside it, and a 3.1
// one, which that dialect does not define, adds nothing.
func TestAdmissionReader_ANullableBesideARefIsReadInItsDialect(t *testing.T) {
	t.Parallel()
	for version, want := range map[string]kindSet{"3.0.3": kindString | kindNull, "3.1.0": kindString} {
		lowerer, _ := LoweredFor(t, openapitest.ComponentSpecVer(version, `    S: {type: string}
    T: {$ref: '#/components/schemas/S', type: string, nullable: true}
`))
		admissionReader := newAdmissionReader(lowerer.Ctx)
		assert.Equal(t, want, admissionReader.written(StatedIn(lowerer, componentSchema(t, lowerer, "T")), admissionReader.nullable).kinds, version)
	}
}

// TestAdmissionReader_ASchemaIsWithinItself pins the comparison every election makes of
// a conjunct with itself, which needs no rendering: rendering costs the whole
// `items` subtree at each level, and a value that will not render shows it ran.
func TestAdmissionReader_ASchemaIsWithinItself(t *testing.T) {
	t.Parallel()
	lowerer, _ := LoweredFor(t, openapitest.ComponentSpec("    E: {type: array, items: {type: string, x-t: .nan}}\n"))
	admissionReader := newAdmissionReader(lowerer.Ctx)
	read := admissionReader.written(StatedIn(lowerer, componentSchema(t, lowerer, "E")), admissionReader.nullable)
	assert.True(t, admissionReader.within(read, read))
}

// TestAdmissionReader_ARememberedSetStillSpends pins why a remembered set costs its
// budget: a read's verdict turns on what it spent, so a set a first reader
// read must cut a later one exactly as reading it would, or which position
// lowered first would decide what another lowers to.
func TestAdmissionReader_ARememberedSetStillSpends(t *testing.T) {
	t.Parallel()
	lowerer, _ := LoweredFor(t, openapitest.ComponentSpec("    E: {type: string, enum: [a, b]}\n"))
	s := componentSchema(t, lowerer, "E")
	first := newAdmissionReader(lowerer.Ctx)
	first.written(StatedIn(lowerer, s), first.nullable)
	require.False(t, first.incomplete())

	short := newAdmissionReader(lowerer.Ctx)
	short.members = budget.New(budget.AtMost(1))
	assert.Empty(t, short.written(StatedIn(lowerer, s), short.nullable).values, "a set past the budget is not read at all")
	assert.True(t, short.incomplete())
}

// TestAdmissionReader_AnUnreadableSetIsRememberedAsUnread pins that a set no reader could
// read cuts every read of it, the first and those after.
func TestAdmissionReader_AnUnreadableSetIsRememberedAsUnread(t *testing.T) {
	t.Parallel()
	lowerer, _ := LoweredFor(t, openapitest.ComponentSpec("    E: {type: string, enum: [a, b]}\n"))
	s := componentSchema(t, lowerer, "E")
	s.GetEnum()[0].Kind = 0
	for range 2 {
		admissionReader := newAdmissionReader(lowerer.Ctx)
		admissionReader.written(StatedIn(lowerer, s), admissionReader.nullable)
		assert.True(t, admissionReader.incomplete())
	}
}

// TestElect_WalksOnlyTheSetItNarrows pins what electing a narrowing costs: it
// looks each of the narrowing's members up in what it narrows, and no more. A set
// compared with itself, or a larger set searched for one a smaller lacks, walked
// the larger set, so each position narrowing a long enum paid for every member.
func TestElect_WalksOnlyTheSetItNarrows(t *testing.T) {
	t.Parallel()
	walked := func(members, narrowing int) int {
		var target, narrowed strings.Builder
		for i := range members {
			fmt.Fprintf(&target, "v%d, ", i)
		}
		for i := range narrowing {
			fmt.Fprintf(&narrowed, "v%d, ", i)
		}
		lowerer, _ := LoweredFor(t, openapitest.ComponentSpec("    En: {type: string, enum: ["+target.String()+"]}\n"+
			"    T: {allOf: [{$ref: '#/components/schemas/En'}, {enum: ["+narrowed.String()+"]}]}\n"))
		s := componentSchema(t, lowerer, "T")
		admissionReader := newAdmissionReader(lowerer.Ctx)
		total := 0
		admissionReader.walked = func(n int) { total += n }
		conjuncts, _ := admissionReader.conjuncts(StatedIn(lowerer, s), allOfBranches(lowerer.Ctx, StatedIn(lowerer, s), ""))
		principal := admissionReader.chooseConjunctToLowerFrom(conjuncts, []*conjunct{&conjuncts[1]})
		require.NotNil(t, principal.branch)
		require.Nil(t, statement.ParsedAt(principal.branch.schema).GetSchema().Ref, "the narrowing is elected")
		return total
	}
	assert.Equal(t, 1, walked(1000, 1))
	assert.Equal(t, 1, walked(4000, 1), "a longer target walks no more")
	assert.Equal(t, 999, walked(1000, 999), "a narrowing walks its own members")
}

// TestElect_ComparesEachPairOnce pins what an election of many conjuncts
// tying with each other costs: one comparison of each conjunct naming a kind
// with every conjunct (mostWithin), and breaking the tie at most one more per
// conjunct. Unions of one keyword and words admitting alike are the two ties
// that read every tied conjunct.
func TestElect_ComparesEachPairOnce(t *testing.T) {
	t.Parallel()
	for _, entry := range []string{
		"{oneOf: [{type: string, maxLength: %d}, {type: integer}]}",
		"{type: string, maxLength: %d}",
	} {
		for _, n := range []int{16, 32, 64} {
			entries := make([]string, n)
			for i := range n {
				entries[i] = fmt.Sprintf(entry, i+1)
			}
			lowerer, _ := LoweredFor(t, openapitest.ComponentSpec("    T: {allOf: ["+strings.Join(entries, ", ")+"]}\n"))
			schema := StatedIn(lowerer, componentSchema(t, lowerer, "T"))
			admissionReader := newAdmissionReader(lowerer.Ctx)
			conjuncts, _ := admissionReader.conjuncts(schema, allOfBranches(lowerer.Ctx, schema, ""))
			compared := 0
			admissionReader.compared = func() { compared++ }
			admissionReader.chooseConjunctToLowerFrom(conjuncts, nil)
			assert.LessOrEqual(t, compared, n*len(conjuncts)+2*n, "%s at %d", entry, n)
			assert.GreaterOrEqual(t, compared, n*len(conjuncts), "%s at %d: every pair is compared", entry, n)
		}
	}
}
