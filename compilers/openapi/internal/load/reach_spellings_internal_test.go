package load

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/ir"
)

// schemasDoc wraps components.schemas entries, written at four spaces of
// indent, in an OpenAPI 3.1 skeleton.
func schemasDoc(schemas string) string {
	return "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n" + schemas + "\n"
}

// onlyReachCrashFixtures each crash the load pipeline without its reach check,
// and each needs a reading or a piece of resolver state the pre-parse scan does
// not have: where the library splits and decodes a $ref, what an $id normalizes
// to, what a merge key or an alias supplies, and which documents a /$defs/
// pointer the resolver reads itself moves a reference into. A check that
// misreads any of them lets the document reach the resolver.
// TestReachCycle_RefusesCrashesOnlyReachCanSee requires reach itself to refuse
// each, and that the pipeline still overflows on it.
var onlyReachCrashFixtures = []struct{ name, schemas string }{
	{"a percent-encoded $ in a $defs pointer", `    A: {$defs: {n: {$ref: "#/%24defs/n"}}}`},
	{"a percent-encoded slash after $defs", `    A: {$defs: {n: {$ref: "#/$defs%2Fn"}}}`},
	{"a second # after a $defs pointer", `    A: {$defs: {n: {$ref: "#/$defs/n#x"}}}`},
	{"an $id named through a '.' reference", `    A: {$id: "http://x.test/y/", $ref: "."}`},
	{"an $id named through a trailing '..'", `    A: {$id: "http://x.test/y/", $ref: "foo/.."}`},
	{"an $id spelled with a percent-encoded space", `    A: {$id: "http://x.test/y/a%20b", $ref: "http://x.test/y/a b"}`},
	{"an $id and a $ref with no path of their own", `    A: {$id: "?x=1", $ref: "?x=1"}`},
	{"an $id with no path of its own inside one with a path",
		"    A:\n      $id: \"http://x.test/a\"\n      properties:\n        p: {$id: \"?x=1\", $ref: \"http://x.test/a?x=1\"}"},
	{"a $defs pointer in an extension, which the resolver reads itself",
		"    A:\n      $defs:\n        n:\n          $ref: \"#/x-s\"\n          $defs: {m: {$ref: \"#\"}}\nx-s: {$ref: \"#/$defs/m\"}"},
	{"a merge key supplies the $anchor", "    Base: &b {$anchor: a}\n    A: {<<: *b, $ref: \"#a\"}"},
	{"a merge key supplies a $defs reference", "    Base: &b {$ref: \"#/$defs/n\"}\n    A:\n      $defs:\n        n: {<<: *b}"},
	{"a merge key supplies the $id", "    Base: &b {$id: \"http://x.test/m\"}\n    A: {<<: *b, $ref: \"http://x.test/m\"}"},
	{"an alias makes one anchored node the root of two schemas",
		"    B:\n      $defs:\n        n:\n          $ref: \"#a\"\n          properties:\n            q: &q {$ref: \"#/components/schemas/B/$defs/n\", $anchor: a}\n    A: *q"},
	{"an aliased node's own anchor reference needs every document that owns it",
		"    A: &q {$anchor: a, $ref: \"#b\"}\n    B:\n      $defs: {n: {$anchor: b, $ref: \"#a\"}}\n      properties: {q: *q}"},
	{"the same, with the alias declared second",
		"    B:\n      $defs: {n: {$anchor: b, $ref: \"#a\"}}\n      properties: {q: &q {$anchor: a, $ref: \"#b\"}}\n    A: *q"},
}

func TestReachCycle_RefusesCrashesOnlyReachCanSee(t *testing.T) {
	t.Parallel()
	for _, tc := range onlyReachCrashFixtures {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := schemasDoc(tc.schemas)
			assertRefusedByReach(t, spec)

			path := filepath.Join(t.TempDir(), "spec.yaml")
			require.NoError(t, os.WriteFile(path, []byte(spec), 0o600))
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			assert.True(t, reachFuzzSubprocessCrashed(ctx, t, path),
				"the fixture no longer crashes the pipeline without the check, so refusing it protects nothing")
		})
	}
}

