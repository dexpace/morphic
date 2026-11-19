// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package typeformat_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/typeformat"
	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/ir"
)

// hopOnly are the kinds the domain edges pass through that no table row
// spells. Every other kind on an edge is a row's kind.
var hopOnly = map[ir.PrimitiveKind]string{
	ir.PrimitiveFloat: "the arbitrary-precision binary float, which no (type, format) pairing selects",
}

// relationRoots are the declared kinds that narrow nothing but any.
var relationRoots = map[ir.PrimitiveKind]string{
	ir.PrimitiveBool:   "no other kind is a boolean",
	ir.PrimitiveString: "the root of the string formats",
	ir.PrimitiveNumber: "the root of the numeric kinds",
	ir.PrimitiveAny:    "the top",
}

func irDeclared(t *testing.T) *decls.Package {
	t.Helper()
	loadedPackage, err := decls.Load("github.com/dexpace/morphic/ir")
	require.NoError(t, err)
	return loadedPackage
}

func declaredKinds(t *testing.T) []ir.PrimitiveKind {
	t.Helper()
	consts, err := irDeclared(t).StringConsts("PrimitiveKind")
	require.NoError(t, err)
	kinds := make([]ir.PrimitiveKind, 0, len(consts))
	for _, declaredConst := range consts {
		kinds = append(kinds, ir.PrimitiveKind(declaredConst.Value))
	}
	require.NotEmpty(t, kinds)
	return kinds
}

func spelled(primitiveKind ir.PrimitiveKind) bool {
	for _, primitive := range typeformat.FormatTable {
		if primitive == primitiveKind {
			return true
		}
	}
	return false
}

// TestFormatTable_EveryRowDistinguishesTheBareType pins the invariant the
// `string/password` row broke (GitHub #579): a (type, format) key may only map
// to a primitive kind the pairing itself distinguishes, because a row mapping to
// the kind the bare type selects makes the pairing unreadable — the position
// resolves to the shared primitive and nothing anywhere records the format. A
// format whose kind does not differ must be absent from the table, to hoist a
// node as byte and an unknown format do, or be read elsewhere, as password is.
func TestFormatTable_EveryRowDistinguishesTheBareType(t *testing.T) {
	t.Parallel()
	for key, primitive := range typeformat.FormatTable {
		typ, _, paired := strings.Cut(key, "/")
		if !paired {
			continue // the bare type's own row, which no format selects
		}
		bare, ok := typeformat.FormatTable[typ]
		require.True(t, ok, "row %q names a type with no row of its own", key)
		assert.NotEqual(t, bare, primitive,
			"row %q maps to the same kind (%s) as the bare type %q, so the pairing "+
				"reaches no field once the position resolves to the shared primitive; "+
				"a format whose kind does not differ must not be a row", key, primitive, typ)
	}
}

// TestFormatTable_EveryStringRowLowersToItsPrimitive holds the table's string
// rows, and what each lowers to, to the pairings
// docs/compilers/openapi.md#string-formats states. The other tests here hold a
// row to its relations alone, so two rows trading primitives pass them. It reads
// the rows off the table, so a row added or dropped fails here too.
func TestFormatTable_EveryStringRowLowersToItsPrimitive(t *testing.T) {
	t.Parallel()
	want := map[string]ir.PrimitiveKind{
		"date":            ir.PrimitiveDate,
		"time-local":      ir.PrimitiveTime,
		"duration":        ir.PrimitiveDuration,
		"uuid":            ir.PrimitiveUUID,
		"uri":             ir.PrimitiveURL,
		"date-time":       ir.PrimitiveDatetimeOffset,
		"date-time-local": ir.PrimitiveDatetime,
		"binary":          ir.PrimitiveBytes,
	}
	got := make(map[string]ir.PrimitiveKind, len(want))
	for key := range typeformat.FormatTable {
		if format, ok := strings.CutPrefix(key, "string/"); ok {
			got[format] = loweredStringFormat(t, format)
		}
	}
	assert.Empty(t, cmp.Diff(want, got), "the string rows lower as the rule states (-want +got)")
}

