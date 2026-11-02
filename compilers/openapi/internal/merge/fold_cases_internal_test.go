// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// TestFold_AKeywordNoOneValueStatesIsKeptAndReported pins the second of the two
// conflict routes. Two declarations that agree on type but pin a keyword with no
// order to different values fold to the first one's, so the other, which may be
// the stricter, is discarded and must be announced.
func TestFold_AKeywordNoOneValueStatesIsKeptAndReported(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(nil)
	dst := ir.Property{
		WireName:    "age",
		Provenance:  ir.Provenance{Pointer: "/components/schemas/A/properties/age"},
		Constraints: &ir.Constraints{Pattern: "^a$"},
	}
	src := ir.Property{
		WireName:    "age",
		Provenance:  ptrAt("/components/schemas/B/properties/age"),
		Constraints: &ir.Constraints{Pattern: "^b$"},
	}

	dst = foldTwo(g, dst, src, written(`{"pattern":"^b$"}`))

	require.Len(t, *recorded, 1)
	assert.Equal(t, "^a$", dst.Constraints.Pattern, "the first pattern is kept")
	assert.Equal(t, ir.SeverityWarning, (*recorded)[0].Severity)
	assert.Equal(t, diag.ConflictingRedecl, (*recorded)[0].Code)
	assert.Contains(t, (*recorded)[0].Message, `conflicting pattern ("^a$" and "^b$")`)
	assert.Contains(t, (*recorded)[0].Message, "/components/schemas/A/properties/age",
		"the kept declaration is named")
	assert.Contains(t, (*recorded)[0].Message, "/components/schemas/B/properties/age",
		"and so is the discarded one")
}

// TestResolvePrimKind_EnumResolvesThroughItsValueType pins the enum case of the
// resolution walk. An enum member can itself be a scalar of the kind being
// redeclared, so it must answer with its value type rather than staying
// unresolved and falling through to the structural comparison.
func TestResolvePrimKind_EnumResolvesThroughItsValueType(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/enum": &ir.Enum{ID: "t/enum", ValueType: ir.PrimString},
		"t/alias": &ir.Scalar{
			ID:   "t/alias",
			Base: &ir.TypeRef{Target: "t/enum"},
		},
	})

	kind, ok := judge(g).primKind(ir.TypeRef{Target: "t/enum"})
	require.True(t, ok)
	assert.Equal(t, ir.PrimString, kind)

	kind, ok = judge(g).primKind(ir.TypeRef{Target: "t/alias"})
	require.True(t, ok, "a scalar's Base chain is followed to the enum behind it")
	assert.Equal(t, ir.PrimString, kind)
}

// TestFold_ALosingDetailIsKeptAndReported sweeps the fold's
// adopt-if-absent fields. Each of them took the first declaration's value and
// dropped a present, different value from the redeclaration with nothing said
// and nothing kept — a consumer diffing two revisions that change the second
// branch's default saw no change. Now the merge says which detail disagreed
// and keeps the losing declaration verbatim, under the same key a losing type
// is kept under, since it is the same event: a redeclaration the merge could
// not fold whole.
func TestFold_ALosingDetailIsKeptAndReported(t *testing.T) {
	t.Parallel()
	one, two := &ir.Value{Kind: ir.ValueNumber, Num: "1"}, &ir.Value{Kind: ir.ValueNumber, Num: "2"}
	cases := []struct {
		name     string
		dst, src ir.Property
		detail   string
	}{
		{"default", ir.Property{Default: one}, ir.Property{Default: two}, "default"},
		{"examples", ir.Property{Examples: []ir.Example{{Value: one}}}, ir.Property{Examples: []ir.Example{{Value: two}}}, "examples"},
		{"deprecation", ir.Property{Deprecation: &ir.Deprecation{Since: "1"}}, ir.Property{Deprecation: &ir.Deprecation{Since: "2"}}, "deprecation"},
		{"xml", ir.Property{XML: &ir.XMLHints{Name: "a"}}, ir.Property{XML: &ir.XMLHints{Name: "b"}}, "xml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g, recorded := stubMerger(map[ir.TypeID]ir.TypeDef{
				"t/int": &ir.Primitive{ID: "t/int", Prim: ir.PrimInt32},
			})
			dst, src := tc.dst, tc.src
			dst.WireName, src.WireName = "id", "id"
			dst.Type, src.Type = ir.TypeRef{Target: "t/int"}, ir.TypeRef{Target: "t/int"}
			src.Provenance = ir.Provenance{Source: 1, Pointer: "/allOf/1/properties/id"}
			want := tc.dst

			dst = foldTwo(g, dst, src, written(`{"type":"integer","x":2}`))

			assert.Equal(t, want.Default, dst.Default, "the first declaration keeps its default")
			assert.Equal(t, want.Examples, dst.Examples, "and its examples")
			assert.Equal(t, want.Deprecation, dst.Deprecation, "and its deprecation")
			assert.Equal(t, want.XML, dst.XML, "and its xml hints")
			require.Len(t, *recorded, 1, "one detail disagreed, one diagnostic; got %v", *recorded)
			d := (*recorded)[0]
			assert.Equal(t, ir.SeverityInfo, d.Severity)
			assert.Equal(t, diag.DegradedConstruct, d.Code)
			assert.Equal(t, jsontext.Pointer("/allOf/1/properties/id"), d.Provenance.Pointer)
			assert.Contains(t, d.Message, `"id"`)
			assert.Contains(t, d.Message, tc.detail, "the message names the detail that disagreed")
			assert.Equal(t, ir.Unmodeled{
				losingDeclarationKey + "/allOf/1/properties/id": {
					Reason:     ir.ReasonDegradedLowering,
					Value:      ir.RawValue(`{"type":"integer","x":2}`),
					Provenance: ir.Provenance{Source: 1, Pointer: "/allOf/1/properties/id"},
				},
			}, dst.Unmodeled)
		})
	}
}

