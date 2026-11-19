// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reftable_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
)

// askLog is a Config.Open serving docs by name, which records every read as
// the name and the document that asked for it.
type askLog struct {
	mu    sync.Mutex
	reads []ask
}

// ask is one read: the document asked for, and the one whose reference asked.
type ask struct{ name, from string }

// opening returns cfg reading docs through the log.
func (l *askLog) opening(t *testing.T, cfg reftable.Config, docs map[string]string) reftable.Config {
	t.Helper()
	cfg.Open = func(name, from string) (*yaml.Node, error) {
		l.mu.Lock()
		l.reads = append(l.reads, ask{name, from})
		l.mu.Unlock()
		src, ok := docs[name]
		if !ok {
			return nil, errors.New("no such document")
		}
		return openapitest.YAMLDocument(t, src), nil
	}
	return cfg
}

// names returns each name read, in order.
func (l *askLog) names() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, len(l.reads))
	for _, r := range l.reads {
		out = append(out, r.name)
	}
	return out
}

// ladder is #1372's chain of n documents, each declaring by `$id` the URI the
// one before it asked for and naming the next by both: the source's A names
// f1 and its B names d1; dK holds T and an S declaring fK, whose p names
// d(K+1) and whose q names f(K+1). base prefixes each name ("" for paths, a
// URL's origin for URLs). It returns the source and the documents by the name
// a reference to them resolves to under prefix.
func ladder(n int, base string) (src string, docs map[string]string) {
	docs = map[string]string{}
	src = head + "components:\n  schemas:\n    A: {$ref: 'f1.yaml'}\n    B: {$ref: 'd1.yaml#/T'}\n"
	for k := 1; k <= n; k++ {
		var body string
		if k < n {
			body = fmt.Sprintf("T: {type: string}\nS: {$id: 'f%d.yaml', properties: {p: {$ref: 'd%d.yaml#/T'}, q: {$ref: 'f%d.yaml'}}}\n", k, k+1, k+1)
		} else {
			body = fmt.Sprintf("T: {type: string}\nS: {$id: 'f%d.yaml', type: string}\n", k)
		}
		docs[fmt.Sprintf("%sd%d.yaml", base, k)] = body
	}
	return src, docs
}

// laddered binds ladder(n, base) as the source named self reads it, through
// log.
func laddered(t *testing.T, n int, base string, log *askLog) *reftable.ReferenceTargetTable {
	t.Helper()
	src, docs := ladder(n, base)
	cfg := resolving
	cfg.Self = base + "spec.yaml"
	if base == "" {
		cfg.Self = "/spec.yaml"
		docs = rooted(docs)
	}
	return bound(t, src, log.opening(t, cfg, docs))
}

// rooted names each of docs by the absolute path resolving gives it.
func rooted(docs map[string]string) map[string]string {
	out := make(map[string]string, len(docs))
	for name, body := range docs {
		out["/"+name] = body
	}
	return out
}

// assertLinear fails unless the step counts at n/2, n and 2n of a chain are
// at most 2.2 times apart as the marginal cost of a doubling: a chain that
// costs a pass per link costs four.
func assertLinear(t *testing.T, label string, steps func(n int) int, sizes ...int) {
	t.Helper()
	require.Len(t, sizes, 3)
	a, b, c := steps(sizes[0]), steps(sizes[1]), steps(sizes[2])
	require.Less(t, a, b, label)
	assert.LessOrEqual(t, float64(c-b), 2.2*float64(b-a), "%s: steps at %v: %d, %d, %d", label, sizes, a, b, c)
}

// TestBind_AChainOfDeclaringDocumentsCostsWorkLinearInItsLength pins #1372
// for paths: each document declares the path the one before asked for, and
// the work grows by about a doubling per doubling of the rungs, every rung
// resolving, in n+1 documents.
func TestBind_AChainOfDeclaringDocumentsCostsWorkLinearInItsLength(t *testing.T) {
	t.Parallel()
	steps := func(n int) int { return laddered(t, n, "", &askLog{}).Steps() }
	assertLinear(t, "paths", steps, 50, 100, 200)
	assertLinear(t, "paths", steps, 100, 200, 400)

	table := laddered(t, 100, "", &askLog{})
	assert.Equal(t, 101, table.Documents(), "the source and one document per rung")
	for _, e := range table.Entries() {
		assert.Equal(t, reftable.Resolved, e.Chain.Status, "%v", e.Site)
	}
	assert.Zero(t, table.Passes()-1, "the chain is answered in one pass, not one per link")
}