// heldOutCrashFixtures crash the raw resolver, which reads a "#/$defs/..."
// pointer against whichever document its chain carries it to, so a plain
// pointer further along is read from there too. The pipeline never reads one
// of a model schema so: load holds each such reference out of the resolver's
// pass (withDefsHeld), and every other pointer is read from the root.
var heldOutCrashFixtures = []struct{ name, schemas string }{
	{"drift reaches a reference only through an alias",
		"    Shared: &s {$ref: \"#/properties/x\"}\n    Top:\n      $defs: {d: {properties: {q: *s}}}\n      properties:\n        x: {$ref: \"#/$defs/d/properties/q\"}"},
	{"drift reaches a reference only through a merge key",
		"    Shared: &s {$ref: \"#/properties/x\"}\n    Top:\n      $defs: {d: {properties: {q: {<<: *s}}}}\n      properties:\n        x: {$ref: \"#/$defs/d/properties/q\"}"},
	{"an empty fragment lands on an ancestor the resolver searched",
		"    Top:\n      $defs:\n        n: {$ref: \"#/$defs/m\"}\n        m: {$ref: \"#\"}\n      properties:\n        x: {$ref: \"#/$defs/n\"}\n        p: {$ref: \"#/properties/q\"}\n        q: {$ref: \"#/properties/p\"}"},
}

// TestReachCycle_LeavesWhatHoldingDefsOutOfTheResolverSurvives requires of
// each such fixture that reach leaves it alone and that the pipeline, with the
// check off, does not overflow: refusing it would be a false refusal, and
// leaving it unprotected only holds while load keeps the $defs references of
// the model out of the resolver's own pass.
func TestReachCycle_LeavesWhatHoldingDefsOutOfTheResolverSurvives(t *testing.T) {
	t.Parallel()
	for _, tc := range heldOutCrashFixtures {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := schemasDoc(tc.schemas)
			assertNotRefusedByReach(t, spec)

			path := filepath.Join(t.TempDir(), "spec.yaml")
			require.NoError(t, os.WriteFile(path, []byte(spec), 0o600))
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			assert.False(t, reachFuzzSubprocessCrashed(ctx, t, path),
				"the pipeline overflowed, so the reference the resolver reads for itself is not held out")
		})
	}
}

// parityFixtures are references the library reads as naming something else than
// they look like to a byte-for-byte reader. The resolver survives each, and so
// must the check: it takes the library's own reading, not a wider one.
var parityFixtures = []struct{ name, schemas string }{
	{"a second # makes the anchor's name include it", `    A: {$anchor: a, $ref: "#a#x"}`},
	{"a space before the anchor is part of its name", `    A: {$anchor: a, $ref: "# a"}`},
	{"an anchor name is not percent-decoded", `    A: {$anchor: a, $ref: "#%61"}`},
	{"a $defs pointer past a second # is not a $defs pointer", `    A: {$defs: {n: {$ref: "#/components/schemas/B#/$defs/n"}}}` + "\n    B: {type: string}"},
	{"an anchor fragment against an $id names the anchor, not the schema", `    A: {$id: "http://x.test/a", $ref: "http://x.test/a#b"}`},
	{"a sequence index with a leading zero names no element", `    A: {$defs: {n: {allOf: [{$ref: "#/$defs/n/allOf/00"}]}}}`},
	{"a sequence index with a sign names no element", `    A: {$defs: {n: {allOf: [{$ref: "#/$defs/n/allOf/+0"}]}}}`},
}

func TestReachCycle_LeavesWhatTheLibraryReadsAsAnotherName(t *testing.T) {
	t.Parallel()
	for _, tc := range parityFixtures {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertNotRefusedByReach(t, schemasDoc(tc.schemas))
		})
	}
}