// TestFold_AnIdenticalDetailIsNotADisagreement is the other half:
// two branches writing the same default have not disagreed, and reporting or
// keeping one would be noise. The source is unread, which is the proof.
func TestFold_AnIdenticalDetailIsNotADisagreement(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/int": &ir.Primitive{ID: "t/int", Prim: ir.PrimInt32},
	})
	detail := func() ir.Property {
		return ir.Property{
			WireName:    "id",
			Type:        ir.TypeRef{Target: "t/int"},
			Default:     &ir.Value{Kind: ir.ValueNumber, Num: "1"},
			Examples:    []ir.Example{{Value: &ir.Value{Kind: ir.ValueNumber, Num: "1"}}},
			Deprecation: &ir.Deprecation{Since: "1"},
			XML:         &ir.XMLHints{Name: "a"},
			Provenance:  ptrAt("/allOf/1/properties/id"),
		}
	}
	dst := detail()

	dst = foldTwo(g, dst, detail(), unread(t))

	assert.Empty(t, *recorded)
	assert.Empty(t, dst.Unmodeled)
}

// TestFold_AnUnrenderableLoserIsAnError pins the one way keeping
// the declaration can fail. The merge has already said the redeclaration is
// kept, so a source that will not render is reported as an error — the
// construct reached the IR in no form — rather than left to the announcement.
func TestFold_AnUnrenderableLoserIsAnError(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/str": &ir.Primitive{ID: "t/str", Prim: ir.PrimString},
		"t/int": &ir.Primitive{ID: "t/int", Prim: ir.PrimInt32},
	})
	dst := ir.Property{WireName: "id", Type: ir.TypeRef{Target: "t/str"}}
	src := ir.Property{WireName: "id", Type: ir.TypeRef{Target: "t/int"}, Provenance: ptrAt("/b")}

	dst = foldTwo(g, dst, src, func() (ir.RawValue, error) { return nil, errors.New("no such node") })

	require.Len(t, *recorded, 2, "the conflict, and then the failure to keep it; got %v", *recorded)
	assert.Equal(t, diag.ConflictingRedecl, (*recorded)[0].Code)
	assert.Equal(t, diag.UnpreservableConstruct, (*recorded)[1].Code)
	assert.Equal(t, ir.SeverityError, (*recorded)[1].Severity)
	assert.Empty(t, dst.Unmodeled, "nothing claims a preservation that did not happen")
}

