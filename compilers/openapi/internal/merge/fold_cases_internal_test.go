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
// order to different values fold to the later one's, so the other, which may be
// the stricter, is discarded and must be announced.
func TestFold_AKeywordNoOneValueStatesIsKeptAndReported(t *testing.T) {
	t.Parallel()
	stubbed, recorded := stubMerger(nil)
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

	dst = foldTwo(stubbed, dst, src, written(`{"pattern":"^b$"}`))

	require.Len(t, *recorded, 1)
	assert.Equal(t, "^b$", dst.Constraints.Pattern, "the later pattern is kept")
	assert.Equal(t, ir.SeverityWarning, (*recorded)[0].Severity)
	assert.Equal(t, diag.ConflictingRedecl, (*recorded)[0].Code)
	assert.Contains(t, (*recorded)[0].Message, `conflicting pattern ("^b$" and "^a$")`)
	assert.Contains(t, (*recorded)[0].Message, "/components/schemas/A/properties/age",
		"the kept declaration is named")
	assert.Contains(t, (*recorded)[0].Message, "/components/schemas/B/properties/age",
		"and so is the discarded one")
}

// TestKindOf_EnumResolvesThroughItsValueType pins the enum case of the
// resolution walk. An enum member can itself be a scalar of the kind being
// redeclared, so it must answer with its value type rather than staying
// unresolved and falling through to the structural comparison.
func TestKindOf_EnumResolvesThroughItsValueType(t *testing.T) {
	t.Parallel()
	stubbed, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"type/enum": &ir.Enum{ID: "type/enum", ValueType: ir.PrimitiveString},
		"type/alias": &ir.Scalar{
			ID:   "type/alias",
			Base: &ir.TypeRef{Target: "type/enum"},
		},
	})

	kind, ok := judge(stubbed).kindOf(ir.TypeRef{Target: "type/enum"})
	require.True(t, ok)
	assert.Equal(t, ir.PrimitiveString, kind.primitive)
	assert.Empty(t, kind.format)

	kind, ok = judge(stubbed).kindOf(ir.TypeRef{Target: "type/alias"})
	require.True(t, ok, "a scalar's Base chain is followed to the enum behind it")
	assert.Equal(t, ir.PrimitiveString, kind.primitive)
	assert.Empty(t, kind.format)
}

// TestFold_ALosingDetailIsKeptAndReported sweeps the fold's
// adopt-if-absent fields. Each of them once took one declaration's value and
// dropped a present, different value from the other with nothing said and
// nothing kept — a consumer diffing two revisions that change the losing
// entry's default saw no change. Now the merge keeps the later declaration's
// value, says which detail disagreed, and keeps the losing declaration
// verbatim, under the same key a losing type is kept under, since it is the
// same event: a declaration the merge could not fold whole. Examples are no
// such field: the field holds those of both
// (TestFold_EveryExampleTheModelWritesIsHeld).
func TestFold_ALosingDetailIsKeptAndReported(t *testing.T) {
	t.Parallel()
	one, two := &ir.Value{Kind: ir.ValueNumber, Number: "1"}, &ir.Value{Kind: ir.ValueNumber, Number: "2"}
	cases := []struct {
		name     string
		dst, src ir.Property
		detail   string
	}{
		{"default", ir.Property{Default: one}, ir.Property{Default: two}, "default"},
		{"deprecation", ir.Property{Deprecation: &ir.Deprecation{Since: "1"}}, ir.Property{Deprecation: &ir.Deprecation{Since: "2"}}, "deprecation"},
		{"xml", ir.Property{XML: &ir.XMLHints{Name: "a"}}, ir.Property{XML: &ir.XMLHints{Name: "b"}}, "xml"},
		{"xml wrapping", ir.Property{XML: &ir.XMLHints{Wrapped: new(true)}}, ir.Property{XML: &ir.XMLHints{Wrapped: new(false)}}, "xml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stubbed, recorded := stubMerger(map[ir.TypeID]ir.TypeDef{
				"type/int": &ir.Primitive{ID: "type/int", PrimitiveKind: ir.PrimitiveInt32},
			})
			dst, src := tc.dst, tc.src
			dst.WireName, src.WireName = "id", "id"
			dst.Type, src.Type = ir.TypeRef{Target: "type/int"}, ir.TypeRef{Target: "type/int"}
			dst.Provenance = ir.Provenance{Source: 1, Pointer: "/allOf/0/properties/id"}
			src.Provenance = ir.Provenance{Source: 1, Pointer: "/allOf/1/properties/id"}
			want := tc.src

			dst = foldTwo(stubbed, dst, src, unread(t))

			assert.Equal(t, want.Default, dst.Default, "the later declaration keeps its default")
			assert.Equal(t, want.Deprecation, dst.Deprecation, "and its deprecation")
			assert.Equal(t, want.XML, dst.XML, "and its xml hints")
			require.Len(t, *recorded, 1, "one detail disagreed, one diagnostic; got %v", *recorded)
			diagnostic := (*recorded)[0]
			assert.Equal(t, ir.SeverityInfo, diagnostic.Severity)
			assert.Equal(t, diag.DegradedConstruct, diagnostic.Code)
			assert.Equal(t, jsontext.Pointer("/allOf/0/properties/id"), diagnostic.Provenance.Pointer)
			assert.Contains(t, diagnostic.Message, `"id"`)
			assert.Contains(t, diagnostic.Message, tc.detail, "the message names the detail that disagreed")
			assert.Equal(t, ir.Unmodeled{
				losingDeclarationKey + "/allOf/0/properties/id": {
					Reason:     ir.ReasonDegradedLowering,
					Value:      ir.RawValue(firstSource),
					Provenance: ir.Provenance{Source: 1, Pointer: "/allOf/0/properties/id"},
				},
			}, dst.Unmodeled)
		})
	}
}

