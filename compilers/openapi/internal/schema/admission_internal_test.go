// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"bytes"
	"encoding/json/jsontext"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// formerWhole is the read admissionOf replaced, kept to hold it to: recursive,
// it reads js afresh at each ask, spending w's budget per schema, and is cut
// short where the budget runs out, at a $dynamicRef the bindings read as no
// $ref and at a $ref landing on no schema.
func (w *admissionReader) formerWhole(js *statement.Statement) admitted {
	if !w.reads.Spend(1) || js.Truth == statement.Unwritten {
		return admitted{kinds: kindAny}
	}
	if js.Truth != statement.Stated {
		if js.Truth.ReadsFalse() {
			return admitted{}
		}
		return admitted{kinds: kindAny}
	}
	if declaresDynamicRef(js.Schema) && js.Ref == nil {
		w.reads.Exhaust()
	}
	out := w.written(js.Schema, definesNullableAt(w.c, js))
	for _, b := range conjunctsIn(w.c, js.Schema) {
		out = out.and(w.formerWhole(b))
	}
	choice := js.ChoiceKeywords()
	for _, branches := range [][]statement.Subschema{choice.OneOf, choice.AnyOf} {
		if len(branches) == 0 {
			continue
		}
		union := w.formerWhole(w.c.Statements().Subschema(branches[0]))
		for _, b := range branches[1:] {
			union = union.or(w.formerWhole(w.c.Statements().Subschema(b)))
		}
		out = out.and(union)
	}
	if js.Ref == nil {
		return out
	}
	next, _ := declaredOf(w.c, js)
	if next == nil {
		w.reads.Exhaust()
		return out
	}
	return out.and(w.formerWhole(next))
}

// specsUnder returns the OpenAPI 3 documents under the repository's testdata,
// by path, each as its bytes.
func specsUnder(t *testing.T) map[string][]byte {
	t.Helper()
	root := filepath.Join(irtest.RepoRoot(t), "testdata")
	out := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".json") {
			return err
		}
		src, err := os.ReadFile(path)
		if err == nil && (bytes.Contains(src, []byte("openapi: 3")) || bytes.Contains(src, []byte(`"openapi": "3`))) {
			out[path] = src
		}
		return err
	})
	require.NoError(t, err)
	return out
}

// loweredOrNot returns the fixture over src, or nil where it does not load.
func loweredOrNot(t *testing.T, src []byte) *Lowerer {
	t.Helper()
	loaded, _, err := load.Load(t.Context(), 0, openapitest.SourceOf(string(src)), load.Options{})
	if err != nil || loaded == nil {
		return nil
	}
	return LowererOver(service.New(0, loaded, service.Policy{}))
}

// statementsOfDocument calls visit with the statement of each schema the
// root document of l writes, wherever it is written.
func statementsOfDocument(l *Lowerer, visit func(at jsontext.Pointer, st *statement.Statement)) {
	var walk func(n *yaml.Node, at jsontext.Pointer, depth int)
	walk = func(n *yaml.Node, at jsontext.Pointer, depth int) {
		if n == nil || depth > 512 {
			return
		}
		if st := l.Ctx.Statements().At(bind.DocumentPointer{Doc: bind.Source, Pointer: at}); st.Held() {
			visit(at, st)
		}
		switch n.Kind {
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				walk(n.Content[i+1], at.AppendToken(n.Content[i].Value), depth+1)
			}
		case yaml.SequenceNode:
			for i, c := range n.Content {
				walk(c, at.AppendToken(strconv.Itoa(i)), depth+1)
			}
		default:
		}
	}
	walk(nodeview.DocumentRoot(l.Doc.GetCore().GetRootNode()), "", 0)
}