// narrowingRegistry holds one bare primitive per kind the narrowing tests pair.
func narrowingRegistry() map[ir.TypeID]ir.TypeDef {
	reg := map[ir.TypeID]ir.TypeDef{}
	for _, k := range []ir.PrimKind{
		ir.PrimNumber, ir.PrimInteger, ir.PrimInt32, ir.PrimInt64, ir.PrimString, ir.PrimDatetimeOffset,
		ir.PrimURL, ir.PrimFloat64, ir.PrimUUID, ir.PrimUint32, ir.PrimBytes,
		ir.PrimUint8, ir.PrimInt16, ir.PrimAny, ir.PrimBool, ir.PrimFloat32, ir.PrimTime, ir.PrimDatetime,
	} {
		id := ir.TypeID("t/prim/" + string(k))
		reg[id] = &ir.Primitive{ID: id, Prim: k}
	}
	return reg
}

func primRef(k ir.PrimKind) ir.TypeRef { return ir.TypeRef{Target: ir.TypeID("t/prim/" + string(k))} }

// TestFold_AnInadmissibleValueIsKeptNotAdopted pins that no
// declaration of a fold lands a default or example the merged field does not
// admit: the field carries no such value, and one info and one Unmodeled entry
// sit at each declaration that wrote it, identical whichever comes first. The
// table holds the broad side's kind refusals (a fractional default, an
// out-of-range default, a non-UUID example) and a null the merged field no
// longer admits; the helpers add every writer, late writers, and a null on
// either side of a fold.
func TestFold_AnInadmissibleValueIsKeptNotAdopted(t *testing.T) {
	t.Parallel()
	const broadPtr, narrowPtr = "/allOf/0/properties/p", "/allOf/1/properties/p"
	half, big, hello := num("1.5"), num("5000000000"), str("hello")
	badURL, commaTime, offsetDate := str("mailto:a b"), str("23:59:59,5"), str("2024-01-01T00:00:00+24:00")
	null := ir.Value{Kind: ir.ValueNull}
	for _, tc := range []struct {
		name          string
		broad, narrow ir.PrimKind
		decorate      func(*ir.Property)
		keyword       string
		raw           string
	}{
		{"fractional default", ir.PrimNumber, ir.PrimInteger,
			func(p *ir.Property) { p.Default = &half; p.Examples = []ir.Example{{Value: &half}} }, "default",
			`{"type":"number","default":1.5,"examples":[1.5]}`},
		{"out of range default", ir.PrimInt64, ir.PrimInt32,
			func(p *ir.Property) { p.Default = &big }, "default", `{"type":"integer","format":"int64","default":5000000000}`},
		{"non uuid example", ir.PrimString, ir.PrimUUID,
			func(p *ir.Property) { p.Examples = []ir.Example{{Value: &hello}} }, "examples", `{"type":"string","examples":["hello"]}`},
		{"non url default", ir.PrimString, ir.PrimURL,
			func(p *ir.Property) { p.Default = &badURL }, "default", `{"type":"string","default":"mailto:a b"}`},
		{"comma fraction time example", ir.PrimString, ir.PrimTime,
			func(p *ir.Property) { p.Examples = []ir.Example{{Value: &commaTime}} }, "examples",
			`{"type":"string","examples":["23:59:59,5"]}`},
		{"offset hour 24 date time example", ir.PrimString, ir.PrimDatetimeOffset,
			func(p *ir.Property) { p.Examples = []ir.Example{{Value: &offsetDate}} }, "examples",
			`{"type":"string","examples":["2024-01-01T00:00:00+24:00"]}`},
		{"null default", ir.PrimNumber, ir.PrimInteger,
			func(p *ir.Property) { p.Default = &null; p.Type.Nullable = true }, "default", `{"type":["number","null"],"default":null}`},
	} {
		var merged [2]ir.Property
		var diags [2][]ir.Diagnostic
		for i, broadFirst := range []bool{true, false} {
			reg := narrowingRegistry()
			g, recorded := stubMerger(reg)
			broad := ir.Property{WireName: "p", Type: primRef(tc.broad), Provenance: ptrAt(broadPtr)}
			tc.decorate(&broad)
			narrow := ir.Property{WireName: "p", Type: primRef(tc.narrow), Provenance: ptrAt(narrowPtr)}
			m := &ir.Model{}
			idx, settle := indexOver(m, g)
			first, second := broad, narrow
			firstSrc, secondSrc := written(tc.raw), unread(t)
			if !broadFirst {
				first, second = narrow, broad
				firstSrc, secondSrc = unread(t), written(tc.raw)
				first.Provenance, second.Provenance = ptrAt(narrowPtr), ptrAt(broadPtr)
			}
			idx.MergeProperty(first, firstSrc)
			idx.MergeProperty(second, secondSrc)
			settle()

			require.Len(t, m.Properties, 1, "%s", tc.name)
			merged[i], diags[i] = m.Properties[0], *recorded
		}
		for i := range merged {
			got := merged[i]
			assert.Equal(t, primRef(tc.narrow).Target, got.Type.Target, "%s (order %d): the narrow type wins", tc.name, i)
			assert.Nil(t, got.Default, "%s (order %d): no inadmissible default lands", tc.name, i)
			assert.Empty(t, got.Examples, "%s (order %d): and no inadmissible example", tc.name, i)
			require.Len(t, diags[i], 1, "%s (order %d): one info", tc.name, i)
			assert.Equal(t, ir.SeverityInfo, diags[i][0].Severity)
			assert.Equal(t, diag.DegradedConstruct, diags[i][0].Code)
			assert.Equal(t, jsontext.Pointer(broadPtr), diags[i][0].Provenance.Pointer, "%s: at the broad declaration", tc.name)
			assert.Contains(t, diags[i][0].Message, tc.keyword)
			assert.Equal(t, ir.Unmodeled{"openapi:conflicting-redeclaration" + broadPtr: {
				Reason: ir.ReasonDegradedLowering, Value: ir.RawValue(tc.raw), Provenance: ptrAt(broadPtr),
			}}, got.Unmodeled, "%s (order %d): the broad declaration is kept verbatim", tc.name, i)
		}
		assert.Equal(t, diags[0], diags[1], "%s: the diagnostics do not depend on order", tc.name)
		assert.Equal(t, merged[0].Unmodeled, merged[1].Unmodeled, "%s: nor does Unmodeled", tc.name)
		assert.Equal(t, merged[0].Type.Nullable, merged[1].Type.Nullable, "%s: nor the nullability", tc.name)
	}
	assertThreeDeclarationsKeepTheWritingOne(t)
	assertEveryWriterOfARefusedValueIsKept(t)
	assertRefusedValuesAreKeptAtTheirOwnDeclarations(t)
	assertANullTheMergedFieldRefusesIsKeptAtItsWriter(t)
	assertTheTopTypeKeepsANull(t)
}

