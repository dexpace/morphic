// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
)

// TestCompile_OneURLSpelledSixWaysIsOneRequestOneKeyAndOneType holds the
// spellings RFC 3986 section 6.2 makes equal (an upper-case scheme, a dot
// segment, an escaped unreserved character, a dot) to one request, one
// Document.Sources entry and one type, and the empty query, which it does not,
// to a request and a type of its own, in either declaration order.
func TestCompile_OneURLSpelledSixWaysIsOneRequestOneKeyAndOneType(t *testing.T) {
	t.Parallel()
	refs := map[string]string{
		"A": "$URL/c.yaml#/X", "B": "$URLUPPER/c.yaml#/X", "C": "$URL/x/../c.yaml#/X",
		"D": "$URL/%63.yaml#/X", "E": "$URL/./c.yaml#/X", "F": "$URL/c.yaml?#/X",
	}
	orders := map[string][]string{
		"A first": {"A", "B", "C", "D", "E", "F"},
		"D first": {"D", "B", "C", "A", "E", "F"},
	}
	var firstIR string
	var firstKinds []string
	for _, name := range []string{"A first", "D first"} {
		srv := serveDocs(t, map[string]string{"/c.yaml": "X: {type: object, properties: {n: {type: string}}}\n"})
		upper := "HTTP" + strings.TrimPrefix(srv.URL, "http")
		src := head31 + "paths: {}\ncomponents:\n  schemas:\n"
		for _, k := range orders[name] {
			ref := strings.NewReplacer("$URLUPPER", upper, "$URL", srv.URL).Replace(refs[k])
			src += "    " + k + ": {$ref: '" + ref + "'}\n"
		}
		rec := &recordingReader{inner: loopbackDisk{}}

		doc, diags := compileWithReader(t, "spec.yaml", src, openapi.Options{AllowExternalRefs: true}, rec)

		assertNoErrorDiags(t, diags)
		hits := srv.requests()
		slices.Sort(hits)
		assert.Equal(t, []string{"/c.yaml", "/c.yaml?"}, hits, "%s: one request per document", name)
		want := labelled(srv.URL+"/c.yaml", "/X")
		for _, k := range []string{"A", "B", "C", "D", "E"} {
			assert.Equal(t, want, useTarget(t, doc, k), "%s: %s", name, k)
		}
		assert.NotEqual(t, want, useTarget(t, doc, "F"), "%s: c.yaml? is another resource", name)
		assert.Equal(t, labelled(srv.URL+"/c.yaml?", "/X"), useTarget(t, doc, "F"), name)
		paths := sourcePaths(doc)
		require.Len(t, paths, 3, "%s: the source and two documents: %v", name, paths)
		slices.Sort(paths[1:])
		assert.Equal(t, []string{"spec.yaml", srv.URL + "/c.yaml", srv.URL + "/c.yaml?"}, paths, name)

		// Compare the order-free parts: the ids embed the server's port, so
		// the port is replaced by a fixed one.
		normal := strings.ReplaceAll(jsonOf(t, doc), strings.TrimPrefix(srv.URL, "http://"), "H")
		kindsNow := kinds(diags)
		if firstIR == "" {
			firstIR, firstKinds = normal, kindsNow
			continue
		}
		assert.Equal(t, firstIR, normal, "the IR is the same in both orders")
		assert.Equal(t, firstKinds, kindsNow)
	}
}

// TestCompile_AnIdentifierAndAReferenceMeetInOneSpelling holds an $id and a
// $ref naming one URL in two spellings (an escaped dot segment, a default
// port) to the one resource, so the reference lands on the schema the
// identifier declares and nothing is read, with external references refused.
func TestCompile_AnIdentifierAndAReferenceMeetInOneSpelling(t *testing.T) {
	t.Parallel()
	for name, id := range map[string]string{"an escaped dot segment": "http://h/a/%2e%2e/b", "a default port": "http://h:80/b"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := head31 + "paths: {}\ncomponents:\n  schemas:\n    P: {$id: '" + id + "', type: object}\n" +
				"    R: {$ref: 'http://h/b'}\n"

			doc, diags := compileText(t, "spec.yaml", src, openapi.Options{})

			assertNoErrorDiags(t, diags)
			assert.Equal(t, componentID("P"), useTarget(t, doc, "R"))
		})
	}
}
