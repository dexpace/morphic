// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

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
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
)

// objectsSpec writes a reference object of each way one reads: chains,
// targets of another kind and of raw YAML, pointers RFC 6901 reads otherwise
// than the parser library, a path item's fields read in place, a loop and a
// read needing itself.
const objectsSpec = `openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /a:
    $ref: '#/paths/~1b'
    put: {requestBody: {content: {a/b: {schema: {type: string}}}}, responses: {'200': {description: ok}}}
  /b:
    post: {requestBody: {content: {a/b: {schema: {type: string}}}}, responses: {'200': {description: ok}}}
components:
  parameters:
    Chain: {$ref: '#/components/parameters/Middle'}
    Middle: {$ref: '#/components/parameters/End'}
    End: {name: e, in: query}
    Header: {$ref: '#/components/headers/H'}
    Schema: {$ref: '#/components/schemas/S'}
    Raw: {$ref: '#/x-p/q'}
    Scalar: {$ref: '#/x-p/s'}
    Slash: {$ref: '#/x-p/q/'}
    Empty: {$ref: '#/x-p/e/'}
    Name: {$ref: '#q'}
    Merged: {$ref: '#/x-m/q'}
    Wide: {$ref: '#/x-p/😀'}
  requestBodies:
    Beside: {$ref: '#/paths/~1a/put/requestBody'}
    Through: {$ref: '#/paths/~1a/post/requestBody'}
    L1: {$ref: '#/components/requestBodies/L2'}
    L2: {$ref: '#/components/requestBodies/L1'}
  responses:
    NeedsB: {$ref: '#/components/responses/NeedsA/headers/H'}
    NeedsA: {$ref: '#/components/responses/NeedsB/headers/H'}
  headers:
    H: {schema: {type: string}}
  schemas:
    S: {type: string}
x-p:
  q: {name: q, in: query}
  s: a string
  e: {"": {name: e, in: query}}
  "😀": {name: w, in: query}
x-m: {<<: [{o: {name: o, in: query}}, {q: {name: q, in: query}}]}
`

// referenceAt returns the table's entry for the reference object of kind at
// the source pointer site.
func referenceAt(t *testing.T, table *bind.Table, site jsontext.Pointer, kind bind.RefKind) bind.Reference {
	t.Helper()
	r, ok := table.Reference(at(site), kind)
	require.True(t, ok, "a reference at %s", site)
	return r
}

// TestBind_ReadsEachReferenceObject pins one row per way a reference object's
// $ref reads: where its chain ends, or why its own hop lands on no object of
// its kind (docs/compilers/openapi.md#references).
func TestBind_ReadsEachReferenceObject(t *testing.T) {
	t.Parallel()
	table := bound(t, objectsSpec, self)
	const params, bodies = "/components/parameters/", "/components/requestBodies/"
	ends := map[jsontext.Pointer]jsontext.Pointer{
		params + "Chain":   params + "End",
		params + "Raw":     "/x-p/q",
		params + "Empty":   "/x-p/e/",
		params + "Merged":  "/x-m/q",
		params + "Wide":    "/x-p/😀",
		bodies + "Beside":  "/paths/~1a/put/requestBody",
		bodies + "Through": "/paths/~1b/post/requestBody",
	}
	for site, end := range ends {
		kind := bind.RefParameter
		if strings.HasPrefix(string(site), bodies) {
			kind = bind.RefRequestBody
		}
		r := referenceAt(t, table, site, kind)
		assert.Equal(t, bind.Chain{Status: bind.Resolved, End: at(end), Hops: hopsTo(site)}, r.Chain, site)
	}
	fails := map[jsontext.Pointer]bind.Hop{
		params + "Header": {Ref: "#/components/headers/H", Status: bind.Unresolved, Reason: bind.NotObject, Detail: "parameter"},
		params + "Schema": {Ref: "#/components/schemas/S", Status: bind.Unresolved, Reason: bind.NotObject, Detail: "parameter"},
		params + "Scalar": {Ref: "#/x-p/s", Status: bind.Unresolved, Reason: bind.NotObject, Detail: "parameter"},
		params + "Slash":  {Ref: "#/x-p/q/", Status: bind.Unresolved, Reason: bind.NoTarget},
		params + "Name":   {Ref: "#q", Status: bind.Unresolved, Reason: bind.BadPointer},
	}
	for site, hop := range fails {
		r := referenceAt(t, table, site, bind.RefParameter)
		assert.Equal(t, hop, r.Hop, site)
		assert.Equal(t, bind.Unresolved, r.Chain.Status, site)
	}
	for _, name := range []jsontext.Pointer{"NeedsA", "NeedsB"} {
		r := referenceAt(t, table, "/components/responses/"+name, bind.RefResponse)
		assert.Equal(t, bind.Unpassable, r.Hop.Reason, "%s: a read needing the read that needs it", name)
	}
}

// hopsTo is how many $refs the chain from site in objectsSpec reads.
func hopsTo(site jsontext.Pointer) int {
	if site == "/components/parameters/Chain" {
		return 2
	}
	return 1
}