// assertANullTheMergedFieldRefusesIsKeptAtItsWriter holds that a null default
// is judged on the declaration that wrote it, whichever side of the fold that
// is and whether the fold narrowed or kept one target: the field ends
// non-nullable with no default, and one info and one entry sit at the writer
// alone, in every order of the declarations.
func assertANullTheMergedFieldRefusesIsKeptAtItsWriter(t *testing.T) {
	t.Helper()
	null := ir.Value{Kind: ir.ValueNull}
	nullInt := decl{kind: ir.PrimInteger, nullable: true, def: &null, raw: `{"type":["integer","null"],"default":null}`}
	nullNum := decl{kind: ir.PrimNumber, nullable: true, def: &null, raw: `{"type":["number","null"],"default":null}`}
	plainInt := decl{kind: ir.PrimInteger, raw: `{"type":"integer"}`}
	plainNum := decl{kind: ir.PrimNumber, raw: `{"type":"number"}`}
	nullIntEx := decl{kind: ir.PrimInteger, nullable: true, examples: []ir.Value{null}, raw: `{"type":["integer","null"],"examples":[null]}`}
	for _, tc := range []struct {
		name  string
		decls []decl // the null writer is first
	}{
		{"a null example, narrow against a number", []decl{nullIntEx, plainNum}},
		{"a null example, the same target", []decl{nullIntEx, plainInt}},
		{"a narrow nullable integer against a number", []decl{nullInt, plainNum}},
		{"a broad nullable number against an integer", []decl{nullNum, plainInt}},
		{"the same target, nullable then not", []decl{nullInt, plainInt}},
		{"a nullable integer among a number and an integer", []decl{nullInt, plainNum, plainInt}},
	} {
		for _, order := range permutations(len(tc.decls)) {
			placed := make([]decl, len(order))
			var writer decl
			for pos, which := range order {
				placed[pos] = tc.decls[which]
				placed[pos].ptr = jsontext.Pointer("/allOf/" + string(rune('0'+pos)))
				if which == 0 {
					writer = placed[pos]
				}
			}
			label := fmt.Sprintf("%s %v", tc.name, order)
			got, diags := mergeDecls(t, placed...)
			assert.Equal(t, primRef(ir.PrimInteger).Target, got.Type.Target, label)
			assert.False(t, got.Type.Nullable, label)
			assert.Nil(t, got.Default, label)
			assert.Empty(t, got.Examples, label)
			assert.Equal(t, entry(writer), got.Unmodeled, "%s: kept at the null writer alone", label)
			require.Len(t, diags, 1, "%s: one info", label)
			assert.Equal(t, ir.SeverityInfo, diags[0].Severity, label)
			assert.Equal(t, writer.ptr, diags[0].Provenance.Pointer, "%s: at the writer", label)
		}
	}
}