// loweredStringFormat compiles a property of `{type: string, format: format}`
// and returns the primitive it reaches. The rule gives such a pairing no node
// of its own, so a position resolving to anything else fails. That reach
// through openapi is why openapitest cannot hold it.
func loweredStringFormat(t *testing.T, format string) ir.PrimitiveKind {
	t.Helper()
	spec := openapitest.ComponentSpec("    H: {type: object, properties: {p: {type: string, format: " + format + "}}}\n")
	doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{openapitest.SourceOf(spec)}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	require.Empty(t, diags, "the format is homed, so nothing is reported")
	model, ok := doc.Types["type/openapi/components/schemas/H"].(*ir.Model)
	require.True(t, ok, "H lowers to a model")
	require.Len(t, model.Properties, 1)
	primitive, ok := doc.Types[model.Properties[0].Type.Target].(*ir.Primitive)
	require.True(t, ok, "%s resolves to a shared primitive", format)
	return primitive.PrimitiveKind
}

// TestFormatTable_EveryRowNarrowsItsBareType holds the relation to what the
// table lowers: every (type, format) row must narrow its bare type's row, or an
// allOf redeclaring a bare type with a format would warn of a conflict that
// loses nothing (GitHub #598). A string row answers to the spelling half, a
// numeric row to the value domain alone.
func TestFormatTable_EveryRowNarrowsItsBareType(t *testing.T) {
	t.Parallel()
	for key, primitive := range typeformat.FormatTable {
		typ, _, paired := strings.Cut(key, "/")
		if !paired {
			continue
		}
		bare, ok := typeformat.FormatTable[typ]
		require.True(t, ok, "row %q names a type with no row of its own", key)
		assert.True(t, typeformat.Narrows(primitive, bare),
			"row %q lowers to %s, which does not narrow the bare %q kind %s", key, primitive, typ, bare)
		if typ != "string" {
			assert.True(t, primitive.StrictlyWithin(bare),
				"numeric row %q must narrow its bare kind by value domain alone", key)
		}
	}
}

// TestFormatTable_EveryRelatedKindIsSpelledOrAHop holds the domain edges to the
// table: a kind an edge touches is one a row selects, or a listed hop.
func TestFormatTable_EveryRelatedKindIsSpelledOrAHop(t *testing.T) {
	t.Parallel()
	kinds := declaredKinds(t)
	for _, primitiveKind := range kinds {
		for _, irPrimitiveKind := range coveringKinds(kinds, primitiveKind) {
			for _, end := range []ir.PrimitiveKind{primitiveKind, irPrimitiveKind} {
				_, hop := hopOnly[end]
				assert.True(t, spelled(end) || hop, "%s on an edge is spelled by no row and is no listed hop", end)
			}
		}
	}
	for primitiveKind, why := range hopOnly {
		assert.False(t, spelled(primitiveKind), "hop %s (%s) is spelled by a row, so it is no hop", primitiveKind, why)
	}
}

// TestRelation_EveryDeclaredKindIsRelatedOrARoot holds relationRoots to the
// declared set: a kind narrows something besides any, or is a listed root.
func TestRelation_EveryDeclaredKindIsRelatedOrARoot(t *testing.T) {
	t.Parallel()
	kinds := declaredKinds(t)
	for _, primitiveKind := range kinds {
		related := false
		for _, other := range kinds {
			related = related || (other != ir.PrimitiveAny && typeformat.Narrows(primitiveKind, other))
		}
		_, root := relationRoots[primitiveKind]
		assert.Equal(t, !root, related, "%s must narrow a kind or be a listed root, not both", primitiveKind)
	}
	for primitiveKind, why := range relationRoots {
		assert.True(t, primitiveKind.Valid(), "root %q (%s) must be a declared kind", primitiveKind, why)
	}
}

// TestRelation_LongestChainFitsTheCap reads the walk's cap from the ir source
// and holds the longest chain the domain edges make to it.
func TestRelation_LongestChainFitsTheCap(t *testing.T) {
	t.Parallel()
	limit, err := irDeclared(t).IntConst("maxDomainDepth")
	require.NoError(t, err)

	kinds := declaredKinds(t)
	var longest func(primitiveKind ir.PrimitiveKind, depth int) int
	longest = func(primitiveKind ir.PrimitiveKind, depth int) int {
		require.LessOrEqual(t, int64(depth), limit, "a chain past the cap: a cycle or a cap too low")
		best := 0
		for _, irPrimitiveKind := range coveringKinds(kinds, primitiveKind) {
			best = max(best, 1+longest(irPrimitiveKind, depth+1))
		}
		return best
	}
	chain := 0
	for _, primitiveKind := range kinds {
		chain = max(chain, longest(primitiveKind, 0))
	}
	assert.LessOrEqual(t, chain, int(limit))
}