// TestReachWithin_WarnsWhenItCouldNotRuleACycleOut pins the budget's report: a
// document the model cannot finish is not refused, since every bound in this
// stage reports diag.CycleScanFailed instead, and the compile goes on.
func TestReachWithin_WarnsWhenItCouldNotRuleACycleOut(t *testing.T) {
	t.Parallel()
	doc, root := buildDoc(t, anchoredReferences(50))

	whole, found := reachWithin(t.Context(), maxReachWork, scan.InSource(0), root, doc)
	assert.False(t, found, "within budget the document is clean: %v", whole)

	d, found := reachWithin(t.Context(), 10, scan.InSource(4), root, doc)
	require.True(t, found, "past the budget it says so")
	assert.Equal(t, ir.SeverityWarning, d.Severity)
	assert.Equal(t, diag.CycleScanFailed, d.Code)
	assert.Contains(t, d.Message, "protection is incomplete")
	assert.Equal(t, 4, d.Provenance.Source, "the warning names the source")
}

// TestReachWithin_WarnsOnMergeKeysNestedPastTheViewsBound pins the other
// incompleteness: the view stops expanding merge keys at nodeview.MergeDepthLimit
// and says so, and the model built on what it dropped says the same.
func TestReachWithin_WarnsOnMergeKeysNestedPastTheViewsBound(t *testing.T) {
	t.Parallel()
	assertNotRefusedByReach(t, mergeChain(nodeview.MergeDepthLimit/2))
	doc, root := buildDoc(t, mergeChain(nodeview.MergeDepthLimit+6))
	d, found := reachCycle(t.Context(), scan.InSource(0), root, doc)
	require.True(t, found)
	assert.Equal(t, ir.SeverityWarning, d.Severity)
	assert.Equal(t, diag.CycleScanFailed, d.Code)
}

// mergeChain is depth components each merging the one before, down to one that
// declares an $anchor, so the gate runs the check.
func mergeChain(depth int) string {
	var sb strings.Builder
	sb.WriteString("    M0: &m0 {$anchor: z}\n")
	for i := 1; i <= depth; i++ {
		fmt.Fprintf(&sb, "    M%d: &m%d {<<: *m%d}\n", i, i, i-1)
	}
	return schemasDoc(strings.TrimRight(sb.String(), "\n"))
}

// anchoredReferences is one schema of n properties, each a $ref to an $anchor
// another property declares by name.
func anchoredReferences(n int) string {
	var sb strings.Builder
	sb.WriteString("    Big:\n      properties:\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "        p%d: {$anchor: a%d, type: string}\n        q%d: {$ref: '#a%d'}\n", i, i, i, i)
	}
	return schemasDoc(strings.TrimRight(sb.String(), "\n"))
}

