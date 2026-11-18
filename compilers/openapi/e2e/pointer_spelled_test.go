// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"cmp"
	"maps"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// spelledHop is one step from an object of the rule table to an object it
// holds: what a document writes before and after the object held.
type spelledHop struct {
	open, shut string
	to         oas.Kind
}

// spelledHops lists the steps from o to each object it holds: under a field or
// a patterned key, in place, as a map's entry or as a list's item.
func spelledHops(o oas.Object) []spelledHop {
	held := func(key string, shape oas.Shape, of oas.Kind) (spelledHop, bool) {
		switch shape {
		case oas.Nested:
			return spelledHop{"{" + key + ": ", "}", of}, true
		case oas.Map:
			return spelledHop{"{" + key + ": {k: ", "}}", of}, true
		case oas.List:
			return spelledHop{"{" + key + ": [", "]}", of}, true
		default:
			return spelledHop{}, false
		}
	}
	var out []spelledHop
	for _, f := range o.Fields {
		if h, ok := held(f.Name, f.Shape, f.Of); ok {
			out = append(out, h)
		}
	}
	for _, p := range o.Patterned {
		key := "k"
		switch {
		case o.Kind == oas.Responses:
			key = "'200'"
		case strings.HasPrefix(p.Name, "/"):
			key = "/p"
		case strings.Contains(p.Name, "{"):
			key = "'{$url}'"
		}
		if h, ok := held(key, p.Shape, p.Of); ok {
			out = append(out, h)
		}
	}
	return out
}

// spelledReach returns the steps from the object of kind start to each object
// the rule table reaches from it, the fewest to each.
func spelledReach(start oas.Kind) map[oas.Kind][]spelledHop {
	out := map[oas.Kind][]spelledHop{start: {}}
	for queue := []oas.Kind{start}; len(queue) > 0; queue = queue[1:] {
		o, _ := oas.Table().Object(queue[0])
		for _, h := range spelledHops(o) {
			if _, seen := out[h.to]; seen || h.to == oas.Reference {
				continue
			}
			out[h.to] = append(slices.Clone(out[queue[0]]), h)
			queue = append(queue, h.to)
		}
	}
	return out
}

// spelledWithin writes inner, an object's text, at the end of hops.
func spelledWithin(hops []spelledHop, inner string) string {
	for _, h := range slices.Backward(hops) {
		inner = h.open + inner + h.shut
	}
	return inner
}

// spelledMany writes n entries of a list of entry, or of a mapping where
// keyed.
func spelledMany(n int, keyed bool, entry string) string {
	items := make([]string, n)
	for i := range items {
		items[i] = entry
		if keyed {
			items[i] = "k" + strconv.Itoa(i) + ": " + entry
		}
	}
	if keyed {
		return "{" + strings.Join(items, ", ") + "}"
	}
	return "[" + strings.Join(items, ", ") + "]"
}

// spelledCase is a document written around a key: every pointer through the
// key grows with it, so what a compile allocates more for a longer key counts
// the pointers it spells through it.
type spelledCase struct {
	name string
	doc  func(key string) string
}

// The entries a case writes under its key: of an object's field, which a
// compile names, and of a value it keeps raw, which costs it nothing an entry.
const (
	spelledEntries    = 300
	spelledRawEntries = 800
)

// spelledShapes lists the values a field is given, by name: as its shape reads
// it first, then every other, so a field is held whatever a document writes
// there.
func spelledShapes(shape oas.Shape, all bool) map[string]string {
	of := map[string]string{
		"a map of strings": spelledMany(spelledEntries, true, "s"), "a map of objects": spelledMany(spelledEntries, true, "{}"),
		"a list of strings": spelledMany(spelledEntries, false, "s"), "a list of objects": spelledMany(spelledEntries, false, "{}"),
		"a map of lists": spelledMany(spelledEntries, true, "[a, a]"),
	}
	if all {
		return of
	}
	name := "a map of strings"
	switch shape {
	case oas.List:
		name = "a list of objects"
	case oas.Map:
		name = "a map of objects"
	case oas.StringList, oas.RawList:
		name = "a list of strings"
	default:
	}
	return map[string]string{name: of[name]}
}

