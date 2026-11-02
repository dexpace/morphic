// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonpointer"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// resumedOver resolves root, written as root.yaml in dir, as resolveExternal's
// first pass does, with settle's work bounded by limit, and returns that work
// and what the pass reported.
func resumedOver(t *testing.T, dir, root string, limit int) (*resumeWork, []ir.Diagnostic) {
	t.Helper()
	tree, _, err := decodeStream(t.Context(), []byte(root))
	require.NoError(t, err)
	releaseAnchors(t.Context(), tree)
	doc, err := unmarshal(t.Context(), []byte(root), tree)
	require.NoError(t, err)
	self := newSourceDocument(filepath.Join(dir, "root.yaml"), []byte(root), tree, judgment{}, newSourceTree(t.Context(), 0, overlay.Origin{}, tree), nil)
	opts := Options{Reader: reader.Disk{}}
	ext := newExternal(t.Context(), doc, opts, newExternalReads(self, budget.Unbounded()))
	ext.work.steps = budget.New(budget.AtMost(limit))
	at := pointerAt(0, overlay.Origin{})
	pass := newResolution(t.Context(), doc, Siblings{}, MediaTypes{}, self, opts, &ext)
	pass.run(t.Context(), doc)
	return ext.work, append(pass.diags(sitePlacer(at, Others{})), crossed(at, &ext)...)
}

// TestSettle_TheResumedWorkIsBounded pins maxResumeWork's bound. Two chains
// leave the source and come back to a response of its own naming raw YAML,
// ahead of the response being resolved, so each stalls on the source's bytes
// and is resumed; they name different YAML, since a hop the library has
// resolved once never stalls. Every bound short of the work they take is
// tried: past it no chain is resumed, each one left reports why at its $ref,
// and the crossing is reported once, at the document.
func TestSettle_TheResumedWorkIsBounded(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := rootOfResponses(
		responseRef("OutA", "./root.yaml#/components/responses/MidA"),
		responseRef("OutB", "./root.yaml#/components/responses/MidB"),
		responseRef("MidA", "#/x-lib/R"),
		responseRef("MidB", "#/x-lib/N/a/b"))
	unbounded, diags := resumedOver(t, dir, root, maxResumeWork)
	require.Empty(t, diags)
	require.Positive(t, unbounded.steps.Spent())

	resumedAt := map[int]bool{}
	for limit := range unbounded.steps.Spent() {
		_, diags := resumedOver(t, dir, root, limit)
		budgets, left := 0, map[string]bool{}
		for _, d := range diags {
			switch d.Code {
			case diag.BudgetExceeded:
				budgets++
				assert.Equal(t, ir.SeverityError, d.Severity)
				assert.Equal(t, fmt.Sprintf("resolving reference chains through referenced documents takes more "+
					"than %d steps; the $refs past them are reported unresolved", limit), d.Message)
				assert.Empty(t, d.Provenance.Pointer, "reported at the document")
			case diag.UnresolvedRef:
				left[string(d.Provenance.Pointer)] = true
				assert.Contains(t, []string{"/components/responses/OutA", "/components/responses/OutB"},
					string(d.Provenance.Pointer), "limit %d", limit)
				assert.True(t, strings.HasSuffix(d.Message, fmt.Sprintf(": resolving it further takes more than the "+
					"%d steps budgeted for chains through referenced documents", limit)), "limit %d: %s", limit, d.Message)
			default:
				require.Fail(t, "an unexpected report", "limit %d: %s %s", limit, d.Code, d.Message)
			}
		}
		require.NotEmpty(t, left, "limit %d", limit)
		require.Equal(t, 1, budgets, "the crossing is reported once, limit %d", limit)
		resumedAt[2-len(left)] = true
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
		var lib strings.Builder
		lib.WriteString("x-d:\n")
		entries := make([]string, 0, 2*n)
		for i := range n {
			entries = append(entries, responseRef(fmt.Sprintf("Out%d", i), fmt.Sprintf("./root.yaml#/components/responses/Mid%d", i)))
		}
		for i := range n {
			target := fmt.Sprintf("#/x-d/D%d", i)
			if layout == "tree" {
				target = fmt.Sprintf("#/x-d/%d/%d/%d/%d", i/64, i/16%4, i/4%4, i%4)
			}
			entries = append(entries, responseRef(fmt.Sprintf("Mid%d", i), target))
		}
		root := strings.Replace(rootOfResponses(entries...), libraryYAML, "", 1)
		if layout == "tree" {
			root += responseTree(n)
		} else {
			root += "x-d:\n"
			for i := range n {
				root += fmt.Sprintf("  D%d: {description: d}\n", i)
			}
		}
		work, diags := resumedOver(t, t.TempDir(), root, maxResumeWork)
		require.Empty(t, diags, "%s %d", layout, n)
		return work.steps.Spent()
	}
	const n = 64
	assert.Greater(t, spent(2*n, "wide"), 3*spent(n, "wide"), "one mapping holding them all")
	assert.LessOrEqual(t, 4*spent(2*n, "tree"), 9*spent(n, "tree"), "mappings four keys wide")
}

// responseTree writes the mapping x-d, holding a response at /x-d/a/b/c/e for
// each of the n numbers whose base-four digits are a, b, c and e.
func responseTree(n int) string {
	var b strings.Builder
	b.WriteString("x-d:\n")
	for a := range (n + 63) / 64 {
		fmt.Fprintf(&b, "  %d:\n", a)
		for c := range 4 {
			fmt.Fprintf(&b, "    %d:\n", c)
			for e := range 4 {
				fmt.Fprintf(&b, "      %d:\n", e)
				for f := range 4 {
					fmt.Fprintf(&b, "        %d: {description: d}\n", f)
				}
			}
		}
	}
	return b.String()
}