// workShapes are documents whose over-approximated graph could be quadratic in
// their size, each repeating a block n times. The later ones share one lookup
// among many references: one pointer spelled many ways, many pointers below one
// $defs name every schema declares, empty fragments a /$defs/ target moves,
// many URIs naming one duplicated anchor, one schema an alias puts in many
// documents, and declarations nested in one another.
var workShapes = []struct {
	name  string
	build func(n int) string
}{
	{"one spelling everywhere", func(n int) string {
		var sb strings.Builder
		for i := 0; i < n; i++ {
			fmt.Fprintf(&sb, "    S%d: {$defs: {a: {$ref: '#/$defs/b'}, b: {type: string}}}\n", i)
		}
		return schemasDoc(strings.TrimRight(sb.String(), "\n"))
	}},
	{"a distinct spelling in each", func(n int) string {
		var sb strings.Builder
		for i := 0; i < n; i++ {
			fmt.Fprintf(&sb, "    S%d: {$defs: {k%d: {type: string}}, properties: {p: {$ref: '#/$defs/k%d'}}}\n", i, i, i)
		}
		return schemasDoc(strings.TrimRight(sb.String(), "\n"))
	}},
	{"an anchor per reference in one schema", anchoredReferences},
	{"a layered chain", func(n int) string {
		var sb strings.Builder
		for j := 0; j < n/6+1; j++ {
			fmt.Fprintf(&sb, "    M%d:\n      $defs:\n", j)
			for k := 0; k < 5; k++ {
				fmt.Fprintf(&sb, "        L%d: {$ref: '#/$defs/L%d'}\n", k, k+1)
			}
			sb.WriteString("        L5: {type: string}\n")
		}
		return schemasDoc(strings.TrimRight(sb.String(), "\n"))
	}},
	{"one pointer spelled a new way in each", func(n int) string {
		return repeated(n, "    S%d: {$defs: {b: {type: string}}, properties: {p: {$ref: '#/$defs/b#%d'}}}\n")
	}},
	{"a distinct pointer below one shared $defs name", func(n int) string {
		return repeated(n, "    S%d: {$defs: {b: {type: string}}, properties: {p: {$ref: '#/$defs/b/x%d'}}}\n")
	}},
	{"a drifted empty fragment spelled a new way in each", func(n int) string {
		return repeated(n, "    S%d: {$defs: {b: {properties: {p: {$ref: '##%d'}}}}, properties: {q: {$ref: '#/$defs/b'}}}\n")
	}},
	{"a new URI to one duplicated anchor in each", func(n int) string {
		var sb strings.Builder
		sb.WriteString("    Big:\n      properties:\n")
		for i := 0; i < n; i++ {
			fmt.Fprintf(&sb, "        p%d: {$anchor: a, type: string}\n        q%d: {$ref: 'u%d#a'}\n", i, i, i)
		}
		return schemasDoc(strings.TrimRight(sb.String(), "\n"))
	}},
	{"one schema aliased into a new component in each", func(n int) string {
		var sb strings.Builder
		sb.WriteString("    Q: &q {type: string, example: {$anchor: z}}\n")
		for i := 0; i < n; i++ {
			fmt.Fprintf(&sb, "    C%d: {properties: {p: *q}}\n", i)
		}
		return schemasDoc(strings.TrimRight(sb.String(), "\n"))
	}},
	{"declarations nested in an extension", func(n int) string {
		return "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\nx-deep: " +
			strings.Repeat("{$anchor: a, k: ", n) + "1" + strings.Repeat("}", n) +
			"\ncomponents:\n  schemas:\n    A: {$ref: '#a'}\n"
	}},
}

// repeated fills block, whose two verbs both take the copy's index, n times
// into a schemas document.
func repeated(n int, block string) string {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, block, i, i)
	}
	return schemasDoc(strings.TrimRight(sb.String(), "\n"))
}

// workSpent runs the whole check over spec and returns the work it recorded.
func workSpent(t *testing.T, spec string) int {
	t.Helper()
	doc, root := buildDoc(t, spec)
	r, starts := newReach(t.Context(), maxReachWork, root, doc)
	state := map[vertex]int{}
	for _, start := range starts {
		r.cycles(start, state)
	}
	require.False(t, r.incomplete(), "the shape fits the budget")
	return r.budget.spent
}

// TestReach_WorkIsLinearInTheDocument proves the property the check's cost
// rests on without a clock: doubling a document at most doubles the work it
// records. Work was quadratic here before references of one class were taken
// as one vertex, a lookup many classes make as one set, a pointer prefix
// indexed once, declarations indexed by name and every climb memoized: 2000
// copies of the first shape cost 4 million steps.
func TestReach_WorkIsLinearInTheDocument(t *testing.T) {
	t.Parallel()
	for _, shape := range workShapes {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()
			small, large := workSpent(t, shape.build(500)), workSpent(t, shape.build(1000))
			assert.Positive(t, small)
			assert.LessOrEqual(t, large, 2*small+32,
				"doubling the document must not more than double the work: %d to %d", small, large)
		})
	}
}

