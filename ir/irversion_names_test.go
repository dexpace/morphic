// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// namesGolden holds the description the newest row's names hashes, so a change
// to a name reviews as a diff of it.
const namesGolden = "testdata/names.golden.txt"

// hintGolden is the compiled document whose hints the description lists. A
// position its spec does not write is not pinned, and adding one moves the hash,
// so it lands with a bump (docs/ir/ir-version.md).
const hintGolden = "../testdata/golden/openapi/hint_positions.golden.json"

// namingType is the reflect.Type of ir.Naming.
var namingType = reflect.TypeFor[ir.Naming]()

// TestIRVersion_PinsTheNames holds IRVersion to the names a document renders:
// an emitter derives identifiers from Canonical or Hint, so moving either
// renames SDK symbols with no key of the document moved, and the description
// must hash to the names generations records for IRVersion. A name changed at an
// unmoved version fails here. -update rewrites namesGolden but never the table,
// so it cannot settle that (docs/ir/ir-version.md).
func TestIRVersion_PinsTheNames(t *testing.T) {
	t.Parallel()
	doc, err := irtest.ReadGolden(hintGolden)
	require.NoError(t, err, "go test ./compilers/openapi/e2e -run TestGolden_HintPositions -update writes it")
	witnesses, _ := runeScan()
	desc := describeNames(witnesses, hintsOf(doc))

	if irtest.Update() {
		require.NoError(t, os.WriteFile(namesGolden, []byte(desc), 0o644))
	}
	want, err := os.ReadFile(namesGolden)
	require.NoError(t, err, "go test ./ir -run TestIRVersion -update writes it")
	assert.Empty(t, cmp.Diff(string(want), desc),
		"%s is stale: go test ./ir -run TestIRVersion -update rewrites it, then review its diff", namesGolden)

	sum := sha256.Sum256([]byte(desc))
	for _, finding := range namesFindings(generations, ir.IRVersion, hex.EncodeToString(sum[:])) {
		t.Error(finding)
	}
}

// describeNames renders what a generation's names are: the Unicode tables the
// grammar reads, the canonical form of each ASCII rune between two letters and
// of every name of up to three runes over witnesses, and hints.
//
// The grammar decides at each rune from that rune and its two neighbours, so
// those names reach every decision it makes over the classes the witnesses
// stand for. The ASCII runes pin which runes separate, which one class hides.
// canonicalCases stays out: a row added there changes no name, so it moves no
// hash.
func describeNames(witnesses []rune, hints []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "unicode %s\n\n# every ASCII rune between two letters\n", unicode.Version)
	for r := rune(0); r <= unicode.MaxASCII; r++ {
		writeCanonical(&b, "a"+string(r)+"b")
	}
	b.WriteString("\n# every name of up to three runes, over one rune of each class\n")
	for _, name := range namesOver(witnesses) {
		writeCanonical(&b, name)
	}
	b.WriteString("\n# hints\n")
	for _, hint := range hints {
		b.WriteString(hint + "\n")
	}
	return b.String()
}

// writeCanonical writes one line, quoted so a rune no reader can see is spelled.
func writeCanonical(b *strings.Builder, name string) {
	fmt.Fprintf(b, "%s %s\n", strconv.QuoteToASCII(name), strconv.QuoteToASCII(ir.CanonicalWords(name)))
}

// namesOver returns every string of up to three of witnesses, shortest first.
func namesOver(witnesses []rune) []string {
	n := len(witnesses)
	names := make([]string, 1, 1+n+n*n+n*n*n)
	last := names
	for range 3 {
		next := make([]string, 0, len(last)*len(witnesses))
		for _, prefix := range last {
			for _, r := range witnesses {
				next = append(next, prefix+string(r))
			}
		}
		names = append(names, next...)
		last = next
	}
	return names
}

// runeClasses is how many values classOf can take: a bit for each question.
const runeClasses = 1 << 7

// classOf returns r's class: everything the grammar can ask of a rune, one bit a
// question, whichever of these it asks today, so a change to which it asks is
// seen.
func classOf(r rune) uint8 {
	var class uint8
	for i, holds := range [...]bool{
		unicode.IsLetter(r), unicode.IsDigit(r), unicode.IsMark(r), unicode.IsLower(r),
		unicode.IsUpper(r), unicode.IsTitle(r), unicode.ToLower(r) != r,
	} {
		if holds {
			class |= 1 << i
		}
	}
	return class
}