// TestAdmissionOf_AgreesWithAReadOfItsOwn holds what admissionOf remembers of
// every schema the documents under testdata write to what a read of that
// schema alone reaches, wherever neither is cut short: a remembered answer is
// the one its reader would have read.
func TestAdmissionOf_AgreesWithAReadOfItsOwn(t *testing.T) {
	t.Parallel()
	compared := 0
	for path, src := range specsUnder(t) {
		l := loweredOrNot(t, src)
		if l == nil {
			continue
		}
		statementsOfDocument(l, func(at jsontext.Pointer, st *statement.Statement) {
			r := admissionOf(l.Ctx, st)
			former := newAdmissionReader(l.Ctx)
			want := former.formerWhole(st)
			if r.cut || r.reads > maxAdmissionReads || r.members > maxAdmissionMembers || former.incomplete() {
				return
			}
			compared++
			assert.Equal(t, fmt.Sprint(want), fmt.Sprint(r.admitted), "%s#%s", path, at)
		})
	}
	assert.Greater(t, compared, 1000, "the corpus reaches what it compares")
}

// TestAdmissionOf_ACycleReadsAlikeWhicheverStatementIsAskedFirst holds the
// reading of every schema of a cycle, and of one reaching it and one beside
// it, to one answer whichever is asked first: a read is never remembered from
// inside an open cycle.
func TestAdmissionOf_ACycleReadsAlikeWhicheverStatementIsAskedFirst(t *testing.T) {
	t.Parallel()
	const n = 64
	spec := openapitest.ComponentSpecVer("3.1.0", clique(n)+
		"    Reaches: {allOf: [{$ref: '#/components/schemas/K7'}, {type: object}]}\n"+
		"    Beside: {allOf: [{type: string}, {minLength: 1}]}\n")
	names := make([]string, 0, 2+n)
	names = append(names, "Reaches", "Beside")
	for i := range n {
		names = append(names, fmt.Sprintf("K%d", i))
	}
	readAll := func(first string) map[string]string {
		l, _ := LoweredFor(t, spec)
		statementOf := func(name string) *statement.Statement {
			return l.Ctx.Statements().Of(l.Doc.Components.Schemas.GetOrZero(name))
		}
		admissionOf(l.Ctx, statementOf(first))
		out := map[string]string{}
		for _, name := range names {
			out[name] = fmt.Sprint(*admissionOf(l.Ctx, statementOf(name)))
		}
		return out
	}
	want := readAll("Beside")
	assert.Contains(t, want["Reaches"], "true", "a schema reaching the cycle is cut")
	for _, first := range []string{"Reaches", "K0", "K31", "K63"} {
		assert.Equal(t, want, readAll(first), "asked %s first", first)
	}
}

// TestAdmissionOf_VisitsEachStatementAndEdgeOnce holds a run's walks to one
// visit of each statement they settle and one of each of its conjunct edges,
// on a cycle and on a chain each read at three sizes, so the cost of the read
// grows with the document and not with its paths.
func TestAdmissionOf_VisitsEachStatementAndEdgeOnce(t *testing.T) {
	t.Parallel()
	chain := func(n int) string {
		var b strings.Builder
		b.WriteString("    C0: {type: string}\n")
		for i := 1; i < n; i++ {
			fmt.Fprintf(&b, "    C%d: {allOf: [{$ref: '#/components/schemas/C%d'}, {$ref: '#/components/schemas/C%d'}]}\n",
				i, i-1, max(0, i-2))
		}
		return b.String()
	}
	for _, tc := range []struct {
		name  string
		shape func(int) string
	}{{"clique", clique}, {"chain", chain}} {
		for _, n := range []int{16, 32, 64} {
			l, _ := LoweredFor(t, openapitest.ComponentSpecVer("3.1.0", tc.shape(n)))
			for _, js := range l.Doc.Components.Schemas.All() {
				admissionOf(l.Ctx, l.Ctx.Statements().Of(js))
			}
			store := admissionStore(l.Ctx)
			edges := 0
			for st := range store.read {
				edges += len(conjunctsOf(l.Ctx, st))
			}
			assert.Equal(t, len(store.read), store.visits, "%s of %d: each statement once", tc.name, n)
			assert.Equal(t, edges, store.edges, "%s of %d: each edge once", tc.name, n)
		}
	}
}