// TestNewReach_ChargesEveryPhase pins that the budget sees the work of each
// phase, so a document cannot spend time the bound does not count: the index
// pays for every pair a merge key supplies, the model's walk for every schema,
// the grouping for every scope it files a declaration under, and the search
// marks, the drift and the search for their own steps.
func TestNewReach_ChargesEveryPhase(t *testing.T) {
	t.Parallel()
	const copies, keys = 40, 50
	var sb strings.Builder
	sb.WriteString("    Big: &big {")
	for k := 0; k < keys; k++ {
		fmt.Fprintf(&sb, "k%d: {type: string}, ", k)
	}
	sb.WriteString("$anchor: a}\n")
	for i := 0; i < copies; i++ {
		fmt.Fprintf(&sb, "    M%d: {<<: *big, $defs: {d: {$ref: '#'}}, properties: {p: {$ref: '#/$defs/d'}}}\n", i)
	}
	doc, root := buildDoc(t, schemasDoc(strings.TrimRight(sb.String(), "\n")))
	b := &budget{limit: maxReachWork}
	r := emptyReach(root, b)
	phase := func(run func()) int {
		before := b.spent
		run()
		return b.spent - before
	}

	var declared, defsRefs []*yaml.Node
	assert.GreaterOrEqual(t, phase(func() { declared, defsRefs = r.tree.index() }), copies*keys,
		"every merged pair is charged")
	var starts []*yaml.Node
	assert.GreaterOrEqual(t, phase(func() { starts = r.collect(t.Context(), doc) }), len(r.schemas))
	assert.Positive(t, phase(func() {
		for _, d := range declared {
			r.scopesOf(d)
		}
	}), "the climb to each declaration's scope")
	assert.GreaterOrEqual(t, phase(func() { r.group(declared) }), 2*len(declared),
		"each declaration is filed under its own document and the whole tree")
	assert.Positive(t, phase(func() { r.markSearched(defsRefs) }))
	assert.Positive(t, phase(func() { r.drift(defsRefs) }))
	assert.Positive(t, phase(func() {
		state := map[vertex]int{}
		for _, start := range starts {
			r.cycles(start, state)
		}
	}))
}

// TestScopesOf_VisitsASharedAncestorOnce pins scopesOf for a declaring mapping
// outside the model that an alias puts under two parents of one schema: both
// climbs end at the schema, which is reported once.
func TestScopesOf_VisitsASharedAncestorOnce(t *testing.T) {
	t.Parallel()
	doc, root := buildDoc(t, schemasDoc(`    A: {example: {p: &x {$anchor: z}}, default: {q: *x}}`))
	r, _ := newReach(t.Context(), maxReachWork, root, doc)
	schema := r.tree.walk(r.tree.root, []string{"components", "schemas", "A"})
	shared := r.tree.walk(schema, []string{"example", "p"})
	require.NotNil(t, shared)
	require.Len(t, r.tree.parents[shared], 2, "the alias gives the node a second parent")
	require.NotContains(t, r.scope, shared, "the node is outside the model, so its scope is its ancestors'")

	assert.Equal(t, []*yaml.Node{schema}, r.scopesOf(shared))
}

// TestLookups_ReferenceNamingNothingLandsNowhere drives lookups' own early
// return: a $ref of whitespace has no URI, no '#' and no anchor.
func TestLookups_ReferenceNamingNothingLandsNowhere(t *testing.T) {
	t.Parallel()
	doc, root := buildDoc(t, schemasDoc(`    A: {type: string}`))
	r, _ := newReach(t.Context(), maxReachWork, root, doc)
	assert.Empty(t, r.lookups(refClass{id: r.intern("   ")}))
}

// TestLookups_AnUndriftedEmptyFragmentHasNoDocumentToLandOn: an empty fragment
// has no root fallback the way a pointer does.
func TestLookups_AnUndriftedEmptyFragmentHasNoDocumentToLandOn(t *testing.T) {
	t.Parallel()
	doc, root := buildDoc(t, schemasDoc(`    A: {$ref: "#"}`))
	r, _ := newReach(t.Context(), maxReachWork, root, doc)
	assert.Empty(t, r.lookups(refClass{id: r.intern("#")}))
	assert.NotEmpty(t, r.lookups(refClass{id: r.intern("#"), drifted: true}), "a drifted one lands on A")
}