// TestBind_AChainOfDeclaringURLsCostsWorkLinearAndEachIsRequestedOnce pins
// #1372 for URLs: the same bound, and each name requested once, the
// identifier f(K+1) too, which no document is, and which main requests once
// for nothing as well.
func TestBind_AChainOfDeclaringURLsCostsWorkLinearAndEachIsRequestedOnce(t *testing.T) {
	t.Parallel()
	const base = "https://x.test/"
	steps := func(n int) int { return laddered(t, n, base, &askLog{}).Steps() }
	assertLinear(t, "https", steps, 50, 100, 200)

	var log askLog
	table := laddered(t, 50, base, &log)
	for _, e := range table.Entries() {
		assert.Equal(t, reftable.Resolved, e.Chain.Status, "%v", e.Site)
	}
	count := map[string]int{}
	for _, name := range log.names() {
		count[name]++
	}
	for k := 1; k <= 50; k++ {
		assert.Equal(t, 1, count[fmt.Sprintf("%sd%d.yaml", base, k)], "d%d is requested once", k)
		assert.Equal(t, 1, count[fmt.Sprintf("%sf%d.yaml", base, k)], "f%d is requested once", k)
	}
	assert.Len(t, count, 100, "and nothing else is: %v", count)
}

// TestBind_HeldDocumentsDeclaringEachOthersURIAreAnsweredInEitherOrder pins,
// for the read that is told who asked, what two documents held together, one
// declaring the other's URI, are answered: both read, by the source, the URI
// naming the declaring document's schema, and the other left unreached, in
// both declaration orders.
func TestBind_HeldDocumentsDeclaringEachOthersURIAreAnsweredInEitherOrder(t *testing.T) {
	t.Parallel()
	docs := map[string]string{
		"https://x.test/u1": "S: {$id: 'https://x.test/u2', type: string}\n",
		"https://x.test/u2": "type: integer\n",
	}
	for _, schemas := range []string{
		"    A: {$ref: 'https://x.test/u1#/S'}\n    B: {$ref: 'https://x.test/u2'}\n",
		"    B: {$ref: 'https://x.test/u2'}\n    A: {$ref: 'https://x.test/u1#/S'}\n",
	} {
		var log askLog
		table := bound(t, head+"components:\n  schemas:\n"+schemas, log.opening(t, self, docs))
		assert.ElementsMatch(t, []string{"https://x.test/u1", "https://x.test/u2"}, log.names())
		for _, r := range log.reads {
			assert.Equal(t, "spec.yaml", r.from, "the source asked for %s", r.name)
		}
		u1 := docNamed(t, table, "https://x.test/u1")
		assert.Equal(t, reftable.DocumentPointer{Doc: u1, Pointer: "/S"}, entryAt(t, table, "/components/schemas/B").Hop.Target)
		assert.True(t, table.Document(docNamed(t, table, "https://x.test/u2")).Unreached)
	}
}

// TestBind_APathADocumentDeclaresIsAnsweredByTheDeclarationNotTheFile pins that
// a path is held as a URL is: the reference to a.yaml is answered by the
// schema b.yaml declares under that path, in either order, though a file of
// that name is there, which is read for nothing and left unreached, in one
// pass.
func TestBind_APathADocumentDeclaresIsAnsweredByTheDeclarationNotTheFile(t *testing.T) {
	t.Parallel()
	docs := map[string]string{
		"/a.yaml": "{type: boolean}\n",
		"/b.yaml": "T: {type: string}\nS: {$id: 'a.yaml', type: integer}\n",
	}
	for _, schemas := range []string{
		"    A: {$ref: 'a.yaml'}\n    B: {$ref: 'b.yaml#/T'}\n",
		"    B: {$ref: 'b.yaml#/T'}\n    A: {$ref: 'a.yaml'}\n",
	} {
		var log askLog
		table := bound(t, head+"components:\n  schemas:\n"+schemas, log.opening(t, resolving, docs))
		b := docNamed(t, table, "/b.yaml")
		assert.Equal(t, reftable.DocumentPointer{Doc: b, Pointer: "/S"}, entryAt(t, table, "/components/schemas/A").Hop.Target,
			"the declared path answers")
		for id := range reftable.DocID(table.Documents()) {
			if table.Document(id).Name == "/a.yaml" {
				assert.True(t, table.Document(id).Unreached, "the file is read for nothing")
			}
		}
		assert.Equal(t, 1, table.Passes())
		assert.NotContains(t, strings.Join(log.names(), " "), "d1", "nothing else")
	}
}