// TestFold_AnIdenticalDetailIsNotADisagreement is the other half:
// two entries writing the same default have not disagreed, and reporting or
// keeping one would be noise. The source is unread, which is the proof.
func TestFold_AnIdenticalDetailIsNotADisagreement(t *testing.T) {
	t.Parallel()
	stubbed, recorded := stubMerger(map[ir.TypeID]ir.TypeDef{
		"type/int": &ir.Primitive{ID: "type/int", PrimitiveKind: ir.PrimitiveInt32},
	})
	detail := func() ir.Property {
		return ir.Property{
			WireName:    "id",
			Type:        ir.TypeRef{Target: "type/int"},
			Default:     &ir.Value{Kind: ir.ValueNumber, Number: "1"},
			Examples:    []ir.Example{{Value: &ir.Value{Kind: ir.ValueNumber, Number: "1"}}},
			Deprecation: &ir.Deprecation{Since: "1"},
			XML:         &ir.XMLHints{Name: "a", Wrapped: new(true)},
			Provenance:  ptrAt("/allOf/1/properties/id"),
		}
	}
	dst := detail()

	dst = foldTwo(stubbed, dst, detail(), unread(t))

	assert.Empty(t, *recorded)
	assert.Empty(t, dst.Unmodeled)
}

// TestFold_AnUnrenderableLoserIsAnError pins the one way keeping
// the declaration can fail. The merge has already said the redeclaration is
// kept, so a source that will not render is reported as an error — the
// construct reached the IR in no form — rather than left to the announcement.
func TestFold_AnUnrenderableLoserIsAnError(t *testing.T) {
	t.Parallel()
	stubbed, recorded := stubMerger(map[ir.TypeID]ir.TypeDef{
		"type/str": &ir.Primitive{ID: "type/str", PrimitiveKind: ir.PrimitiveString},
		"type/int": &ir.Primitive{ID: "type/int", PrimitiveKind: ir.PrimitiveInt32},
	})
	loser := ir.Property{WireName: "id", Type: ir.TypeRef{Target: "type/str"}, Provenance: ptrAt("/a")}
	winner := ir.Property{WireName: "id", Type: ir.TypeRef{Target: "type/int"}, Provenance: ptrAt("/b")}

	dst := stubbed.fold([]declaration{
		{property: loser, source: func() (ir.RawValue, error) { return nil, errors.New("no such node") }},
		{property: winner, source: unread(t)},
	})

	require.Len(t, *recorded, 2, "the conflict, and then the failure to keep it; got %v", *recorded)
	assert.Equal(t, diag.ConflictingRedecl, (*recorded)[0].Code)
	assert.Equal(t, diag.UnpreservableConstruct, (*recorded)[1].Code)
	assert.Equal(t, ir.SeverityError, (*recorded)[1].Severity)
	assert.Empty(t, dst.Unmodeled, "nothing claims a preservation that did not happen")
}