// TestLookups_ChargeEveryStep pins that a lookup pays for what it reads: a
// pointer from the root or from any node per token, and the document set an
// empty fragment lands in per schema it filters.
func TestLookups_ChargeEveryStep(t *testing.T) {
	t.Parallel()
	const depth = 100
	path := strings.Repeat("/a", depth)
	chain := strings.Repeat("{a: ", depth) + "1" + strings.Repeat("}", depth)
	spec := schemasDoc("    A: {$ref: '#/$defs/n', $defs: {n: {type: string}}}\n    B: {$ref: '#'}") + "x-c: {$defs: {b: " + chain + "}}\n"
	doc, root := buildDoc(t, spec)
	r, _ := newReach(t.Context(), maxReachWork, root, doc)
	spent := func(c refClass) int {
		r.docs = nil
		before := r.budget.spent
		r.lookups(c)
		return r.budget.spent - before
	}

	assert.GreaterOrEqual(t, spent(refClass{id: r.intern("#/x-c/$defs/b" + path)}), depth, "a pointer from the root")
	assert.GreaterOrEqual(t, spent(refClass{id: r.intern("#/$defs/b" + path)}), depth, "a pointer from any node")
	assert.GreaterOrEqual(t, spent(refClass{id: r.intern("#"), drifted: true}), len(r.schemas), "the document set")
	assert.Positive(t, spent(refClass{id: r.intern("#absent")}), "a lookup that reads no tree")
	a := r.tree.walk(r.tree.root, []string{"components", "schemas", "A"})
	require.Contains(t, r.held, a, "A's $defs reference is held")
	assert.Positive(t, spent(refClass{held: a}), "a held reference")
}

// TestCycles_ChargesEveryStep pins that the search pays per member it steps
// through, not per class it looks up: one reference to an anchor many plain
// schemas declare is two lookups and a step per declaration.
func TestCycles_ChargesEveryStep(t *testing.T) {
	t.Parallel()
	const declarations = 200
	var sb strings.Builder
	sb.WriteString("    A:\n      $ref: '#a'\n      properties:\n")
	for i := 0; i < declarations; i++ {
		fmt.Fprintf(&sb, "        p%d: {$anchor: a, type: string}\n", i)
	}
	doc, root := buildDoc(t, schemasDoc(strings.TrimRight(sb.String(), "\n")))
	r, starts := newReach(t.Context(), maxReachWork, root, doc)
	require.Len(t, starts, 1)

	before := r.budget.spent
	require.False(t, r.cycles(starts[0], map[vertex]int{}))
	assert.GreaterOrEqual(t, r.budget.spent-before, declarations)
}

// TestReach_EveryWalkStopsOnceTheBudgetIsSpent pins that each walk asks the
// budget before every step, so with nothing left it takes none. A walk that
// charged nothing would run to the end, which is what the budget exists to
// prevent.
func TestReach_EveryWalkStopsOnceTheBudgetIsSpent(t *testing.T) {
	t.Parallel()
	spec := schemasDoc("    A:\n      $defs: {n: {$anchor: z, $ref: '#/$defs/m'}, m: {$ref: '#/$defs/n'}}")
	doc, root := buildDoc(t, spec)
	prepared := func() (*reach, *budget, []*yaml.Node, []*yaml.Node) {
		b := &budget{limit: maxReachWork}
		r := emptyReach(root, b)
		declared, defsRefs := r.tree.index()
		r.collect(t.Context(), doc)
		return r, b, declared, defsRefs
	}

	t.Run("index", func(t *testing.T) {
		t.Parallel()
		tr := newTree(root, &budget{})
		tr.index()
		assert.Empty(t, tr.anywhere.next)
	})
	t.Run("scopesOf", func(t *testing.T) {
		t.Parallel()
		r, b, declared, _ := prepared()
		b.limit = b.spent
		assert.Nil(t, r.scopesOf(declared[0]))
		assert.Empty(t, r.scopes)
	})
	t.Run("markSearched", func(t *testing.T) {
		t.Parallel()
		r, b, _, defsRefs := prepared()
		b.limit = b.spent
		r.markSearched(defsRefs)
		assert.Empty(t, r.searched)
	})
	t.Run("markDrifted", func(t *testing.T) {
		t.Parallel()
		r, b, declared, _ := prepared()
		b.limit = b.spent
		assert.Nil(t, r.markDrifted(declared[0]))
		assert.Empty(t, r.drifted)
	})
	t.Run("drift", func(t *testing.T) {
		t.Parallel()
		r, b, _, defsRefs := prepared()
		b.limit = b.spent
		r.drift(defsRefs)
		assert.Empty(t, r.static, "no reference was even read")
	})
	t.Run("cycles", func(t *testing.T) {
		t.Parallel()
		r, starts := newReach(t.Context(), maxReachWork, root, doc)
		require.True(t, r.cycles(starts[0], map[vertex]int{}), "with budget to spare the cycle is found")
		r.budget.limit = r.budget.spent
		assert.False(t, r.cycles(starts[0], map[vertex]int{}), "with none it gives no verdict")
	})
}

