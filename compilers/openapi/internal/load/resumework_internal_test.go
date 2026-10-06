package load

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonpointer"
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

// resumedOver resolves root, written as root.yaml in dir, as resolveExternal's
// first pass does, with settle's work bounded by limit, and returns the model,
// that work, and what the pass reported.
func resumedOver(t *testing.T, dir, root string, limit int) (*soa.OpenAPI, *resumeWork, []ir.Diagnostic) {
	t.Helper()
	tree, _, err := decodeStream([]byte(root))
	require.NoError(t, err)
	releaseAnchors(tree)
	doc, valErrs, err := unmarshal(t.Context(), []byte(root), tree)
	require.NoError(t, err)
	self := newSourceDocument(filepath.Join(dir, "root.yaml"), []byte(root), tree, valErrs)
	opts := Options{AllowExternalRefs: true}
	reader := newExternal(doc, opts, newExternalReads(self))
	reader.work.limit = limit
	_, diags := newResolution(t.Context(), pointerAt(0, overlay.Origin{}), doc, self, opts, &reader).run(doc)
	return doc, reader.work, diags
}

// TestSettle_TheResumedWorkIsBounded pins maxResumeWork's bound. Two chains
// take a further hop in another document after an earlier $ref read it, so
// each is resumed; they end on different schemas, since a hop the library has
// resolved once is answered from its cache, and never stalls. Every bound short
// of the work they take is tried: past it no chain is resumed, each one left
// reports why at its $ref, and the crossing is reported once, at the document,
// since which chain crosses it follows the walk's order.
func TestSettle_TheResumedWorkIsBounded(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{"other.yaml": `components:
  schemas:
    Base: {type: object}
    BaseA: {type: object}
    BaseB: {type: object}
    A: {$ref: '#/components/schemas/BaseA'}
    B: {$ref: '#/components/schemas/BaseB'}
`})
	root := rootOfSchemas("    First: {$ref: './other.yaml#/components/schemas/Base'}\n",
		"    SA: {$ref: './other.yaml#/components/schemas/A'}\n",
		"    SB: {$ref: './other.yaml#/components/schemas/B'}\n")
	_, unbounded, diags := resumedOver(t, dir, root, maxResumeWork)
	require.Empty(t, diags)
	require.Positive(t, unbounded.spent)

	resumedAt := map[int]bool{}
	for limit := range unbounded.spent {
		doc, _, diags := resumedOver(t, dir, root, limit)
		var left []string
		for _, name := range []string{"SA", "SB"} {
			schema, _ := doc.Components.Schemas.Get(name)
			if trail := resolutionTrail(schema); trail.stopped != "" {
				left = append(left, "/components/schemas/"+name+" "+diag.UnresolvedRef)
			}
		}
		resumedAt[2-len(left)] = true
		require.NotEmpty(t, left, "limit %d", limit)
		require.Equal(t, append([]string{" " + diag.BudgetExceeded}, left...), diagLines(diags), "limit %d", limit)

		for _, d := range diags {
			if d.Code == diag.BudgetExceeded {
				assert.Equal(t, ir.SeverityError, d.Severity)
				assert.Equal(t, fmt.Sprintf("resolving reference chains through referenced documents takes more "+
					"than %d steps; the $refs past them are reported unresolved", limit), d.Message)
				continue
			}
			assert.True(t, strings.HasSuffix(d.Message, fmt.Sprintf(": resolving it further takes more than the "+
				"%d steps budgeted for chains through referenced documents", limit)), "limit %d: %s", limit, d.Message)
		}
	}
	assert.Equal(t, map[int]bool{0: true, 1: true}, resumedAt,
		"some bound stops the work before each chain, and none short of both")
}

