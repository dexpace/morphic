// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package spelling_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/spelling"
	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/ir"
)

// hopOnly are the kinds the domain edges pass through that no table row
// spells. Every other kind on an edge is a row's kind.
var hopOnly = map[ir.PrimKind]string{
	ir.PrimFloat: "the arbitrary-precision binary float, which no (type, format) pairing selects",
}

// relationRoots are the declared kinds that narrow nothing but any.
var relationRoots = map[ir.PrimKind]string{
	ir.PrimBool:     "no other kind is a boolean",
	ir.PrimString:   "the root of the string formats",
	ir.PrimNumber:   "the root of the numeric kinds",
	ir.PrimAny:      "the top",
	ir.PrimDatetime: "no spelling lowers to it",
}

func irDeclared(t *testing.T) *decls.Package {
	t.Helper()
	p, err := decls.Load("github.com/dexpace/morphic/ir")
	require.NoError(t, err)
	return p
}

func declaredKinds(t *testing.T) []ir.PrimKind {
	t.Helper()
	consts, err := irDeclared(t).StringConsts("PrimKind")
	require.NoError(t, err)
	kinds := make([]ir.PrimKind, 0, len(consts))
	for _, c := range consts {
		kinds = append(kinds, ir.PrimKind(c.Value))
	}
	require.NotEmpty(t, kinds)
	return kinds
}