// diamonds is a ladder of n schemas each combining, under comb, two schemas
// that each combine the next and write a format and a value set of their own,
// so the paths to the last schema double at each step.
func diamonds(comb string, n int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "    D%d: {type: string, format: leaf, enum: [z]}\n", n)
	for i := range n {
		side := func(tag string) string {
			return fmt.Sprintf("{%s: [{$ref: '#/components/schemas/D%d'}], type: string, format: f%s%d, enum: [z, %s%d]}",
				comb, i+1, tag, i, tag, i)
		}
		fmt.Fprintf(&b, "    D%d: {%s: [%s, %s]}\n", i, comb, side("a"), side("b"))
	}
	return b.String()
}

// TestAdmissionOf_MergesWhatGrowsWithTheDocument holds the formats and value-
// set members every reading of a run holds to growth linear in the document,
// on ladders whose paths double at each step: a read past its caps stops
// conjoining, so what each schema merges is bounded however many paths reach
// it.
func TestAdmissionOf_MergesWhatGrowsWithTheDocument(t *testing.T) {
	t.Parallel()
	for _, comb := range []string{"allOf", "oneOf", "anyOf"} {
		merged := func(n int) int {
			l, diags := LoweredFor(t, openapitest.ComponentSpecVer("3.1.0", diamonds(comb, n)))
			require.Empty(t, diags)
			admissionOf(l.Ctx, l.Ctx.Statements().Of(l.Doc.Components.Schemas.GetOrZero("D0")))
			return admissionStore(l.Ctx).merged
		}
		at4, at8, at16 := merged(4), merged(8), merged(16)
		assert.LessOrEqual(t, at16-at8, 5*(at8-at4)/2, "%s: merged %d, %d, %d at 4, 8, 16", comb, at4, at8, at16)
	}
}

// TestInstanceKinds_ReadsEachKindOfInstance pins the kinds each kind of JSON
// instance a merge asks of a structure's source is (modelFields.Admits): an
// integer apart from a number, which holds it, and none for the null, which
// the nulls walk reads.
func TestInstanceKinds_ReadsEachKindOfInstance(t *testing.T) {
	t.Parallel()
	for kind, want := range map[merge.JSONInstanceKind]kindSet{
		merge.InstanceString: kindString, merge.InstanceInteger: kindInteger, merge.InstanceNumber: kindNumber,
		merge.InstanceBoolean: kindBoolean, merge.InstanceArray: kindArray, merge.InstanceObject: kindObject,
		merge.InstanceNull: 0,
	} {
		assert.Equal(t, want, instanceKinds(kind), "%d", kind)
	}
}

// TestAdmission_TakesItsNullFromTheNullsWalk holds the null a conjunct admits,
// as a disjoint report reads it (empty), to the nulls walk's verdict on every
// schema the documents under testdata write and on each spelling of a
// `nullable` beside a `type`, at every version, read where the IR holds it, a
// union's variants among it (mayAdmitNull): the admission reads its kinds
// with null aside and derives null no second way. A null read past its budget
// is undecided and set aside.
func TestAdmission_TakesItsNullFromTheNullsWalk(t *testing.T) {
	t.Parallel()
	specs := specsUnder(t)
	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		for _, value := range []string{`"yes"`, `"true"`, "1", "null", "[true]", "yes", "true", "True", "false"} {
			specs[version+" nullable: "+value] = []byte(openapitest.HolderSpec(version,
				"{type: ['null'], nullable: "+value+"}", "    N: {allOf: [{type: 'null', nullable: "+value+"}]}\n"))
		}
	}
	compared := 0
	for path, src := range specs {
		l := loweredOrNot(t, src)
		if l == nil {
			continue
		}
		statementsOfDocument(l, func(at jsontext.Pointer, st *statement.Statement) {
			n := newNulls(l.Ctx)
			n.variants = true
			want := n.conjunct(st, nullSite{}).admits(l.Ctx)
			if n.conjuncts.Exhausted() {
				return
			}
			compared++
			assert.Equal(t, want, !empty(l.Ctx, conjunctAtom{st: st}), "%s#%s", path, at)
		})
	}
	assert.Greater(t, compared, 1000, "the corpus reaches what it compares")
}