// TestSettle_AResumedReadIsChargedTheKeysItScans pins GitHub #773: n chains
// whose resumed hop is into one mapping holding them all are charged the keys
// the library compares to find each, so doubling n about quadruples the work.
// The same chains into a tree of mappings four keys wide about double it: a
// little more, as the tree's top level fills.
func TestSettle_AResumedReadIsChargedTheKeysItScans(t *testing.T) {
	t.Parallel()
	// spent returns the work n chains take in the layout named.
	spent := func(n int, layout string) int {
		var other strings.Builder
		other.WriteString("components:\n  schemas:\n")
		roots := make([]string, 0, 1+n)
		roots = append(roots, "    First: {$ref: './other.yaml#/components/schemas/C0'}\n")
		for i := range n {
			target := fmt.Sprintf("#/d/D%d", i)
			if layout == "tree" {
				target = fmt.Sprintf("#/d/%d/%d/%d/%d", i/64, i/16%4, i/4%4, i%4)
			}
			fmt.Fprintf(&other, "    C%d: {$ref: '%s'}\n", i, target)
			roots = append(roots, fmt.Sprintf("    S%d: {$ref: './other.yaml#/components/schemas/C%d'}\n", i, i))
		}
		if layout == "tree" {
			other.WriteString(schemaTree(n))
		} else {
			other.WriteString("d:\n")
			for i := range n {
				fmt.Fprintf(&other, "  D%d: {type: object}\n", i)
			}
		}
		dir := externalDir(t, map[string]string{"other.yaml": other.String()})
		_, work, diags := resumedOver(t, dir, rootOfSchemas(roots...), maxResumeWork)
		require.Empty(t, diags, "%s %d", layout, n)
		return work.spent
	}
	const n = 64
	assert.Greater(t, spent(2*n, "wide"), 3*spent(n, "wide"), "one mapping holding them all")
	assert.LessOrEqual(t, 4*spent(2*n, "tree"), 9*spent(n, "tree"), "mappings four keys wide")
}

// schemaTree writes the mapping d, holding a schema at /d/a/b/c/e for each of the
// n numbers whose base-four digits are a, b, c and e.
func schemaTree(n int) string {
	var b strings.Builder
	b.WriteString("d:\n")
	for a := range (n + 63) / 64 {
		fmt.Fprintf(&b, "  %d:\n", a)
		for c := range 4 {
			fmt.Fprintf(&b, "    %d:\n", c)
			for e := range 4 {
				fmt.Fprintf(&b, "      %d:\n", e)
				for f := range 4 {
					fmt.Fprintf(&b, "        %d: {type: object}\n", f)
				}
			}
		}
	}
	return b.String()
}

// TestResumeWork_Charge pins what a resumption is charged: a step, and for each
// hop it reads in the tree, a step and the library's read of it, once for each
// time it is read, then the keys indexed to count them. The hops are followed
// while they stay in the document, each once, and a charge past the limit says
// so.
func TestResumeWork_Charge(t *testing.T) {
	t.Parallel()
	tree, parsed := parseTree([]byte(`components:
  schemas:
    Base: {type: object}
    Alias: {$ref: '#/components/schemas/Base'}
    Two: {$ref: '#/components/schemas/Alias'}
    Loop: {$ref: '#/components/schemas/Loop'}
    Out: {$ref: 'third.yaml#/x'}
    Back: {$ref: '#/components/schemas/Out'}
`))
	require.True(t, parsed)
	cost := func(name string) int {
		steps, _ := newTreeReads().cost(tree, "/components/schemas/"+name, math.MaxInt)
		return steps
	}
	const indexed = 1 + 1 + 6 // the root's pairs, components', schemas'
	for _, c := range []struct {
		ref   string
		reads int
		hops  []string
	}{
		{"#/components/schemas/Two", 2, []string{"Two", "Alias", "Base"}},
		{"#/components/schemas/Two", 1, []string{"Two", "Alias", "Base"}},
		{"#/components/schemas/Loop", 2, []string{"Loop"}},
		{"#/components/schemas/Back", 2, []string{"Back", "Out"}},
		{"#/components/schemas/Missing", 2, []string{"Missing"}},
	} {
		w := newResumeWork()
		require.True(t, w.charge(tree, references.Reference(c.ref), c.reads))
		want := 1 + indexed
		for _, hop := range c.hops {
			want += 1 + c.reads*cost(hop)
		}
		assert.Equal(t, want, w.spent, "%s read %d times", c.ref, c.reads)
	}

	w := newResumeWork()
	assert.True(t, w.charge(tree, "third.yaml#/components/schemas/Base", 2))
	assert.Equal(t, 1, w.spent, "a hop into another document is not charged")

	full := newResumeWork()
	full.charge(tree, "#/components/schemas/Two", 2)
	for limit := range full.spent {
		w := newResumeWork()
		w.limit = limit
		assert.False(t, w.charge(tree, "#/components/schemas/Two", 2), "limit %d", limit)
		assert.Greater(t, w.spent, limit)
		assert.False(t, w.spend(0), "a budget crossed stays crossed")
	}

	w = newResumeWork()
	w.limit = 1
	assert.False(t, w.charge(tree, "#/components/schemas/Two", 2))
	assert.Empty(t, w.reads.keys, "a read past what is left of the budget stops before it indexes anything")
}