// TestNarrows_HoldsEveryContainment pins the relation both ways: every
// containment holds one way only, a kind never narrows itself, and the
// unrelated pairs hold neither way.
func TestNarrows_HoldsEveryContainment(t *testing.T) {
	t.Parallel()
	holds := [][2]ir.PrimitiveKind{
		{ir.PrimitiveInt8, ir.PrimitiveInt16}, {ir.PrimitiveInt16, ir.PrimitiveInt32}, {ir.PrimitiveInt32, ir.PrimitiveInt64},
		{ir.PrimitiveInt64, ir.PrimitiveInteger}, {ir.PrimitiveUint8, ir.PrimitiveUint16}, {ir.PrimitiveUint16, ir.PrimitiveUint32},
		{ir.PrimitiveUint32, ir.PrimitiveUint64}, {ir.PrimitiveUint64, ir.PrimitiveInteger}, {ir.PrimitiveInteger, ir.PrimitiveDecimal},
		{ir.PrimitiveDecimal128, ir.PrimitiveDecimal}, {ir.PrimitiveDecimal, ir.PrimitiveNumber}, {ir.PrimitiveFloat32, ir.PrimitiveFloat64},
		{ir.PrimitiveFloat64, ir.PrimitiveFloat}, {ir.PrimitiveFloat, ir.PrimitiveNumber}, {ir.PrimitiveDate, ir.PrimitiveString},
		{ir.PrimitiveTime, ir.PrimitiveString}, {ir.PrimitiveDatetime, ir.PrimitiveString}, {ir.PrimitiveDatetimeOffset, ir.PrimitiveString},
		{ir.PrimitiveDuration, ir.PrimitiveString}, {ir.PrimitiveURL, ir.PrimitiveString}, {ir.PrimitiveUUID, ir.PrimitiveString},
		{ir.PrimitiveBytes, ir.PrimitiveString},
		{ir.PrimitiveInt8, ir.PrimitiveNumber}, {ir.PrimitiveUint8, ir.PrimitiveDecimal}, {ir.PrimitiveInt32, ir.PrimitiveInteger},
		{ir.PrimitiveFloat32, ir.PrimitiveNumber}, {ir.PrimitiveUint8, ir.PrimitiveInt16}, {ir.PrimitiveInt16, ir.PrimitiveFloat32},
		{ir.PrimitiveUint16, ir.PrimitiveFloat32}, {ir.PrimitiveInt32, ir.PrimitiveFloat64}, {ir.PrimitiveUint32, ir.PrimitiveFloat64},
		{ir.PrimitiveInteger, ir.PrimitiveFloat}, {ir.PrimitiveUUID, ir.PrimitiveAny}, {ir.PrimitiveInteger, ir.PrimitiveAny},
	}
	for _, p := range holds {
		assert.True(t, typeformat.Narrows(p[0], p[1]), "%s narrows %s", p[0], p[1])
		assert.False(t, typeformat.Narrows(p[1], p[0]), "%s does not narrow %s", p[1], p[0])
	}

	unrelated := [][2]ir.PrimitiveKind{
		{ir.PrimitiveString, ir.PrimitiveInteger}, {ir.PrimitiveUUID, ir.PrimitiveDatetimeOffset}, {ir.PrimitiveInt32, ir.PrimitiveUint32},
		{ir.PrimitiveDatetime, ir.PrimitiveDatetimeOffset}, {ir.PrimitiveBool, ir.PrimitiveString}, {ir.PrimitiveBool, ir.PrimitiveInteger},
		{ir.PrimitiveBytes, ir.PrimitiveInteger}, {ir.PrimitiveTime, ir.PrimitiveDatetime},
	}
	for _, p := range unrelated {
		assert.False(t, typeformat.Narrows(p[0], p[1]), "%s does not narrow %s", p[0], p[1])
		assert.False(t, typeformat.Narrows(p[1], p[0]), "%s does not narrow %s", p[1], p[0])
	}

	for _, primitiveKind := range declaredKinds(t) {
		assert.False(t, typeformat.Narrows(primitiveKind, primitiveKind), "%s is not strictly narrower than itself", primitiveKind)
	}
}