// TestVariantHolders_ClimbsEverySchemaHoldingAUnionBranch pins which
// statements a node written at a position is read with (modelFields.Admits):
// a union's branch with the schema writing the union and each schema holding
// that one as an allOf entry or a union branch, and any other position alone:
// an allOf entry, an entry of another keyword, a pointer naming an entry its
// holder lacks, and a statement read elsewhere.
func TestVariantHolders_ClimbsEverySchemaHoldingAUnionBranch(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpecVer("3.1.0", `    U: {type: object, anyOf: [{type: object}, {type: array}]}
    O: {oneOf: [{type: string}]}
    N: {type: object, allOf: [{allOf: [{oneOf: [{anyOf: [{type: string}]}]}]}]}
    T: {prefixItems: [{type: string}]}
`))
	at := func(p string) bind.DocumentPointer {
		return bind.DocumentPointer{Doc: bind.Source, Pointer: jsontext.Pointer(p)}
	}
	for _, tc := range []struct {
		from    string
		holders []string
	}{
		{"/components/schemas/U/anyOf/1", []string{"/components/schemas/U"}},
		{"/components/schemas/O/oneOf/0", []string{"/components/schemas/O"}},
		{"/components/schemas/N/allOf/0/allOf/0/oneOf/0/anyOf/0", []string{
			"/components/schemas/N/allOf/0/allOf/0/oneOf/0", "/components/schemas/N/allOf/0/allOf/0",
			"/components/schemas/N/allOf/0", "/components/schemas/N",
		}},
		{"/components/schemas/N/allOf/0", nil},
		{"/components/schemas/T/prefixItems/0", nil},
		{"/components/schemas/U/anyOf/2", nil},
		{"/components/schemas/U/oneOf/0", nil},
		{"/components/schemas/U", nil},
	} {
		want := []*statement.Statement{l.Ctx.Statements().At(at(tc.from))}
		for _, h := range tc.holders {
			want = append(want, l.Ctx.Statements().At(at(h)))
		}
		got := variantHolders(l.Ctx, at(tc.from), want[0])
		require.Len(t, got, len(want), tc.from)
		for i := range want {
			assert.Same(t, want[i], got[i], "%s: holder %d", tc.from, i)
		}
	}
	other := l.Ctx.Statements().At(at("/components/schemas/O/oneOf/0"))
	assert.Len(t, variantHolders(l.Ctx, at("/components/schemas/U/anyOf/0"), other), 1,
		"a statement read elsewhere is no branch of the union")
}

// loweredKinds returns the kinds of instance the node id names admits, past
// each alias, null aside: a primitive's, a list's, a model's.
func loweredKinds(t *testing.T, doc *ir.Document, id ir.TypeID) kindSet {
	t.Helper()
	for range 8 {
		switch td := doc.Types[id].(type) {
		case *ir.Scalar:
			require.NotNil(t, td.Base, "%s is an alias", id)
			id = td.Base.Target
		case *ir.Primitive:
			return map[ir.PrimKind]kindSet{ir.PrimString: kindString, ir.PrimAny: anyInstance}[td.Prim]
		case *ir.List:
			return kindArray
		case *ir.Model:
			return kindObject
		default:
			t.Fatalf("%s lowers to %T", id, td)
		}
	}
	t.Fatalf("%s is an alias of an alias past the walk", id)
	return 0
}