// TestDocuments_ADriftedReferenceCanLandOnAnyReferenceOrSearchedAncestor pins
// documents' filter: a schema that is a reference, or an ancestor the resolver
// searched for a $defs entry, and nothing else. Only a reference load did not
// hold searches: the one in A's extension.
func TestDocuments_ADriftedReferenceCanLandOnAnyReferenceOrSearchedAncestor(t *testing.T) {
	t.Parallel()
	doc, root := buildDoc(t, schemasDoc(
		"    A: {x-raw: {$ref: \"#/$defs/m\"}, $defs: {m: {type: string}}}\n    B: {$ref: \"#/components/schemas/A\"}\n    C: {type: string}"))
	r, _ := newReach(t.Context(), maxReachWork, root, doc)
	node := func(path ...string) *yaml.Node {
		return r.tree.walk(r.tree.root, append([]string{"components", "schemas"}, path...))
	}

	assert.ElementsMatch(t, []*yaml.Node{node("A"), node("B")}, r.documents().members,
		"A is searched without being a reference, B is a reference, m and C are neither")
	assert.Same(t, r.documents(), r.documents(), "every drifted empty fragment shares one set")
}

// TestParentScopes_ChargesEveryOwnerItReads pins the union of a node's parents'
// scopes: each owner once, and every owner it reads paid for, since an alias can
// give a node many parents.
func TestParentScopes_ChargesEveryOwnerItReads(t *testing.T) {
	t.Parallel()
	a, b, c := &yaml.Node{}, &yaml.Node{}, &yaml.Node{}
	m, p1, p2 := &yaml.Node{}, &yaml.Node{}, &yaml.Node{}
	b0 := &budget{limit: maxReachWork}
	r := emptyReach(&yaml.Node{}, b0)
	r.tree.parents[m] = []*yaml.Node{p1, p2}
	r.scopes[p1] = []*yaml.Node{a, b, c}
	r.scopes[p2] = []*yaml.Node{b, c}

	assert.Equal(t, []*yaml.Node{a, b, c}, r.parentScopes(m))
	assert.Equal(t, 5, b0.spent)
}

// TestScopesOf_StopsAtTheNearestSchema pins that a climb ends at the first schema
// it reaches: a schema nested in another belongs to the same document, so
// climbing on would only report it again.
func TestScopesOf_StopsAtTheNearestSchema(t *testing.T) {
	t.Parallel()
	doc, root := buildDoc(t, schemasDoc(`    A: {properties: {p: {properties: {q: {$anchor: z}}}}}`))
	r, _ := newReach(t.Context(), maxReachWork, root, doc)
	deep := r.tree.walk(r.tree.root, []string{"components", "schemas", "A", "properties", "p", "properties", "q"})
	require.NotNil(t, deep)

	assert.Len(t, r.scopesOf(deep), 1)
}

// TestOwnerOf pins ownerOf: a node in one schema document is searched as that
// document, and a node in several, or none, as the whole tree.
func TestOwnerOf(t *testing.T) {
	t.Parallel()
	one, two, a, b := &yaml.Node{}, &yaml.Node{}, &yaml.Node{}, &yaml.Node{}
	r := emptyReach(&yaml.Node{}, &budget{limit: maxReachWork})
	r.addScope(one, a)
	r.addScope(one, a)
	r.addScope(two, a)
	r.addScope(two, b)

	assert.Same(t, a, r.ownerOf(one), "the same owner recorded twice is still one owner")
	assert.Nil(t, r.ownerOf(two), "two owners are searched as the whole tree")
	assert.Nil(t, r.ownerOf(&yaml.Node{}), "and so is a node with none")
}