// assertTheTopTypeKeepsANull holds that a fold of two top-type declarations
// keeps a null default or example: the top type admits every instance, so
// nothing is refused, reported or kept aside, in either order.
func assertTheTopTypeKeepsANull(t *testing.T) {
	t.Helper()
	null := ir.Value{Kind: ir.ValueNull}
	plain := decl{kind: ir.PrimAny, raw: `{"description":"x"}`}
	for _, writer := range []decl{
		{kind: ir.PrimAny, def: &null, raw: `{"default":null}`},
		{kind: ir.PrimAny, examples: []ir.Value{null}, raw: `{"examples":[null]}`},
	} {
		for _, order := range [][]decl{{writer, plain}, {plain, writer}} {
			label := fmt.Sprintf("%s first", order[0].raw)
			order[0].ptr, order[1].ptr = "/allOf/0", "/allOf/1"
			got, diags := mergeDecls(t, order...)
			assert.Equal(t, writer.def, got.Default, label)
			assert.Len(t, got.Examples, len(writer.examples), label)
			assert.Empty(t, got.Unmodeled, label)
			assert.Empty(t, diags, label)
		}
	}
}

// permutations returns every ordering of 0..n-1.
func permutations(n int) [][]int {
	all := [][]int{{}}
	for k := range n {
		next := make([][]int, 0, len(all)*(k+1))
		for _, rest := range all {
			for at := 0; at <= len(rest); at++ {
				next = append(next, slices.Insert(slices.Clone(rest), at, k))
			}
		}
		all = next
	}
	return all
}

// decl is one declaration of the field "p": its kind, optional default and
// examples, and the JSON it was written as.
type decl struct {
	ptr      jsontext.Pointer
	kind     ir.PrimKind
	nullable bool
	def      *ir.Value
	examples []ir.Value
	raw      string
}

// mergeDecls merges decls in order into one Model and returns its only
// property and the diagnostics recorded.
func mergeDecls(t *testing.T, decls ...decl) (ir.Property, []ir.Diagnostic) {
	t.Helper()
	reg := narrowingRegistry()
	g, recorded := stubMerger(reg)
	m := &ir.Model{}
	idx, settle := indexOver(m, g)
	for _, d := range decls {
		p := ir.Property{WireName: "p", Type: primRef(d.kind), Default: d.def, Provenance: ptrAt(d.ptr)}
		p.Type.Nullable = d.nullable
		for i := range d.examples {
			p.Examples = append(p.Examples, ir.Example{Value: &d.examples[i]})
		}
		idx.MergeProperty(p, written(d.raw))
	}
	settle()
	require.Len(t, m.Properties, 1)
	return m.Properties[0], *recorded
}

// entry is the Unmodeled entry a kept declaration lands as.
func entry(d decl) ir.Unmodeled {
	return ir.Unmodeled{losingDeclarationKey + string(d.ptr): {
		Reason: ir.ReasonDegradedLowering, Value: ir.RawValue(d.raw), Provenance: ptrAt(d.ptr),
	}}
}