// TestAdmissionOf_KindsAreWhatEachConjunctionLowersTo holds the kinds the
// run's admission reads a conjunction as to the node it lowers to, where a
// shape keyword's claim meets a declaration: a declared kind beside an entry
// or a $ref target the keywords claim another kind of, a use of an inferred
// kind, and compositions over claims. Each is one kind, in both entry orders.
func TestAdmissionOf_KindsAreWhatEachConjunctionLowersTo(t *testing.T) {
	t.Parallel()
	const items, ref = "{items: {type: string}}", "{$ref: '#/components/schemas/E'}"
	rows := map[string]struct {
		a, b  string
		kinds kindSet
	}{
		"BesideAnEntry":  {"{type: string}", items, kindString},
		"BesideARef":     {"{type: string}", ref, kindString},
		"BesideAModelOf": {"{type: string}", "{$ref: '#/components/schemas/P'}", kindString},
		"AUse":           {ref, "{minItems: 1}", kindArray},
		"ATypedUse":      {ref, "{type: array, minItems: 1}", kindArray},
		"Declared":       {"{$ref: '#/components/schemas/P'}", "{type: object}", kindObject},
		"TwoRefs":        {"{$ref: '#/components/schemas/P'}", "{$ref: '#/components/schemas/Q'}", kindObject},
		"Required":       {"{properties: {a: {type: string}}}", "{required: [a]}", kindObject},
	}
	for _, swap := range []bool{false, true} {
		schemas := "    E: " + items + "\n    P: {properties: {a: {type: string}}}\n    Q: {properties: {b: {type: string}}}\n" +
			"    Out: {$ref: '#/components/schemas/BesideAnEntry/allOf/1'}\n"
		for name, row := range rows {
			a, b := row.a, row.b
			if swap {
				a, b = b, a
			}
			schemas += "    " + name + ": {allOf: [" + a + ", " + b + "]}\n"
		}
		l, _ := LoweredWith(t, openapitest.ComponentSpecVer("3.1.0", schemas), lowering.Policy{})
		l.Diags.AppendAll(LowerComponentSchemas(t.Context(), l.Ctx, l.Types, l.Schemas()))
		doc := l.Document(t)
		for name, st := range l.Schemas() {
			r := admissionOf(l.Ctx, st)
			assert.Equal(t, r.kinds, loweredKinds(t, doc, ir.TypeID("t/openapi/components/schemas/"+name)), "%s swapped=%t", name, swap)
			if row, ok := rows[name]; ok {
				assert.Equal(t, row.kinds, r.kinds, "%s swapped=%t", name, swap)
			}
		}
	}
}