// TestResumable_TheBudgetStopsWhatItWouldResume pins that a resolution settle
// would resume is refused, with the error it fails with, once the budget is
// spent: a stall of either kind, and a hop that reached a stand-in. A failure
// settle would not resume is not refused: its error stands.
func TestResumable_TheBudgetStopsWhatItWouldResume(t *testing.T) {
	t.Parallel()
	data := []byte("components:\n  schemas:\n    Base: {type: object}\n")
	tree, parsed := parseTree(data)
	require.True(t, parsed)
	read := newExternalReads(sourceDocument{})
	read.recordTree("other.yaml", tree, data)
	walked := jsonpointer.ErrInvalidPath.Wrap(errors.New("expected index, got key at /components"))
	hop := func(document any, reached bool, object any) record {
		return record{path: "other.yaml", document: &document, reached: reached, object: object}
	}
	stand, ok := read.standIn(&soa.ReferencedParameter{})
	require.True(t, ok)
	schema := oas3.NewJSONSchemaFromReference("./other.yaml#/components/schemas/Alias")
	other := fakeResolvable{ref: "./other.yaml#/components/parameters/P"}
	base, _ := newTreeReads().cost(tree, "/components/schemas/Base", math.MaxInt)
	const indexed = 3 // the pairs of the root, components and schemas
	stalls := []struct {
		name  string
		r     resolvable
		c     chain
		spent int
	}{
		{"a schema's stall, read by ends and the library", schema, chain{stopped: "#/components/schemas/Base",
			records: []record{hop(slices.Clone(data), true, nil), hop(slices.Clone(data), false, nil)}},
			1 + 1 + 2*base + indexed},
		{"another kind's stall, read by the library", other, chain{stopped: "#/components/schemas/Base",
			records: []record{hop(slices.Clone(data), true, nil)}}, 1 + 1 + base + indexed},
		{"a stand-in reached", other, chain{records: []record{hop(slices.Clone(data), true, stand)}}, 1},
	}
	for _, c := range stalls {
		reader := newExternal(&soa.OpenAPI{}, Options{}, read)
		resume, refused := reader.resumable(c.r, c.c, walked)
		require.True(t, resume, "%s: resumed within the budget", c.name)
		require.NoError(t, refused)
		assert.Equal(t, c.spent, reader.work.spent, c.name)

		reader.work.limit = reader.work.spent
		resume, refused = reader.resumable(c.r, c.c, walked)
		assert.False(t, resume, c.name)
		require.Error(t, refused, c.name)
		assert.Equal(t, fmt.Sprintf("resolving it further takes more than the %d steps budgeted "+
			"for chains through referenced documents", reader.work.limit), refused.Error())
	}

	reader := newExternal(&soa.OpenAPI{}, Options{}, read)
	reader.work.limit = 0
	reader.work.spend(1)
	resume, refused := reader.resumable(schema, stalls[0].c, errors.New("circular reference detected"))
	assert.False(t, resume)
	assert.NoError(t, refused, "a failure settle would not resume keeps its own error")
}