// spelledLeaves lists values a raw value holds many of, by name: the first
// three wherever one stands, the rest too where all is set. Each is one a
// reader could place a finding at, or read otherwise than as written.
func spelledLeaves(all bool) map[string]string {
	leaves := map[string]string{"a key written twice": "{a: 1, a: 2}", "a merge": "{<<: {a: 1}, a: 2}", "a mapping": "{a: 1, b: 2}"}
	if !all {
		return leaves
	}
	maps.Copy(leaves, map[string]string{
		"binary": "!!binary aGk=", "infinity": ".inf", "hex": "0x10", "a huge number": "1e400", "a null key": "{~: 1}",
		"a timestamp": "2001-01-01", "a set": "!!set {a, b}", "an integer key": "{1: a}", "a boolean key": "{true: a}",
		"a tag": "!foo bar", "a long integer": "123456789012345678901234567890", "an empty key": "{'': 1}",
		"not a number": ".nan", "a $ref": "{$ref: '#/nowhere'}", "an ordered map": "!!omap [a: 1]", "a tagged string": "!!str 5",
		"a nested value": "{a: [{b: {c: [1, {d: 2, e: [3]}]}}]}",
	})
	return leaves
}

// spelledRawSpots lists where a document keeps a value raw, the value at `@`.
var spelledRawSpots = map[string]string{
	"a root extension":      "x-e: @\n",
	"a schema's example":    "components: {schemas: {S: {example: @}}}\n",
	"a schema's default":    "components: {schemas: {S: {default: @}}}\n",
	"a schema's enum":       "components: {schemas: {S: {enum: [@]}}}\n",
	"a schema's const":      "components: {schemas: {S: {const: @}}}\n",
	"a schema's examples":   "components: {schemas: {S: {examples: [@]}}}\n",
	"a schema's extension":  "components: {schemas: {S: {x-e: @}}}\n",
	"an example's value":    "components: {examples: {E: {value: @}}}\n",
	"a parameter's example": "components: {parameters: {P: {name: n, in: query, schema: {type: string}, example: @}}}\n",
	"a link's values":       "components: {links: {L: {operationId: o, requestBody: @, parameters: {p: @}}}}\n",
	"an unknown field":      "components: {schemas: {S: {zz: @}}}\ninfo2: @\n",
	"a server variable":     "servers: [{url: u, variables: {v: {default: d, x-e: @}}}]\n",
}

// spelledCases lists the documents of a version: each field of each object a
// component's $ref reaches, once, under a raw mount named by the key; a raw value
// inside each object of the table, the key inside it; and each raw spot
// holding each leaf under the key. all widens the values and the leaves.
func spelledCases(version string, all bool) []spelledCase {
	head := "openapi: " + version + "\ninfo: {title: t, version: '1'}\n"
	var cases []spelledCase
	components, _ := oas.Table().Object(oas.Components)
	fielded := map[string]bool{}
	for _, c := range components.Fields {
		for kind, hops := range spelledReach(c.Of) {
			o, _ := oas.Table().Object(kind)
			for _, f := range o.Fields {
				if c.Shape != oas.Map || fielded[o.Name+" "+f.Name] {
					continue
				}
				fielded[o.Name+" "+f.Name] = true
				for shape, value := range spelledShapes(f.Shape, all) {
					cases = append(cases, spelledCase{name: c.Name + " " + o.Name + " " + f.Name + " as " + shape,
						doc: func(key string) string {
							return head + key + ": " + spelledWithin(hops, "{"+f.Name+": "+value+"}") + "\n" +
								"components: {" + c.Name + ": {A: {$ref: '#/" + key + "'}}}\n"
						}})
				}
			}
		}
	}
	for kind, hops := range spelledReach(oas.Document) {
		o, _ := oas.Table().Object(kind)
		names := []string{"x-e", "zz"}
		for _, f := range o.Fields {
			if f.Shape == oas.Any || f.Shape == oas.RawMap || f.Shape == oas.RawList {
				names = append(names, f.Name)
			}
		}
		for _, name := range names {
			cases = append(cases, spelledCase{name: "inside " + o.Name + " " + name, doc: func(key string) string {
				body := spelledWithin(hops, "{"+name+": {"+key+": "+spelledMany(spelledRawEntries, true, "1")+"}}")
				return head + strings.TrimSuffix(strings.TrimPrefix(body, "{"), "}") + "\n"
			}})
		}
	}
	for spot, body := range spelledRawSpots {
		for leaf, text := range spelledLeaves(all) {
			cases = append(cases, spelledCase{name: spot + " holding " + leaf, doc: func(key string) string {
				return head + strings.ReplaceAll(body, "@", "{"+key+": "+spelledMany(spelledRawEntries, false, text)+"}")
			}})
		}
	}
	slices.SortFunc(cases, func(a, b spelledCase) int { return cmp.Compare(a.name, b.name) })
	return cases
}