// TestAdmissionOf_PartialIsWhatAKeywordRefusesInPart holds the kinds a
// statement admits only in part to the keywords that refuse some instance of
// them, read through its conjuncts and unions: a bound, a value set, a format,
// a keyword of arrays or objects alone, and a oneOf whose branches share a
// kind, while a type, a redaction, a keyword written null, a schema applied to
// parts that states nothing and a `then` without an `if` leave every kind they
// admit whole.
func TestAdmissionOf_PartialIsWhatAKeywordRefusesInPart(t *testing.T) {
	t.Parallel()
	rows := map[string]struct {
		schema  string
		partial kindSet
	}{
		"Int":        {"{type: integer}", 0},
		"Min":        {"{type: integer, minimum: 5}", kindInteger},
		"UntypedMin": {"{minimum: 5}", kindNumber},
		"Enum":       {"{enum: [1, a]}", kindInteger | kindString},
		"Pattern":    {"{type: string, pattern: '^a'}", kindString},
		"Format":     {"{format: int32}", kindString | kindNumber},
		"Redaction":  {"{type: string, format: password}", 0},
		"Items":      {"{items: {type: string}}", kindArray},
		"AnyItems":   {"{type: array, items: {}}", 0},
		"TrueItems":  {"{type: array, items: true, unevaluatedItems: true}", 0},
		"AnyPrefix":  {"{prefixItems: [{}, true]}", 0},
		"Prefix":     {"{prefixItems: [{}, {type: string}]}", kindArray},
		"Contains":   {"{contains: {}}", kindArray},
		"NullItems":  {"{items: null}", 0},
		"Props":      {"{properties: {a: {type: string}}}", kindObject},
		"AnyProps":   {"{properties: {a: {}}, patternProperties: {b: true}, propertyNames: {}}", 0},
		"OpenProps":  {"{additionalProperties: true, unevaluatedProperties: {}}", 0},
		"Closed":     {"{additionalProperties: false}", kindObject},
		"Required":   {"{required: [a]}", kindObject},
		"Dependent":  {"{dependentSchemas: {a: {}}}", kindObject},
		"Then":       {"{then: {type: string}}", 0},
		"AnyOfWhole": {"{anyOf: [{type: integer}, {type: integer, minimum: 5}]}", 0},
		"AnyOfPart":  {"{anyOf: [{type: object}, {type: integer, minimum: 5}]}", kindInteger},
		"OneOfApart": {"{oneOf: [{type: object}, {type: integer}]}", 0},
		"OneOfBoth":  {"{oneOf: [{type: integer}, {type: integer, minimum: 5}]}", kindInteger},
		"AllOf":      {"{allOf: [{type: integer}, {$ref: '#/components/schemas/UntypedMin'}]}", kindInteger},
		"Ref":        {"{$ref: '#/components/schemas/Min'}", kindInteger},
	}
	var schemas strings.Builder
	for name, row := range rows {
		schemas.WriteString("    " + name + ": " + row.schema + "\n")
	}
	l, _ := LoweredFor(t, openapitest.ComponentSpecVer("3.1.0", schemas.String()))
	for name, row := range rows {
		st := l.Ctx.Statements().At(bind.DocumentPointer{Doc: bind.Source, Pointer: jsontext.Pointer("/components/schemas/" + name)})
		r := admissionOf(l.Ctx, st)
		assert.Equal(t, row.partial, r.partial&r.admits, "%s: %s", name, row.schema)
	}
}

// fanout writes n statements, each a oneOf of the one value set of n members
// and a set of a member of its own, as an allOf entry.
func fanout(n int) string {
	var b strings.Builder
	b.WriteString("    Base: {enum: [")
	for i := range n {
		fmt.Fprintf(&b, "b%d, ", i)
	}
	b.WriteString("b]}\n")
	for i := range n {
		fmt.Fprintf(&b, "    S%d: {allOf: [{oneOf: [{$ref: '#/components/schemas/Base'}, {enum: [x%d]}]}]}\n", i, i)
	}
	return b.String()
}

// heldMembers is how many value-set members the run's readings hold, each set
// counted once however many readings share it.
func heldMembers(store *admissions) int {
	seen := map[uintptr]bool{}
	held := 0
	count := func(set map[string]kindSet) {
		if at := reflect.ValueOf(set).Pointer(); set != nil && !seen[at] {
			seen[at] = true
			held += len(set)
		}
	}
	for _, r := range store.read {
		count(r.admitted.values)
		for _, set := range r.admitted.joined {
			count(set)
		}
	}
	return held
}

// TestAdmissionOf_AUnionSharesTheSetsItJoins holds the value-set members a
// run's readings hold to growth linear in the document where n statements each
// join one set of n members with one of their own: a union holds its
// branches' sets, which copying them into one would make quadratic.
func TestAdmissionOf_AUnionSharesTheSetsItJoins(t *testing.T) {
	t.Parallel()
	held := func(n int) int {
		l, diags := LoweredFor(t, openapitest.ComponentSpecVer("3.1.0", fanout(n)))
		require.Empty(t, diags)
		for name, st := range l.Schemas() {
			require.True(t, admissionOf(l.Ctx, st).admitted.fixed, name)
		}
		return heldMembers(admissionStore(l.Ctx))
	}
	at8, at16, at32 := held(8), held(16), held(32)
	assert.LessOrEqual(t, at32-at16, 5*(at16-at8)/2, "held %d, %d, %d at 8, 16, 32", at8, at16, at32)
}