// TestNarrows_OmitsRoundingContainments pins the choice not to take a
// containment that holds only by rounding. A narrowing is adopted silently, so
// it must hold of every value and every JSON instance. OpenAPI floats round:
// an int32, uint32, int64 or uint64 sits inside float32 only by losing low bits, an
// int64 or uint64 inside float64 likewise, and integer inside either float
// width has no bound at all. Those pairs overlap: the first declaration is kept
// and the second reported.
func TestNarrows_OmitsRoundingContainments(t *testing.T) {
	t.Parallel()
	rounding := [][2]ir.PrimitiveKind{
		{ir.PrimitiveInt32, ir.PrimitiveFloat32}, {ir.PrimitiveUint32, ir.PrimitiveFloat32}, {ir.PrimitiveInt64, ir.PrimitiveFloat32}, {ir.PrimitiveUint64, ir.PrimitiveFloat32},
		{ir.PrimitiveInt64, ir.PrimitiveFloat64}, {ir.PrimitiveUint64, ir.PrimitiveFloat64},
		{ir.PrimitiveInteger, ir.PrimitiveFloat32}, {ir.PrimitiveInteger, ir.PrimitiveFloat64},
	}
	for _, p := range rounding {
		assert.False(t, typeformat.Narrows(p[0], p[1]), "%s is not narrower than %s", p[0], p[1])
		assert.Equal(t, typeformat.Overlap, typeformat.Relate(p[0], p[1]), "%s against %s", p[0], p[1])
		assert.Equal(t, typeformat.Overlap, typeformat.Relate(p[1], p[0]), "%s against %s", p[1], p[0])
	}
}

// TestRelate_ClassifiesEveryVerdict covers each verdict, both ways round.
func TestRelate_ClassifiesEveryVerdict(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a, b ir.PrimitiveKind
		want typeformat.Relation
		back typeformat.Relation
	}{
		{"same", ir.PrimitiveString, ir.PrimitiveString, typeformat.Same, typeformat.Same},
		{"domain narrowing", ir.PrimitiveInt32, ir.PrimitiveInteger, typeformat.ANarrower, typeformat.BNarrower},
		{"spelling narrowing", ir.PrimitiveUUID, ir.PrimitiveString, typeformat.ANarrower, typeformat.BNarrower},
		{"numeric overlap", ir.PrimitiveInt32, ir.PrimitiveUint32, typeformat.Overlap, typeformat.Overlap},
		{"number kinds overlap", ir.PrimitiveDecimal128, ir.PrimitiveFloat32, typeformat.Overlap, typeformat.Overlap},
		{"bytes against uuid", ir.PrimitiveBytes, ir.PrimitiveUUID, typeformat.Overlap, typeformat.Overlap},
		{"two string formats", ir.PrimitiveUUID, ir.PrimitiveDatetimeOffset, typeformat.Disjoint, typeformat.Disjoint},
		{"string against number", ir.PrimitiveString, ir.PrimitiveInteger, typeformat.Disjoint, typeformat.Disjoint},
		{"bool against string", ir.PrimitiveBool, ir.PrimitiveString, typeformat.Disjoint, typeformat.Disjoint},
		{"datetime against a format", ir.PrimitiveDatetime, ir.PrimitiveDate, typeformat.Disjoint, typeformat.Disjoint},
		{"any is the top", ir.PrimitiveUUID, ir.PrimitiveAny, typeformat.ANarrower, typeformat.BNarrower},
		{"any against itself", ir.PrimitiveAny, ir.PrimitiveAny, typeformat.Same, typeformat.Same},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, typeformat.Relate(tc.a, tc.b), "%s: %s against %s", tc.name, tc.a, tc.b)
		assert.Equal(t, tc.back, typeformat.Relate(tc.b, tc.a), "%s: %s against %s", tc.name, tc.b, tc.a)
	}
}

// TestLookup_ReadsTheTable pins the exported lookup: a known key selects its
// row and an unknown one reports it.
func TestLookup_ReadsTheTable(t *testing.T) {
	t.Parallel()
	primitive, ok := typeformat.Lookup("integer/int32")
	assert.True(t, ok)
	assert.Equal(t, ir.PrimitiveInt32, primitive)

	_, ok = typeformat.Lookup("string/password")
	assert.False(t, ok)
}