// runeScan reads every rune once, at a cost the race detector makes worth
// sharing, and returns the first printable rune of each class and how many
// classes there are. The witnesses are derived rather than listed, so a class
// the Unicode tables gain is witnessed unasked.
var runeScan = sync.OnceValues(func() ([]rune, int) {
	var seen, witnessed [runeClasses]bool
	var witnesses []rune
	classes := 0
	for r := rune(0); r <= unicode.MaxRune; r++ {
		c := classOf(r)
		if !seen[c] {
			seen[c] = true
			classes++
		}
		if !witnessed[c] && unicode.IsPrint(r) {
			witnessed[c] = true
			witnesses = append(witnesses, r)
		}
	}
	return witnesses, classes
})

// TestRuneScan_WitnessesEveryClass pins that no class is left out for want of a
// printable rune, which would leave the grammar's answer on it unpinned.
func TestRuneScan_WitnessesEveryClass(t *testing.T) {
	t.Parallel()
	witnesses, classes := runeScan()
	assert.Len(t, witnesses, classes)
}

// hintsOf returns one sorted line per Naming in doc that holds a hint, its path
// beside the hint, so a line names the position it pins.
func hintsOf(doc *ir.Document) []string {
	var hints []string
	ir.WalkValues(doc, ir.DocumentPath, ir.VisitFunc(func(v reflect.Value, at ir.WalkPath) bool {
		if v.Type() != namingType {
			return true
		}
		if hint := v.FieldByName("Hint").String(); hint != "" {
			hints = append(hints, at.String()+" "+hint)
		}
		return false
	}))
	slices.Sort(hints)
	return hints
}

// TestHintsOf_ListsEveryHintInItsPlace pins the reader on a Naming in a registry
// and one in a slice, and that a Naming with no hint draws no line.
func TestHintsOf_ListsEveryHintInItsPlace(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Types: ir.TypeRegistry{
		"t/a": &ir.Model{ID: "t/a", Name: ir.Naming{Hint: "a_hint"}, Properties: []ir.Property{
			{ID: "p/a", Name: ir.Naming{Source: "x", Canonical: "x"}},
			{ID: "p/b", Name: ir.Naming{Hint: "b_hint"}},
		}},
	}}
	assert.Equal(t, []string{
		"doc.Types[t/a].Name a_hint",
		"doc.Types[t/a].Properties[1].Name b_hint",
	}, hintsOf(doc))
}

// namesFindings returns what keeps rows from pinning a build at version whose
// names hash to names. A build ahead of the table has no row to compare with,
// and pinFindings already says to append one.
func namesFindings(rows []generation, version, names string) []string {
	if len(rows) == 0 || rows[len(rows)-1].version != version {
		return nil
	}
	last := rows[len(rows)-1]
	switch last.names {
	case names:
		return nil
	case "":
		return []string{fmt.Sprintf("%s records no names: set names: %q on its row", version, names)}
	default:
		next := nextGeneration(version)
		return []string{fmt.Sprintf("the names a document renders changed, which renames generated symbols, "+
			"but IRVersion is still %s, published with other names (docs/ir/ir-version.md). Set IRVersion to %s, "+
			"append its row with names: %q and the meaning that changed, and say what changed under %s in "+
			"docs/ir/version-history.md. A row this branch appended takes the hash in place, "+
			"and a position only added to the hint spec lands with whichever change bumps next",
			version, next, names, next)}
	}
}

// TestNamesFindings plants each way a build can disagree with its table and
// requires the finding that says what to do; a build that agrees has none.
func TestNamesFindings(t *testing.T) {
	t.Parallel()
	pinned := []generation{{version: "0.3.0", format: 1, shape: shapeA, names: shapeB}}
	for _, tc := range []struct {
		name    string
		rows    []generation
		version string
		names   string
		want    string // "" means no finding
	}{
		{"the pinned names", pinned, "0.3.0", shapeB, ""},
		{"a build ahead of the table", pinned, "0.4.0", shapeA, ""},
		{"an empty table", nil, "0.3.0", shapeB, ""},
		{"a row that never pinned", []generation{{version: "0.3.0", format: 1, shape: shapeA}}, "0.3.0", shapeB,
			`0.3.0 records no names: set names: "` + shapeB + `" on its row`},
		{"a name changed at an unmoved version", pinned, "0.3.0", shapeA,
			`the names a document renders changed, which renames generated symbols, but IRVersion is still 0.3.0, ` +
				`published with other names (docs/ir/ir-version.md). Set IRVersion to 0.4.0, append its row with ` +
				`names: "` + shapeA + `" and the meaning that changed, and say what changed under 0.4.0 in ` +
				`docs/ir/version-history.md. A row this branch appended takes the hash in place, ` +
				`and a position only added to the hint spec lands with whichever change bumps next`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := namesFindings(tc.rows, tc.version, tc.names)
			if tc.want == "" {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, []string{tc.want}, got)
		})
	}
}