func spelled(k ir.PrimKind) bool {
	for _, prim := range spelling.FormatTable {
		if prim == k {
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
	for key, prim := range spelling.FormatTable {
		typ, _, paired := strings.Cut(key, "/")
		if !paired {
			continue // the bare type's own row, which no format selects
		}
		bare, ok := spelling.FormatTable[typ]
		require.True(t, ok, "row %q names a type with no row of its own", key)
		assert.NotEqual(t, bare, prim,
			"row %q maps to the same kind (%s) as the bare type %q, so the pairing "+
				"reaches no field once the position resolves to the shared primitive; "+
				"a format whose kind does not differ must not be a row", key, prim, typ)
	}
}

// TestFormatTable_EveryRowNarrowsItsBareType holds the relation to what the
// table lowers: every (type, format) row must narrow its bare type's row, or an
// allOf redeclaring a bare type with a format would warn of a conflict that
// loses nothing (GitHub #598). A string row answers to the spelling half, a
// numeric row to the value domain alone.
func TestFormatTable_EveryRowNarrowsItsBareType(t *testing.T) {
	t.Parallel()
	for key, prim := range spelling.FormatTable {
		typ, _, paired := strings.Cut(key, "/")
		if !paired {
			continue
		}
		bare, ok := spelling.FormatTable[typ]
		require.True(t, ok, "row %q names a type with no row of its own", key)
		assert.True(t, spelling.Narrows(prim, bare),
			"row %q lowers to %s, which does not narrow the bare %q kind %s", key, prim, typ, bare)
		if typ != "string" {
			assert.True(t, prim.StrictlyWithin(bare),
				"numeric row %q must narrow its bare kind by value domain alone", key)
		}
	}
}

// TestFormatTable_EveryRelatedKindIsSpelledOrAHop holds the domain edges to the
// table: a kind an edge touches is one a row selects, or a listed hop.
func TestFormatTable_EveryRelatedKindIsSpelledOrAHop(t *testing.T) {
	t.Parallel()
	kinds := declaredKinds(t)
	for _, k := range kinds {
		for _, p := range coveringKinds(kinds, k) {
			for _, end := range []ir.PrimKind{k, p} {
				_, hop := hopOnly[end]
				assert.True(t, spelled(end) || hop, "%s on an edge is spelled by no row and is no listed hop", end)
			}
		}
	}
	for k, why := range hopOnly {
		assert.False(t, spelled(k), "hop %s (%s) is spelled by a row, so it is no hop", k, why)
	}
}

// TestRelation_EveryDeclaredKindIsRelatedOrARoot holds relationRoots to the
// declared set: a kind narrows something besides any, or is a listed root.
func TestRelation_EveryDeclaredKindIsRelatedOrARoot(t *testing.T) {
	t.Parallel()
	kinds := declaredKinds(t)
	for _, k := range kinds {
		related := false
		for _, other := range kinds {
			related = related || (other != ir.PrimAny && spelling.Narrows(k, other))
		}
		_, root := relationRoots[k]
		assert.Equal(t, !root, related, "%s must narrow a kind or be a listed root, not both", k)
	}
	for r, why := range relationRoots {
		assert.True(t, r.Valid(), "root %q (%s) must be a declared kind", r, why)
	}
}

// TestRelation_LongestChainFitsTheCap reads the walk's cap from the ir source
// and holds the longest chain the domain edges make to it.
func TestRelation_LongestChainFitsTheCap(t *testing.T) {
	t.Parallel()
	limit, err := irDeclared(t).IntConst("maxDomainDepth")
	require.NoError(t, err)

	kinds := declaredKinds(t)
	var longest func(k ir.PrimKind, depth int) int
	longest = func(k ir.PrimKind, depth int) int {
		require.LessOrEqual(t, int64(depth), limit, "a chain past the cap: a cycle or a cap too low")
		best := 0
		for _, p := range coveringKinds(kinds, k) {
			best = max(best, 1+longest(p, depth+1))
		}
		return best
	}
	chain := 0
	for _, k := range kinds {
		chain = max(chain, longest(k, 0))
	}
	assert.LessOrEqual(t, chain, int(limit))
}

// TestNarrows_HoldsEveryContainment pins the relation both ways: every
// containment holds one way only, a kind never narrows itself, and the
// unrelated pairs hold neither way.
func TestNarrows_HoldsEveryContainment(t *testing.T) {
	t.Parallel()
	holds := [][2]ir.PrimKind{
		{ir.PrimInt8, ir.PrimInt16}, {ir.PrimInt16, ir.PrimInt32}, {ir.PrimInt32, ir.PrimInt64},
		{ir.PrimInt64, ir.PrimInteger}, {ir.PrimUint8, ir.PrimUint16}, {ir.PrimUint16, ir.PrimUint32},
		{ir.PrimUint32, ir.PrimUint64}, {ir.PrimUint64, ir.PrimInteger}, {ir.PrimInteger, ir.PrimDecimal},
		{ir.PrimDecimal128, ir.PrimDecimal}, {ir.PrimDecimal, ir.PrimNumber}, {ir.PrimFloat32, ir.PrimFloat64},
		{ir.PrimFloat64, ir.PrimFloat}, {ir.PrimFloat, ir.PrimNumber}, {ir.PrimDate, ir.PrimString},
		{ir.PrimTime, ir.PrimString}, {ir.PrimDatetimeOffset, ir.PrimString},
		{ir.PrimDuration, ir.PrimString}, {ir.PrimURL, ir.PrimString}, {ir.PrimUUID, ir.PrimString},
		{ir.PrimBytes, ir.PrimString},
		{ir.PrimInt8, ir.PrimNumber}, {ir.PrimUint8, ir.PrimDecimal}, {ir.PrimInt32, ir.PrimInteger},
		{ir.PrimFloat32, ir.PrimNumber}, {ir.PrimUint8, ir.PrimInt16}, {ir.PrimInt16, ir.PrimFloat32},
		{ir.PrimUint16, ir.PrimFloat32}, {ir.PrimInt32, ir.PrimFloat64}, {ir.PrimUint32, ir.PrimFloat64},
		{ir.PrimInteger, ir.PrimFloat}, {ir.PrimUUID, ir.PrimAny}, {ir.PrimInteger, ir.PrimAny},
	}
	for _, p := range holds {
		assert.True(t, spelling.Narrows(p[0], p[1]), "%s narrows %s", p[0], p[1])
		assert.False(t, spelling.Narrows(p[1], p[0]), "%s does not narrow %s", p[1], p[0])
	}

	unrelated := [][2]ir.PrimKind{
		{ir.PrimString, ir.PrimInteger}, {ir.PrimUUID, ir.PrimDatetimeOffset}, {ir.PrimInt32, ir.PrimUint32},
		{ir.PrimDatetime, ir.PrimDatetimeOffset}, {ir.PrimBool, ir.PrimString}, {ir.PrimBool, ir.PrimInteger},
		{ir.PrimBytes, ir.PrimInteger}, {ir.PrimDatetime, ir.PrimString},
	}
	for _, p := range unrelated {
		assert.False(t, spelling.Narrows(p[0], p[1]), "%s does not narrow %s", p[0], p[1])
		assert.False(t, spelling.Narrows(p[1], p[0]), "%s does not narrow %s", p[1], p[0])
	}

	for _, k := range declaredKinds(t) {
		assert.False(t, spelling.Narrows(k, k), "%s is not strictly narrower than itself", k)
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
	rounding := [][2]ir.PrimKind{
		{ir.PrimInt32, ir.PrimFloat32}, {ir.PrimUint32, ir.PrimFloat32}, {ir.PrimInt64, ir.PrimFloat32}, {ir.PrimUint64, ir.PrimFloat32},
		{ir.PrimInt64, ir.PrimFloat64}, {ir.PrimUint64, ir.PrimFloat64},
		{ir.PrimInteger, ir.PrimFloat32}, {ir.PrimInteger, ir.PrimFloat64},
	}
	for _, p := range rounding {
		assert.False(t, spelling.Narrows(p[0], p[1]), "%s is not narrower than %s", p[0], p[1])
		assert.Equal(t, spelling.Overlap, spelling.Relate(p[0], p[1]), "%s against %s", p[0], p[1])
		assert.Equal(t, spelling.Overlap, spelling.Relate(p[1], p[0]), "%s against %s", p[1], p[0])
	}
}

// TestRelate_ClassifiesEveryVerdict covers each verdict, both ways round.
func TestRelate_ClassifiesEveryVerdict(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a, b ir.PrimKind
		want spelling.Relation
		back spelling.Relation
	}{
		{"same", ir.PrimString, ir.PrimString, spelling.Same, spelling.Same},
		{"domain narrowing", ir.PrimInt32, ir.PrimInteger, spelling.ANarrower, spelling.BNarrower},
		{"spelling narrowing", ir.PrimUUID, ir.PrimString, spelling.ANarrower, spelling.BNarrower},
		{"numeric overlap", ir.PrimInt32, ir.PrimUint32, spelling.Overlap, spelling.Overlap},
		{"number kinds overlap", ir.PrimDecimal128, ir.PrimFloat32, spelling.Overlap, spelling.Overlap},
		{"bytes against uuid", ir.PrimBytes, ir.PrimUUID, spelling.Overlap, spelling.Overlap},
		{"two string formats", ir.PrimUUID, ir.PrimDatetimeOffset, spelling.Disjoint, spelling.Disjoint},
		{"string against number", ir.PrimString, ir.PrimInteger, spelling.Disjoint, spelling.Disjoint},
		{"bool against string", ir.PrimBool, ir.PrimString, spelling.Disjoint, spelling.Disjoint},
		{"datetime against a format", ir.PrimDatetime, ir.PrimDate, spelling.Disjoint, spelling.Disjoint},
		{"any is the top", ir.PrimUUID, ir.PrimAny, spelling.ANarrower, spelling.BNarrower},
		{"any against itself", ir.PrimAny, ir.PrimAny, spelling.Same, spelling.Same},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, spelling.Relate(tc.a, tc.b), "%s: %s against %s", tc.name, tc.a, tc.b)
		assert.Equal(t, tc.back, spelling.Relate(tc.b, tc.a), "%s: %s against %s", tc.name, tc.b, tc.a)
	}
}

// TestLookup_ReadsTheTable pins the exported lookup: a known key selects its
// row and an unknown one reports it.
func TestLookup_ReadsTheTable(t *testing.T) {
	t.Parallel()
	prim, ok := spelling.Lookup("integer/int32")
	assert.True(t, ok)
	assert.Equal(t, ir.PrimInt32, prim)

	_, ok = spelling.Lookup("string/password")
	assert.False(t, ok)
}

// coveringKinds returns the kinds immediately above k by value domain: those k
// is strictly within, with no declared kind between, and never the top type.
// It reads the containment through the exported StrictlyWithin, which is what
// the unexported edge table in ir yields.
func coveringKinds(kinds []ir.PrimKind, k ir.PrimKind) []ir.PrimKind {
	var out []ir.PrimKind
	for _, p := range kinds {
		if p == ir.PrimAny || !k.StrictlyWithin(p) {
			continue
		}
		between := false
		for _, m := range kinds {
			between = between || (m != ir.PrimAny && k.StrictlyWithin(m) && m.StrictlyWithin(p))
		}
		if !between {
			out = append(out, p)
		}
	}
	return out
}
