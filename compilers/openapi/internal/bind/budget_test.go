package bind_test

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
)

// budgetSpec reaches every kind of work a table does: chains, a loop, a
// "#/$defs/..." pointer read up an ancestor and in a resource of its own, raw
// YAML a hop lands on and the sites inside it, a mapping value, a pointer
// through a reference object, and another document.
const budgetSpec = `openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /a: {$ref: '#/components/pathItems/P'}
components:
  pathItems:
    P: {get: {responses: {'200': {description: ok, content: {a/b: {schema: {type: string}}}}}}}
  schemas:
    A: {$ref: '#/components/schemas/B'}
    B: {$ref: '#/x-lib/C'}
    L1: {$ref: '#/components/schemas/L2'}
    L2: {$ref: '#/components/schemas/L1'}
    H: {$defs: {X: {}}, properties: {p: {$ref: '#/$defs/X'}, q: {$id: 'https://x.test/q', $ref: '#/$defs/Y', $defs: {Y: {}}}}}
    T: {$ref: '#/paths/~1a/get/responses/200/content/a~1b/schema'}
    O: {$ref: 'other.yaml#/S'}
    P: {discriminator: {propertyName: k, mapping: {c: '#/x-lib/C', h: '#/components/schemas/H/properties/p'}}}
x-lib:
  C: {properties: {d: {$ref: '#/components/schemas/H'}}}
`

// budgetConfig is the configuration budgetSpec is bound under, at limit.
func budgetConfig(limit int) bind.Config {
	cfg := withDocuments(map[string]string{"other.yaml": "S: {$ref: '#/T'}\nT: {type: string}\n"})
	cfg.Through = func(p jsontext.Pointer) (string, jsontext.Pointer, bool) {
		return "spec.yaml", "/components/pathItems/P", p == "/paths/~1a"
	}
	cfg.Limit = limit
	return cfg
}

// TestBind_StopsAtItsBudget pins the budget: every limit short of what the
// table costs cuts it, and an answer read within the cut is the full one, or
// one the budget stopped, never another. A site the budget never reached is
// missing. The cut lands at the same place each time, since the sites are
// read in order.
func TestBind_StopsAtItsBudget(t *testing.T) {
	t.Parallel()
	full := bound(t, budgetSpec, budgetConfig(0))
	require.False(t, full.Exhausted())
	require.Len(t, full.Loops(), 1)
	want := map[bind.Position]bind.Entry{}
	for _, e := range full.Entries() {
		want[e.Site] = e
	}

	stopped, missing := 0, 0
	for limit := 1; limit < full.Steps(); limit++ {
		cut := bound(t, budgetSpec, budgetConfig(limit))
		require.True(t, cut.Exhausted(), "limit %d", limit)
		assert.Equal(t, cut.Entries(), bound(t, budgetSpec, budgetConfig(limit)).Entries(), "limit %d", limit)
		missing += len(want) - len(cut.Entries())
		for _, e := range cut.Entries() {
			if e.Hop.Status == bind.OverBudget || e.Chain.Status == bind.OverBudget {
				stopped++
				continue
			}
			assert.Equal(t, want[e.Site], e, "limit %d: an answer read within the budget is the full one", limit)
		}
	}
	assert.Positive(t, stopped, "some limit stops a chain partway")
	assert.Positive(t, missing, "some limit stops before a site is read")
}

// deepMerge writes a document whose component A takes its $ref through a
// chain of `<<` merge keys deeper than the reading expands, and B through a
// shallow one.
func deepMerge() string {
	var b strings.Builder
	b.WriteString(head + "x-c0: &c0 {$ref: '#/components/schemas/T'}\n")
	for i := 1; i <= 70; i++ {
		fmt.Fprintf(&b, "x-c%d: &c%d {<<: *c%d}\n", i, i, i-1)
	}
	b.WriteString("components:\n  schemas:\n    T: {type: string}\n    A: {<<: *c70}\n    B: {<<: *c10}\n")
	return b.String()
}