// assertThreeDeclarationsKeepTheWritingOne holds that a value one of three
// declarations wrote is kept at that declaration whichever position the
// narrowing one takes: the value never lands, and no entry sits at a
// declaration that does not hold it.
func assertThreeDeclarationsKeepTheWritingOne(t *testing.T) {
	t.Helper()
	half, two := num("1.5"), num("2.5")
	wide := decl{ptr: "/allOf/0", kind: ir.PrimNumber, raw: `{"type":"number"}`}
	narrow := decl{ptr: "/allOf/2", kind: ir.PrimInteger, raw: `{"type":"integer"}`}
	for _, tc := range []struct {
		name    string
		writer  decl
		keyword string
	}{
		{"default", decl{ptr: "/allOf/1", kind: ir.PrimNumber, def: &half,
			raw: `{"type":"number","default":1.5}`}, "default"},
		{"examples", decl{ptr: "/allOf/1", kind: ir.PrimNumber, examples: []ir.Value{two},
			raw: `{"type":"number","examples":[2.5]}`}, "examples"},
	} {
		for name, order := range map[string][]decl{
			"abc": {wide, tc.writer, narrow}, "cab": {narrow, wide, tc.writer}, "bca": {tc.writer, narrow, wide},
		} {
			got, diags := mergeDecls(t, order...)
			label := tc.name + " " + name
			assert.Equal(t, primRef(ir.PrimInteger).Target, got.Type.Target, label)
			assert.Nil(t, got.Default, label)
			assert.Empty(t, got.Examples, label)
			assert.Equal(t, entry(tc.writer), got.Unmodeled, "%s: kept at the declaration that wrote it", label)
			require.Len(t, diags, 1, label)
			assert.Equal(t, tc.writer.ptr, diags[0].Provenance.Pointer, "%s: the info names it", label)
			assert.Contains(t, diags[0].Message, tc.keyword, label)
		}
	}
}

// assertEveryWriterOfARefusedValueIsKept holds that two declarations giving a
// refused value equal are both reported and kept in all six orders, the
// narrowing one never, and that each pointer is the one its position gives.
func assertEveryWriterOfARefusedValueIsKept(t *testing.T) {
	t.Helper()
	half, two := num("1.5"), num("2.5")
	for _, kw := range []string{"default", "examples"} {
		writer := func(name string) decl {
			d := decl{kind: ir.PrimNumber, raw: `{"writer":"` + name + `"}`}
			if kw == "default" {
				d.def = &half
			} else {
				d.examples = []ir.Value{two}
			}
			return d
		}
		decls := map[string]decl{"a": writer("a"), "b": writer("b"), "c": {kind: ir.PrimInteger, raw: `{"type":"integer"}`}}
		for _, order := range []string{"abc", "acb", "bac", "bca", "cab", "cba"} {
			placed := make([]decl, 0, len(order))
			want := ir.Unmodeled{}
			var wantPtrs []jsontext.Pointer
			for i, name := range order {
				d := decls[string(name)]
				d.ptr = jsontext.Pointer("/allOf/" + string(rune('0'+i)))
				placed = append(placed, d)
				if name != 'c' {
					want[losingDeclarationKey+string(d.ptr)] = entry(d)[losingDeclarationKey+string(d.ptr)]
					wantPtrs = append(wantPtrs, d.ptr)
				}
			}
			label := kw + " " + order
			got, diags := mergeDecls(t, placed...)
			assert.Equal(t, primRef(ir.PrimInteger).Target, got.Type.Target, label)
			assert.Nil(t, got.Default, label)
			assert.Empty(t, got.Examples, label)
			assert.Equal(t, want, got.Unmodeled, "%s: both writers are kept, and only they", label)
			gotPtrs := make([]jsontext.Pointer, 0, len(diags))
			for _, d := range diags {
				gotPtrs = append(gotPtrs, d.Provenance.Pointer)
				assert.Contains(t, d.Message, kw, label)
			}
			assert.ElementsMatch(t, wantPtrs, gotPtrs, "%s: and both are reported", label)
		}
	}
}