// compileBytes returns the bytes a compile of src allocates.
func compileBytes(t *testing.T, src string) int {
	t.Helper()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, _, err := openapi.New().Compile(t.Context(), []compilers.Source{openapitest.SourceOf(src)}, compilers.Options{})
	runtime.ReadMemStats(&after)
	require.NoError(t, err)
	return int(after.TotalAlloc - before.TotalAlloc)
}

// The keys a case is written around, and what a compile may allocate for each
// byte the long one adds: spelledPerNode pointers for each node the total
// names through the key, and spelledPerKey for the key itself. A compile
// spells a named node's pointer under a hundred times, in the walks of the
// binder, the rule table and the lowering and in the IDs.
const (
	spelledShortKey = 20
	spelledLongKey  = 920
	spelledPerNode  = 200
	spelledPerKey   = 300
)

// spelledMinors lists a version of each minor the rule table tells apart, the
// last the one whose cases are widened.
var spelledMinors = []string{"3.0.3", "3.1.1", "3.2.0"}

// TestCompile_SpellsNoPointerTheNamedTotalLeavesOut holds a whole compile to
// the total the pointer bound's second weighing takes
// (docs/compilers/openapi.md#source-budgets): for each case spelledCases
// lists, what a compile allocates more for a longer key is within a constant
// of what the total grows by, so no walk spells a pointer at each entry of a
// value the total counts by its root. A short run holds the last minor alone.
// It does not run in parallel, since the count is process-wide.
func TestCompile_SpellsNoPointerTheNamedTotalLeavesOut(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(previous) })
	for i, version := range spelledMinors {
		last := i == len(spelledMinors)-1
		if last || !testing.Short() {
			spellsWithinTheTotal(t, spelledCases(version, last && !testing.Short()))
		}
	}
}

// spellsWithinTheTotal holds each of cases to what its total grows by, the
// fewest of three compiles where one is past. A case whose total is not taken
// is left to the first total.
func spellsWithinTheTotal(t *testing.T, cases []spelledCase) {
	t.Helper()
	short, long := strings.Repeat("K", spelledShortKey), strings.Repeat("K", spelledLongKey)
	taken := 0
	for _, c := range cases {
		narrow, wide := c.doc(short), c.doc(long)
		low, whole := bind.NamedPointerBytes(t.Context(), openapitest.YAMLDocument(t, narrow), 1<<22)
		high, alsoWhole := bind.NamedPointerBytes(t.Context(), openapitest.YAMLDocument(t, wide), 1<<22)
		if !whole || !alsoWhole {
			continue
		}
		taken++
		allowed := (spelledPerNode*(high-low)/(spelledLongKey-spelledShortKey) + spelledPerKey) * (spelledLongKey - spelledShortKey)
		spent := compileBytes(t, wide) - compileBytes(t, narrow)
		for range 2 {
			if spent <= allowed {
				break
			}
			spent = min(spent, compileBytes(t, wide)-compileBytes(t, narrow))
		}
		assert.LessOrEqual(t, spent, allowed, "%s: the pointers spelled through the key, for %d bytes the total names", c.name, high-low)
	}
	require.Greater(t, taken, len(cases)*9/10, "the total is taken for the cases")
}