// narrowingRegistry holds one bare primitive per kind the narrowing tests pair.
func narrowingRegistry() map[ir.TypeID]ir.TypeDef {
	reg := map[ir.TypeID]ir.TypeDef{}
	for _, primitiveKind := range []ir.PrimitiveKind{
		ir.PrimitiveNumber, ir.PrimitiveInteger, ir.PrimitiveInt32, ir.PrimitiveInt64, ir.PrimitiveString, ir.PrimitiveDatetimeOffset,
		ir.PrimitiveURL, ir.PrimitiveFloat64, ir.PrimitiveUUID, ir.PrimitiveUint32, ir.PrimitiveBytes,
		ir.PrimitiveUint8, ir.PrimitiveInt16, ir.PrimitiveAny, ir.PrimitiveBool, ir.PrimitiveFloat32, ir.PrimitiveTime, ir.PrimitiveDatetime,
	} {
		id := ir.TypeID("type/primitive/" + string(primitiveKind))
		reg[id] = &ir.Primitive{ID: id, PrimitiveKind: primitiveKind}
	}
	return reg
}

func primitiveRef(primitiveKind ir.PrimitiveKind) ir.TypeRef {
	return ir.TypeRef{Target: ir.TypeID("type/primitive/" + string(primitiveKind))}
}

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
	half, big, hello := number("1.5"), number("5000000000"), stringValue("hello")
	badURL, commaTime, offsetDate := stringValue("mailto:a b"), stringValue("23:59:59,5"), stringValue("2024-01-01T00:00:00+24:00")
	null := ir.Value{Kind: ir.ValueNull}
	for _, tc := range []struct {
		name          string
		broad, narrow ir.PrimitiveKind
		decorate      func(*ir.Property)
		keyword       string
		raw           string
	}{
		{"fractional default", ir.PrimitiveNumber, ir.PrimitiveInteger,
			func(property *ir.Property) {
				property.Default = &half
				property.Examples = []ir.Example{{Value: &half}}
			}, "default",
			`{"type":"number","default":1.5,"examples":[1.5]}`},
		{"out of range default", ir.PrimitiveInt64, ir.PrimitiveInt32,
			func(property *ir.Property) { property.Default = &big }, "default", `{"type":"integer","format":"int64","default":5000000000}`},
		{"non uuid example", ir.PrimitiveString, ir.PrimitiveUUID,
			func(property *ir.Property) { property.Examples = []ir.Example{{Value: &hello}} }, "examples", `{"type":"string","examples":["hello"]}`},
		{"non url default", ir.PrimitiveString, ir.PrimitiveURL,
			func(property *ir.Property) { property.Default = &badURL }, "default", `{"type":"string","default":"mailto:a b"}`},
		{"comma fraction time example", ir.PrimitiveString, ir.PrimitiveTime,
			func(property *ir.Property) { property.Examples = []ir.Example{{Value: &commaTime}} }, "examples",
			`{"type":"string","examples":["23:59:59,5"]}`},
		{"offset hour 24 date time example", ir.PrimitiveString, ir.PrimitiveDatetimeOffset,
			func(property *ir.Property) { property.Examples = []ir.Example{{Value: &offsetDate}} }, "examples",
			`{"type":"string","examples":["2024-01-01T00:00:00+24:00"]}`},
		{"null default", ir.PrimitiveNumber, ir.PrimitiveInteger,
			func(property *ir.Property) { property.Default = &null; property.Type.Nullable = true }, "default", `{"type":["number","null"],"default":null}`},
	} {
		var merged [2]ir.Property
		var diags [2][]ir.Diagnostic
		for i, broadFirst := range []bool{true, false} {
			reg := narrowingRegistry()
			stubbed, recorded := stubMerger(reg)
			broad := ir.Property{WireName: "p", Type: primitiveRef(tc.broad), Provenance: ptrAt(broadPtr)}
			tc.decorate(&broad)
			narrow := ir.Property{WireName: "p", Type: primitiveRef(tc.narrow), Provenance: ptrAt(narrowPtr)}
			model := &ir.Model{}
			idx, settle := indexOver(model, stubbed)
			first, second := broad, narrow
			firstSrc, secondSrc := written(tc.raw), unread(t)
			if !broadFirst {
				first, second = narrow, broad
				firstSrc, secondSrc = unread(t), written(tc.raw)
				first.Provenance, second.Provenance = ptrAt(narrowPtr), ptrAt(broadPtr)
			}
			idx.MergeProperty(first, firstSrc, "")
			idx.MergeProperty(second, secondSrc, "")
			settle()

			require.Len(t, model.Properties, 1, "%s", tc.name)
			merged[i], diags[i] = model.Properties[0], *recorded
		}
		for i := range merged {
			got := merged[i]
			assert.Equal(t, primitiveRef(tc.narrow).Target, got.Type.Target, "%s (order %d): the narrow type wins", tc.name, i)
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
	nullInt := decl{kind: ir.PrimitiveInteger, nullable: true, def: &null, raw: `{"type":["integer","null"],"default":null}`}
	nullNumber := decl{kind: ir.PrimitiveNumber, nullable: true, def: &null, raw: `{"type":["number","null"],"default":null}`}
	plainInt := decl{kind: ir.PrimitiveInteger, raw: `{"type":"integer"}`}
	plainNumber := decl{kind: ir.PrimitiveNumber, raw: `{"type":"number"}`}
	nullIntEx := decl{kind: ir.PrimitiveInteger, nullable: true, examples: []ir.Value{null}, raw: `{"type":["integer","null"],"examples":[null]}`}
	for _, tc := range []struct {
		name  string
		decls []decl // the null writer is first
	}{
		{"a null example, narrow against a number", []decl{nullIntEx, plainNumber}},
		{"a null example, the same target", []decl{nullIntEx, plainInt}},
		{"a narrow nullable integer against a number", []decl{nullInt, plainNumber}},
		{"a broad nullable number against an integer", []decl{nullNumber, plainInt}},
		{"the same target, nullable then not", []decl{nullInt, plainInt}},
		{"a nullable integer among a number and an integer", []decl{nullInt, plainNumber, plainInt}},
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
			assert.Equal(t, primitiveRef(ir.PrimitiveInteger).Target, got.Type.Target, label)
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
	plain := decl{kind: ir.PrimitiveAny, raw: `{"description":"x"}`}
	for _, writer := range []decl{
		{kind: ir.PrimitiveAny, def: &null, raw: `{"default":null}`},
		{kind: ir.PrimitiveAny, examples: []ir.Value{null}, raw: `{"examples":[null]}`},
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
	kind     ir.PrimitiveKind
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
	stubbed, recorded := stubMerger(reg)
	model := &ir.Model{}
	idx, settle := indexOver(model, stubbed)
	for _, decl := range decls {
		property := ir.Property{WireName: "p", Type: primitiveRef(decl.kind), Default: decl.def, Provenance: ptrAt(decl.ptr)}
		property.Type.Nullable = decl.nullable
		for i := range decl.examples {
			property.Examples = append(property.Examples, ir.Example{Value: &decl.examples[i]})
		}
		idx.MergeProperty(property, written(decl.raw), "")
	}
	settle()
	require.Len(t, model.Properties, 1)
	return model.Properties[0], *recorded
}

// entry is the Unmodeled entry a kept declaration lands as.
func entry(declValue decl) ir.Unmodeled {
	return ir.Unmodeled{losingDeclarationKey + string(declValue.ptr): {
		Reason: ir.ReasonDegradedLowering, Value: ir.RawValue(declValue.raw), Provenance: ptrAt(declValue.ptr),
	}}
}

// assertThreeDeclarationsKeepTheWritingOne holds that a value one of three
// declarations wrote is kept at that declaration whichever position the
// narrowing one takes: the value never lands, and no entry sits at a
// declaration that does not hold it.
func assertThreeDeclarationsKeepTheWritingOne(t *testing.T) {
	t.Helper()
	half, two := number("1.5"), number("2.5")
	wide := decl{ptr: "/allOf/0", kind: ir.PrimitiveNumber, raw: `{"type":"number"}`}
	narrow := decl{ptr: "/allOf/2", kind: ir.PrimitiveInteger, raw: `{"type":"integer"}`}
	for _, tc := range []struct {
		name    string
		writer  decl
		keyword string
	}{
		{"default", decl{ptr: "/allOf/1", kind: ir.PrimitiveNumber, def: &half,
			raw: `{"type":"number","default":1.5}`}, "default"},
		{"examples", decl{ptr: "/allOf/1", kind: ir.PrimitiveNumber, examples: []ir.Value{two},
			raw: `{"type":"number","examples":[2.5]}`}, "examples"},
	} {
		for name, order := range map[string][]decl{
			"abc": {wide, tc.writer, narrow}, "cab": {narrow, wide, tc.writer}, "bca": {tc.writer, narrow, wide},
		} {
			got, diags := mergeDecls(t, order...)
			label := tc.name + " " + name
			assert.Equal(t, primitiveRef(ir.PrimitiveInteger).Target, got.Type.Target, label)
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
	half, two := number("1.5"), number("2.5")
	for _, kw := range []string{"default", "examples"} {
		writer := func(name string) decl {
			declValue := decl{kind: ir.PrimitiveNumber, raw: `{"writer":"` + name + `"}`}
			if kw == "default" {
				declValue.def = &half
			} else {
				declValue.examples = []ir.Value{two}
			}
			return declValue
		}
		decls := map[string]decl{"a": writer("a"), "b": writer("b"), "c": {kind: ir.PrimitiveInteger, raw: `{"type":"integer"}`}}
		for _, order := range []string{"abc", "acb", "bac", "bca", "cab", "cba"} {
			placed := make([]decl, 0, len(order))
			want := ir.Unmodeled{}
			var wantPtrs []jsontext.Pointer
			for i, name := range order {
				decl := decls[string(name)]
				decl.ptr = jsontext.Pointer("/allOf/" + string(rune('0'+i)))
				placed = append(placed, decl)
				if name != 'c' {
					want[losingDeclarationKey+string(decl.ptr)] = entry(decl)[losingDeclarationKey+string(decl.ptr)]
					wantPtrs = append(wantPtrs, decl.ptr)
				}
			}
			label := kw + " " + order
			got, diags := mergeDecls(t, placed...)
			assert.Equal(t, primitiveRef(ir.PrimitiveInteger).Target, got.Type.Target, label)
			assert.Nil(t, got.Default, label)
			assert.Empty(t, got.Examples, label)
			assert.Equal(t, want, got.Unmodeled, "%s: both writers are kept, and only they", label)
			gotPtrs := make([]jsontext.Pointer, 0, len(diags))
			for _, diagnostic := range diags {
				gotPtrs = append(gotPtrs, diagnostic.Provenance.Pointer)
				assert.Contains(t, diagnostic.Message, kw, label)
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
	half, two, big := number("1.5"), number("2.5"), number("5000000000")
	defaulted := decl{ptr: "/allOf/0", kind: ir.PrimitiveNumber, def: &half, raw: `{"default":1.5}`}
	exampled := decl{ptr: "/allOf/1", kind: ir.PrimitiveNumber, examples: []ir.Value{two}, raw: `{"examples":[2.5]}`}
	both := decl{ptr: "/allOf/1", kind: ir.PrimitiveNumber, def: &half, examples: []ir.Value{two}, raw: `{"both":1}`}
	narrow := decl{ptr: "/allOf/2", kind: ir.PrimitiveInteger, raw: `{"type":"integer"}`}

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

	wide := decl{ptr: "/allOf/0", kind: ir.PrimitiveNumber, raw: `{"type":"number"}`}
	got, diags = mergeDecls(t, wide, both, narrow)
	assert.Equal(t, entry(both), got.Unmodeled, "a default and examples from one declaration are one entry")
	require.Len(t, diags, 1, "and one info naming both")
	assert.Contains(t, diags[0].Message, "default and examples")

	late := decl{ptr: "/allOf/1", kind: ir.PrimitiveInteger, def: &big, raw: `{"late":5000000000}`}
	last := decl{ptr: "/allOf/2", kind: ir.PrimitiveInt32, raw: `{"last":1}`}
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
	lower := stringValue("2024-01-01t00:00:00z")
	got, diags := mergeDecls(t,
		decl{ptr: "/a", kind: ir.PrimitiveString, def: &lower, raw: `{}`},
		decl{ptr: "/b", kind: ir.PrimitiveDatetimeOffset, raw: `{}`})
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
	stubbed, recorded := stubMerger(narrowingRegistry())
	half := number("1.5")

	got := stubbed.fold([]declaration{
		{property: ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveNumber), Default: &half, Provenance: ptrAt("/a")}},
		{property: ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveInteger), Provenance: ptrAt("/b")}, source: unread(t)},
	})

	require.Len(t, *recorded, 2)
	assert.Equal(t, diag.DegradedConstruct, (*recorded)[0].Code)
	assert.Equal(t, diag.UnpreservableConstruct, (*recorded)[1].Code)
	assert.Contains(t, (*recorded)[1].Message, errNoSource.Error())
	assert.Empty(t, got.Unmodeled)
}