// assertRefusedValuesAreKeptAtTheirOwnDeclarations holds the grouping: one
// info and one entry per declaration that wrote a refused value, a value
// adopted from the narrow side's own declaration after a refusal moves the
// origin to that declaration.
func assertRefusedValuesAreKeptAtTheirOwnDeclarations(t *testing.T) {
	t.Helper()
	half, two, big := num("1.5"), num("2.5"), num("5000000000")
	defaulted := decl{ptr: "/allOf/0", kind: ir.PrimNumber, def: &half, raw: `{"default":1.5}`}
	exampled := decl{ptr: "/allOf/1", kind: ir.PrimNumber, examples: []ir.Value{two}, raw: `{"examples":[2.5]}`}
	both := decl{ptr: "/allOf/1", kind: ir.PrimNumber, def: &half, examples: []ir.Value{two}, raw: `{"both":1}`}
	narrow := decl{ptr: "/allOf/2", kind: ir.PrimInteger, raw: `{"type":"integer"}`}

	got, diags := mergeDecls(t, defaulted, exampled, narrow)
	assert.Equal(t, ir.Unmodeled{
		losingDeclarationKey + "/allOf/0": entry(defaulted)[losingDeclarationKey+"/allOf/0"],
		losingDeclarationKey + "/allOf/1": entry(exampled)[losingDeclarationKey+"/allOf/1"],
	}, got.Unmodeled, "a default and examples from different declarations are two entries")
	require.Len(t, diags, 2, "and two infos")
	assert.Equal(t, jsontext.Pointer("/allOf/0"), diags[0].Provenance.Pointer)
	assert.Contains(t, diags[0].Message, "default")
	assert.NotContains(t, diags[0].Message, "examples")
	assert.Equal(t, jsontext.Pointer("/allOf/1"), diags[1].Provenance.Pointer)
	assert.Contains(t, diags[1].Message, "examples")

	wide := decl{ptr: "/allOf/0", kind: ir.PrimNumber, raw: `{"type":"number"}`}
	got, diags = mergeDecls(t, wide, both, narrow)
	assert.Equal(t, entry(both), got.Unmodeled, "a default and examples from one declaration are one entry")
	require.Len(t, diags, 1, "and one info naming both")
	assert.Contains(t, diags[0].Message, "default and examples")

	late := decl{ptr: "/allOf/1", kind: ir.PrimInteger, def: &big, raw: `{"late":5000000000}`}
	last := decl{ptr: "/allOf/2", kind: ir.PrimInt32, raw: `{"last":1}`}
	got, diags = mergeDecls(t, defaulted, late, last)
	assert.Equal(t, ir.Unmodeled{
		losingDeclarationKey + "/allOf/0": entry(defaulted)[losingDeclarationKey+"/allOf/0"],
		losingDeclarationKey + "/allOf/1": entry(late)[losingDeclarationKey+"/allOf/1"],
	}, got.Unmodeled, "a value adopted after a refusal is kept at the declaration that wrote it")
	require.Len(t, diags, 2)
	assert.Equal(t, jsontext.Pointer("/allOf/0"), diags[0].Provenance.Pointer)
	assert.Equal(t, jsontext.Pointer("/allOf/1"), diags[1].Provenance.Pointer)
}

// TestFold_ALowerCaseDateTimeIsAdopted pins that RFC 3339 admits
// a lower-case T and Z, so a string default of that spelling lands on a
// datetime_offset field rather than being refused.
func TestFold_ALowerCaseDateTimeIsAdopted(t *testing.T) {
	t.Parallel()
	lower := str("2024-01-01t00:00:00z")
	got, diags := mergeDecls(t,
		decl{ptr: "/a", kind: ir.PrimString, def: &lower, raw: `{}`},
		decl{ptr: "/b", kind: ir.PrimDatetimeOffset, raw: `{}`})
	require.NotNil(t, got.Default)
	assert.Equal(t, lower, *got.Default)
	assert.Empty(t, diags)
	assert.Empty(t, got.Unmodeled)
}

// TestFold_ADeclarationWithNoSourceIsReportedUnkeepable pins that a
// declaration the fold must keep but has no source for is reported as kept in
// no form, rather than dropped silently.
func TestFold_ADeclarationWithNoSourceIsReportedUnkeepable(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(narrowingRegistry())
	half := num("1.5")

	got := foldTwo(g,
		ir.Property{WireName: "p", Type: primRef(ir.PrimNumber), Default: &half, Provenance: ptrAt("/a")},
		ir.Property{WireName: "p", Type: primRef(ir.PrimInteger), Provenance: ptrAt("/b")}, unread(t))

	require.Len(t, *recorded, 2)
	assert.Equal(t, diag.DegradedConstruct, (*recorded)[0].Code)
	assert.Equal(t, diag.UnpreservableConstruct, (*recorded)[1].Code)
	assert.Contains(t, (*recorded)[1].Message, errNoSource.Error())
	assert.Empty(t, got.Unmodeled)
}