// TestTable_SaysWhyARefWentUnread pins Unread: none for a table that read
// everything, the budget for one it stopped, and the merge bound for one a
// merge past it hid a $ref from, whose site is missing while the shallow
// merge's is read.
func TestTable_SaysWhyARefWentUnread(t *testing.T) {
	t.Parallel()
	full := bound(t, budgetSpec, budgetConfig(0))
	assert.False(t, full.Truncated())
	assert.Empty(t, full.Unread())
	assert.Empty(t, (*bind.Table)(nil).Unread(), "a nil table read nothing, so it says nothing")
	assert.Equal(t, "it was not read within the budget", bound(t, budgetSpec, budgetConfig(1)).Unread())

	deep := bound(t, deepMerge(), self)
	require.False(t, deep.Exhausted())
	assert.True(t, deep.Truncated())
	assert.Equal(t, "it is past a `<<` merge chain deeper than the 64 levels the compiler expands", deep.Unread())
	_, ok := deep.Entry(at("/components/schemas/A"))
	assert.False(t, ok, "A's $ref is past the bound")
	assert.Equal(t, at("/components/schemas/T"), entryAt(t, deep, "/components/schemas/B").Hop.Target)
}

// mergedKeys writes a document whose x-d70 merges the definitions and keys of
// x-d0 through a chain of `<<` deeper than the reading expands, and x-d10
// through a shallow one, and whose root merges x-d70 too when root is set,
// beside the schemas that read them.
func mergedKeys(root bool) string {
	var b strings.Builder
	b.WriteString(head + "x-d0: &d0 {$defs: {X: {type: string}}, k: {type: integer}}\n")
	for i := 1; i <= 70; i++ {
		fmt.Fprintf(&b, "x-d%d: &d%d {<<: *d%d}\n", i, i, i-1)
	}
	if root {
		b.WriteString("<<: *d70\n")
	}
	b.WriteString(`components:
  schemas:
    Key: {$ref: '#/x-d70/k'}
    Near: {$ref: '#/x-d10/k'}
    Missing: {$ref: '#/x-d10/nope'}
    Own: {<<: *d70, $id: 'https://x.test/own', $ref: '#/$defs/X'}
    Inside: {<<: *d70, properties: {q: {$ref: '#/$defs/X'}}}
    Shallow: {<<: *d10, properties: {r: {$ref: '#/$defs/Y'}}}
    Plain: {$ref: '#/$defs/X'}
`)
	return b.String()
}

// TestBind_SaysAKeyMayLiePastTheMergeBound pins MergeBound at each read a
// merge past the bound can stop: a pointer, a schema resource's own
// definitions, an ancestor's, and the root's. A key a shallow merge lacks is
// missing outright, and one it supplies resolves.
func TestBind_SaysAKeyMayLiePastTheMergeBound(t *testing.T) {
	t.Parallel()
	table := bound(t, mergedKeys(false), self)
	for site, want := range map[jsontext.Pointer]bind.Reason{
		"/components/schemas/Key":                  bind.MergeBound,
		"/components/schemas/Own":                  bind.MergeBound,
		"/components/schemas/Inside/properties/q":  bind.MergeBound,
		"/components/schemas/Missing":              bind.NoTarget,
		"/components/schemas/Shallow/properties/r": bind.NoDefinition,
		"/components/schemas/Plain":                bind.NoDefinition,
	} {
		assert.Equal(t, want, entryAt(t, table, site).Hop.Reason, site)
	}
	assert.Equal(t, at("/x-d10/k"), entryAt(t, table, "/components/schemas/Near").Hop.Target)

	merged := bound(t, mergedKeys(true), self)
	assert.Equal(t, bind.MergeBound, entryAt(t, merged, "/components/schemas/Plain").Hop.Reason,
		"the root's definitions lie past the bound")
}