// TestBind_ReportsALoopOfReferenceObjectsAtItsLeastMember pins a loop of
// reference objects as a loop of schema $refs is read: found whole, at its
// least member, its kind named, each member's chain Cyclic.
func TestBind_ReportsALoopOfReferenceObjectsAtItsLeastMember(t *testing.T) {
	t.Parallel()
	table := bound(t, objectsSpec, self)
	var loops []bind.Loop
	for _, l := range table.Loops() {
		if l.Kind != 0 {
			loops = append(loops, l)
		}
	}
	l1, l2 := at("/components/requestBodies/L1"), at("/components/requestBodies/L2")
	assert.Equal(t, []bind.Loop{{Members: []bind.Position{l1, l2}, Site: l1, Kind: bind.RefRequestBody}}, loops)
	for _, p := range []bind.Position{l1, l2} {
		r, ok := table.Reference(p, bind.RefRequestBody)
		require.True(t, ok)
		assert.Equal(t, bind.Chain{Status: bind.Cyclic, Member: true}, r.Chain)
	}
}

// TestBind_ReferenceObjectsAnswerAlikeInEitherOrder pins that what the table
// reads of reference objects depends on what the document says, not the
// order it says it in.
func TestBind_ReferenceObjectsAnswerAlikeInEitherOrder(t *testing.T) {
	t.Parallel()
	before, rest, ok := strings.Cut(objectsSpec, "  parameters:\n")
	require.True(t, ok)
	params, after, ok := strings.Cut(rest, "  requestBodies:\n")
	require.True(t, ok)
	lines := strings.SplitAfter(params, "\n")
	slices.Reverse(lines)
	reversed := before + "  parameters:\n" + strings.Join(lines, "") + "  requestBodies:\n" + after
	forward, backward := bound(t, objectsSpec, self), bound(t, reversed, self)
	assert.Equal(t, forward.References(), backward.References())
	assert.Equal(t, forward.ObjectLandings(), backward.ObjectLandings())
	assert.Equal(t, forward.Loops(), backward.Loops())
}

// TestBind_StopsReferenceObjectsAtItsBudget pins the budget over reference
// objects: the cost itself suffices, every limit short of it cuts the table,
// and a reference answered within the cut is answered as in full, or stopped,
// never otherwise, the same way each time.
func TestBind_StopsReferenceObjectsAtItsBudget(t *testing.T) {
	t.Parallel()
	full := bound(t, objectsSpec, self)
	require.False(t, full.Exhausted())
	cfg := self
	cfg.Limit = budget.AtMost(full.Steps())
	require.False(t, bound(t, objectsSpec, cfg).Exhausted(), "the cost itself suffices")
	want := map[bind.Position]bind.Reference{}
	for _, r := range full.References() {
		want[r.Site] = r
	}
	stopped := 0
	for limit := 1; limit < full.Steps(); limit++ {
		cfg.Limit = budget.AtMost(limit)
		cut := bound(t, objectsSpec, cfg)
		require.True(t, cut.Exhausted(), "limit %d", limit)
		assert.Equal(t, cut.References(), bound(t, objectsSpec, cfg).References(), "limit %d", limit)
		for _, r := range cut.References() {
			if r.Hop.Status == bind.OverBudget || r.Chain.Status == bind.OverBudget {
				stopped++
				continue
			}
			assert.Equal(t, want[r.Site], r, "limit %d: an answer within the budget is the full one", limit)
		}
	}
	assert.Positive(t, stopped, "some limit stops a reference object")
}

// TestBind_CountsReferenceObjectWorkLinear pins the cost of reference objects
// in steps: twice the aliases in a chain, or twice the references naming the
// head of one, cost about twice the work, as each position of a chain is
// settled once however many references enter it (GitHub #1468).
func TestBind_CountsReferenceObjectWorkLinear(t *testing.T) {
	t.Parallel()
	alias := func(i int) string { return fmt.Sprintf("    P%d: {$ref: '#/components/parameters/P%d'}\n", i, i+1) }
	shapes := map[string]func(b *strings.Builder, n int){
		"a chain": func(b *strings.Builder, n int) {
			for i := range n {
				b.WriteString(alias(i))
			}
			fmt.Fprintf(b, "    P%d: {name: q, in: query}\n", n)
		},
		"references into one chain": func(b *strings.Builder, n int) {
			for i := range 64 {
				b.WriteString(alias(i))
			}
			b.WriteString("    P64: {name: q, in: query}\n")
			for i := range n {
				fmt.Fprintf(b, "    U%d: {$ref: '#/components/parameters/P0'}\n", i)
			}
		},
	}
	for name, shape := range shapes {
		steps := func(n int) int {
			var b strings.Builder
			b.WriteString(head + "components:\n  parameters:\n")
			shape(&b, n)
			table := bound(t, b.String(), self)
			require.False(t, table.Exhausted(), name)
			return table.Steps()
		}
		short, long := steps(1_000), steps(2_000)
		assert.Less(t, long, 5*short/2, "%s: twice the references, about twice the work", name)
	}
}

// TestBind_PlacesEachObjectLandingAtTheLeastReferenceReachingIt pins the
// object landings: each position of raw YAML a reference object's chain lands
// on, read as its kind, placed at the least reference whose chain reaches it.
func TestBind_PlacesEachObjectLandingAtTheLeastReferenceReachingIt(t *testing.T) {
	t.Parallel()
	table := bound(t, head+"components:\n  parameters:\n    B: {$ref: '#/x-p/q'}\n    A: {$ref: '#/x-p/r'}\n"+
		"x-p:\n  r: {$ref: '#/x-p/q'}\n  q: {name: q, in: query}\n", self)
	reached := func(p, site jsontext.Pointer) bind.ObjectLanding {
		return bind.ObjectLanding{At: at(p), Kind: bind.RefParameter, Name: "spec.yaml", Site: at(site), Reached: true}
	}
	assert.Equal(t, []bind.ObjectLanding{
		reached("/x-p/q", "/components/parameters/A"),
		reached("/x-p/r", "/components/parameters/A"),
	}, table.ObjectLandings())
}