// TestLookupEncoded_ReadsTheTable pins the exported lookup of the encoded
// pairings: a known key selects its row, and a key only the format table or
// neither table has reports none.
func TestLookupEncoded_ReadsTheTable(t *testing.T) {
	t.Parallel()
	row, ok := typeformat.LookupEncoded("integer/unixtime")
	assert.True(t, ok)
	assert.Equal(t, typeformat.Encoded{Primitive: ir.PrimitiveDatetimeOffset, Encoding: "unixTimestamp", Wire: ir.PrimitiveInteger}, row)

	for _, key := range []string{"string/date-time", "boolean/unixtime", "string"} {
		_, ok = typeformat.LookupEncoded(key)
		assert.False(t, ok, key)
	}
}

// TestEncodedTable_IsApartFromTheFormatTable holds the encoded pairings to the
// format table and to the IR. No key is a row of both, which would make the
// order of the two lookups decide the lowering. Each key pairs a type the
// format table has with a format. Each primitive is one ir declares, and Wire
// is that type's own primitive or none, none only where the primitive narrows
// it, as a datetime_offset is a string. Each encoding is a name
// docs/ir/constraints.md#encodings lists, which a consumer keys a row by.
func TestEncodedTable_IsApartFromTheFormatTable(t *testing.T) {
	t.Parallel()
	names := documentedEncodings(t)
	require.NotEmpty(t, typeformat.EncodedTable)
	for key, row := range typeformat.EncodedTable {
		_, inBoth := typeformat.FormatTable[key]
		assert.False(t, inBoth, "%s is a row of both tables", key)
		typ, format, paired := strings.Cut(key, "/")
		require.True(t, paired && format != "", "%s pairs a type with a format", key)
		bare, typed := typeformat.FormatTable[typ]
		require.True(t, typed, "%s names a type the format table has", key)

		assert.True(t, row.Primitive.Valid(), "%s: %q is a primitive ir declares", key, row.Primitive)
		if row.Wire == "" {
			assert.True(t, typeformat.Narrows(row.Primitive, bare), "%s travels in its primitive's own spelling, so it narrows %s", key, bare)
		} else {
			assert.Equal(t, bare, row.Wire, "%s travels as the type it is written on", key)
		}
		assert.Contains(t, names, row.Encoding, "%s: the IR documents the encoding a consumer keys a row by", key)
	}
}

// documentedEncodings reads the encoding names the Name field's comment lists
// in docs/ir/constraints.md#encodings.
func documentedEncodings(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile("../../../../docs/ir/constraints.md")
	require.NoError(t, err)
	_, section, found := strings.Cut(string(data), "\n## Encodings\n")
	require.True(t, found, "the document has an Encodings section")
	_, field, found := strings.Cut(section, "Name ")
	require.True(t, found, "the section declares Name")
	field, _, found = strings.Cut(field, "WireType ")
	require.True(t, found, "WireType follows Name")
	quoted := regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(field, -1)
	names := make([]string, 0, len(quoted))
	for _, m := range quoted {
		names = append(names, m[1])
	}
	require.Contains(t, names, "base64", "the reading finds the names the comment lists")
	return names
}

// coveringKinds returns the kinds immediately above primitiveKind by value
// domain: those primitiveKind is strictly within, with no declared kind
// between, and never the top type. It reads the containment through the
// exported StrictlyWithin, which is what the unexported edge table in ir
// yields.
func coveringKinds(kinds []ir.PrimitiveKind, primitiveKind ir.PrimitiveKind) []ir.PrimitiveKind {
	var out []ir.PrimitiveKind
	for _, irPrimitiveKind := range kinds {
		if irPrimitiveKind == ir.PrimitiveAny || !primitiveKind.StrictlyWithin(irPrimitiveKind) {
			continue
		}
		between := false
		for _, primitiveKindValue := range kinds {
			between = between || (primitiveKindValue != ir.PrimitiveAny && primitiveKind.StrictlyWithin(primitiveKindValue) && primitiveKindValue.StrictlyWithin(irPrimitiveKind))
		}
		if !between {
			out = append(out, irPrimitiveKind)
		}
	}
	return out
}