// TestBind_AnswersAlikeInEitherOrder pins that a table depends on what the
// document says, not the order it says it in: the same components, paths and
// mapping entries declared in reverse give the same entries, loops, landings
// and mapping answers.
func TestBind_AnswersAlikeInEitherOrder(t *testing.T) {
	t.Parallel()
	lines := []string{
		"    A: {$ref: '#/components/schemas/B'}\n",
		"    B: {$ref: '#/x-lib/C'}\n",
		"    L1: {$ref: '#/components/schemas/L2'}\n",
		"    L2: {$ref: '#/components/schemas/L3'}\n",
		"    L3: {$ref: '#/components/schemas/L1'}\n",
		"    R: {$ref: '#/components/schemas/L2'}\n",
		"    H: {$defs: {X: {}}, properties: {p: {$ref: '#/$defs/X'}}}\n",
		"    M: {$ref: '#/components/schemas/H/properties/p'}\n",
		"    P: {discriminator: {propertyName: k, mapping: {a: '#/x-lib/C', b: '#/components/schemas/M'}}}\n",
	}
	build := func(order []string) *bind.Table {
		return bound(t, head+"components:\n  schemas:\n"+strings.Join(order, "")+
			"x-lib:\n  C: {properties: {d: {$ref: '#/components/schemas/A'}}}\n", self)
	}
	reversed := slices.Clone(lines)
	slices.Reverse(reversed)
	forward, backward := build(lines), build(reversed)
	assert.Equal(t, forward.Entries(), backward.Entries())
	assert.Equal(t, forward.Loops(), backward.Loops())
	assert.Equal(t, forward.Landings(), backward.Landings())
	disc := at("/components/schemas/P/discriminator")
	for _, v := range []string{"#/x-lib/C", "#/components/schemas/M"} {
		f, _ := forward.Mapping(disc, v)
		b, _ := backward.Mapping(disc, v)
		assert.Equal(t, f, b, v)
	}
}

// TestSpellings pins how each value of the table's types reads: a status, a
// role, a position, and why each kind of hop lands on no schema.
func TestSpellings(t *testing.T) {
	t.Parallel()
	for s, want := range map[bind.Status]string{0: "invalid", bind.Resolved: "resolved", bind.Unresolved: "unresolved",
		bind.Cyclic: "cyclic", bind.OverBudget: "over-budget", 5: "invalid"} {
		assert.Equal(t, want, s.String())
	}
	for r, want := range map[bind.Role]string{bind.NoRole: "none", bind.Schema: "schema", bind.Raw: "raw",
		bind.Object: "object", 9: "none"} {
		assert.Equal(t, want, r.String())
	}
	assert.Equal(t, "#/a", bind.Position{Pointer: "/a"}.String())
	assert.Equal(t, "doc2#/a", bind.Position{Doc: 2, Pointer: "/a"}.String())
	for text, hop := range map[string]bind.Hop{
		"it names nothing in the document it points into":                            {Reason: bind.NoTarget},
		"it names a position that holds no schema":                                   {Reason: bind.NotSchema},
		"it names a $anchor, which the compiler does not read as a reference target": {Reason: bind.AnchorTarget},
		"its fragment is no JSON pointer":                                            {Reason: bind.BadPointer},
		"no definition answers it: neither the document's root nor a schema of the model above it holds one " +
			"by that name": {Reason: bind.NoDefinition},
		"it names another document, and external references are not read":                    {Reason: bind.ExternalOff},
		"the document it names could not be read: gone":                                      {Reason: bind.Unreadable, Detail: "gone"},
		"its pointer passes through a reference that resolves to nothing the compiler reads": {Reason: bind.Unpassable},
		"it names a key of a mapping that merges through `<<` deeper than the 64 levels the compiler expands, " +
			"and no level it read holds the key": {Reason: bind.MergeBound},
		"it was not read within the budget": {Status: bind.OverBudget},
		"it does not resolve":               {Reason: 200},
	} {
		assert.Equal(t, text, hop.Why(), fmt.Sprint(hop.Reason))
	}
}