// TestResumeWork_Charge pins what a resumption is charged: a step, and for each
// hop it reads in the tree, a step and the library's read of it, then the keys
// indexed to count them. The hops are followed while they stay in the document,
// each once; a charge past the limit says so, and a read is priced no further
// than what is left.
func TestResumeWork_Charge(t *testing.T) {
	t.Parallel()
	tree, err := parseTree(t.Context(), []byte(`components:
  schemas:
    Base: {type: object}
    Alias: {$ref: '#/components/schemas/Base'}
    Two: {$ref: '#/components/schemas/Alias'}
    Loop: {$ref: '#/components/schemas/Loop'}
    Out: {$ref: 'third.yaml#/x'}
    Back: {$ref: '#/components/schemas/Out'}
    Plus+Key: {$ref: '#/components/schemas/Base'}
`))
	require.NoError(t, err)
	cost := func(name string) int {
		steps, _ := newTreeReads().cost(tree, "/components/schemas/"+name, budget.Unbounded())
		return steps
	}
	const indexed = 1 + 1 + 7 // the root's pairs, components', schemas'
	for _, c := range []struct {
		ref  string
		hops []string
	}{
		{"#/components/schemas/Two", []string{"Two", "Alias", "Base"}},
		{"#/components/schemas/Loop", []string{"Loop"}},
		{"#/components/schemas/Back", []string{"Back", "Out"}},
		{"#/components/schemas/Missing", []string{"Missing"}},
		{"#/components/schemas/Plus+Key", []string{"Plus+Key", "Base"}},
	} {
		w := newResumeWork()
		require.True(t, w.charge(tree, references.Reference(c.ref)))
		want := 1 + indexed
		for _, hop := range c.hops {
			want += 1 + cost(hop)
		}
		assert.Equal(t, want, w.steps.Spent(), c.ref)
	}

	w := newResumeWork()
	assert.Equal(t, budget.AtMost(maxResumeWork), w.steps.Limit(), "settle resumes within maxResumeWork")
	assert.True(t, w.charge(tree, "third.yaml#/components/schemas/Base"))
	assert.Equal(t, 1, w.steps.Spent(), "a hop into another document is not charged")

	full := newResumeWork()
	full.charge(tree, "#/components/schemas/Two")
	for limit := range full.steps.Spent() {
		w := newResumeWork()
		w.steps = budget.New(budget.AtMost(limit))
		assert.False(t, w.charge(tree, "#/components/schemas/Two"), "limit %d", limit)
		assert.Greater(t, w.steps.Spent(), limit)
		assert.False(t, w.steps.Spend(0), "a budget crossed stays crossed")
	}

	w = newResumeWork()
	w.steps = budget.New(budget.AtMost(1))
	assert.False(t, w.charge(tree, "#/components/schemas/Two"))
	assert.Empty(t, w.reads.keys, "a read past what is left of the budget stops before it indexes anything")
}

// TestResumable_TheBudgetStopsWhatItWouldResume pins that a resolution settle
// would resume is refused, with the error it fails with, once the budget is
// spent: a stall of either kind, and a hop that reached a stand-in. A failure
// settle would not resume is not refused: its error stands.
func TestResumable_TheBudgetStopsWhatItWouldResume(t *testing.T) {
	t.Parallel()
	data := []byte("components:\n  schemas:\n    Base: {type: object}\n")
	tree, err := parseTree(t.Context(), data)
	require.NoError(t, err)
	read := newExternalReads(sourceDocument{}, budget.Unbounded())
	read.recordTree("other.yaml", tree, data)
	walked := jsonpointer.ErrInvalidPath.Wrap(errors.New("expected index, got key at /components"))
	hop := func(document any, object any) record {
		return record{path: "other.yaml", document: &document, object: object}
	}
	stand, ok := read.standIn(&soa.ReferencedParameter{})
	require.True(t, ok)
	base, _ := newTreeReads().cost(tree, "/components/schemas/Base", budget.Unbounded())
	const indexed = 3 // the pairs of the root, components and schemas
	stalls := []struct {
		name  string
		c     chain
		spent int
	}{
		{"a stall, read by the library", chain{stopped: "#/components/schemas/Base",
			records: []record{hop(slices.Clone(data), nil)}}, 1 + 1 + base + indexed},
		{"a stand-in reached", chain{records: []record{hop(slices.Clone(data), stand)}}, 1},
	}
	for _, c := range stalls {
		ext := newExternal(t.Context(), &soa.OpenAPI{}, Options{}, read)
		resume, refused := ext.resumable(c.c, walked)
		require.True(t, resume, "%s: resumed within the budget", c.name)
		require.NoError(t, refused)
		assert.Equal(t, c.spent, ext.work.steps.Spent(), c.name)

		spent := ext.work.steps.Spent()
		ext.work.steps = budget.New(budget.AtMost(spent))
		ext.work.steps.Spend(spent)
		resume, refused = ext.resumable(c.c, walked)
		assert.False(t, resume, c.name)
		require.Error(t, refused, c.name)
		assert.Equal(t, fmt.Sprintf("resolving it further takes more than the %d steps budgeted "+
			"for chains through referenced documents", spent), refused.Error())
	}

	ext := newExternal(t.Context(), &soa.OpenAPI{}, Options{}, read)
	ext.work.steps = budget.New(budget.AtMost(0))
	ext.work.steps.Spend(1)
	resume, refused := ext.resumable(stalls[0].c, errors.New("circular reference detected"))
	assert.False(t, resume)
	assert.NoError(t, refused, "a failure settle would not resume keeps its own error")
}