// TestAdmitted_AUnionHoldsEveryMemberOfTheSetsItJoins holds a union's value
// set to the members of each set it joins, read in place: a member of any of
// them is one, what it shares with another set is what any of them shares, and
// a member two of them hold is one value, so a union holding it twice is
// within a set holding it once.
func TestAdmitted_AUnionHoldsEveryMemberOfTheSetsItJoins(t *testing.T) {
	t.Parallel()
	set := func(keys ...string) admitted {
		values := map[string]kindSet{}
		for _, k := range keys {
			values[k] = kindString
		}
		return admitted{kinds: kindString, fixed: true, values: values}
	}
	union := set("a").or(set("b")).or(set("c"))
	for key, want := range map[string]bool{"a": true, "b": true, "c": true, "d": false} {
		assert.Equal(t, want, union.hasMember(key), key)
	}
	assert.Equal(t, 3, union.size())
	assert.True(t, union.reachable())
	assert.Equal(t, map[string]kindSet{"b": kindString, "c": kindString}, union.and(set("b", "c", "d")).values)
	assert.Equal(t, map[string]kindSet{"c": kindString}, set("c", "x", "y", "z").and(union).values)
	assert.True(t, set("c").valuesWithin(union))
	assert.False(t, union.valuesWithin(set("a", "b")))
	assert.True(t, union.valuesWithin(set("a", "b", "c")))
	assert.True(t, set("a").or(set("a")).valuesWithin(set("a")), "a member two branches hold is one value")
	overlap := set("a", "b").or(set("b", "c")).or(set("c", "d", "e"))
	assert.Equal(t, 5, overlap.size(), "a member two branches hold is held once")
	assert.False(t, overlap.valuesWithin(set("a", "b", "c", "d")), "a larger set is no narrowing of a smaller")
	assert.True(t, overlap.valuesWithin(set("a", "b", "c", "d", "e")))
	assert.Equal(t, 5, set("y", "p", "q", "r").or(set("x").or(set("y"))).size(),
		"a member a joined set of the smaller holds is held once")
}

// TestAdmissionOf_AUnionHoldsFewSetsApart holds the sets a union of k
// branches, each naming a set of its own, holds apart to maxJoinedSets beside
// its first at k, 4k and 16k, so asking whether it holds a member reads a
// bounded number of maps whatever its width, and its members to every
// branch's.
func TestAdmissionOf_AUnionHoldsFewSetsApart(t *testing.T) {
	t.Parallel()
	for _, k := range []int{4, 16, 64} {
		var b strings.Builder
		b.WriteString("    U: {oneOf: [")
		for j := range k {
			fmt.Fprintf(&b, "{$ref: '#/components/schemas/E%d'}, ", j)
		}
		b.WriteString("]}\n")
		for j := range k {
			fmt.Fprintf(&b, "    E%d: {enum: [a%d, b%d, c]}\n", j, j, j)
		}
		l, _ := LoweredFor(t, openapitest.ComponentSpecVer("3.1.0", b.String()))
		at := bind.DocumentPointer{Doc: bind.Source, Pointer: "/components/schemas/U"}
		union := admissionOf(l.Ctx, l.Ctx.Statements().At(at)).admitted
		assert.LessOrEqual(t, len(union.joined), maxJoinedSets, "a union of %d", k)
		assert.Equal(t, 2*k+1, union.size(), "a union of %d holds each member once", k)
		for _, r := range admissionStore(l.Ctx).read {
			assert.LessOrEqual(t, len(r.admitted.joined), maxJoinedSets, "a union of %d", k)
		}
	}
}
