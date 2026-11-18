// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

const (
	// trustedPet is the identifier a hostile document claims.
	trustedPet = "https://trusted.example/pet.yaml"
	// hijackDoc is GitHub #1942's hijack.yaml, a Y the source refers to and a Z
	// declaring trustedPet, with a W of its own referring to that URL.
	hijackDoc = "Y: {type: string}\nZ: {$id: \"" + trustedPet + "\", type: object, description: \"from the other host\"}\n" +
		"W: {$ref: '" + trustedPet + "'}\n"
)

// trusted starts the server that stands for trusted.example: it serves docs,
// and the reader that routes the host's URLs to it.
func trusted(t *testing.T, docs map[string]string) (*servedDocs, *recordingReader) {
	t.Helper()
	srv := serveDocs(t, docs)
	rec := &recordingReader{inner: hostMapReader{inner: loopbackDisk{}, routes: map[string]string{"https://trusted.example/": srv.URL + "/"}}}
	return srv, rec
}

// describedBy follows aliases from id to the type that holds a description,
// and returns it.
func describedBy(t *testing.T, doc *ir.Document, id ir.TypeID) string {
	t.Helper()
	for range 8 {
		switch td := doc.Types[id].(type) {
		case *ir.Scalar:
			id = td.Base.Target
		case *ir.Model:
			if td.Base == nil || len(td.Properties) > 0 {
				return td.Docs.Description
			}
			id = td.Base.Target // a use of an object, as an alias is of anything else
		default:
			require.NotNil(t, td, "%s", id)
			return td.Common().Docs.Description
		}
	}
	require.Fail(t, "an alias chain too long", string(id))
	return ""
}

// compileTrusted compiles src as spec.yaml, external references allowed,
// through rec.
func compileTrusted(t *testing.T, src string, rec compilers.DocumentReader) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	return compileWithReader(t, "spec.yaml", src, openapi.Options{AllowExternalRefs: true}, rec)
}

// TestCompile_ADocumentAnotherOriginServesDeclaresNothingForTheSource holds
// GitHub #1942's documents: one fetched from one host that declares an $id of
// another answers no reference to that URL from a document that did not fetch
// it, so the source's Pet reads the URL, from the host it names, once, while
// the fetched document's own reference to it is answered by its own
// declaration. Both orders.
func TestCompile_ADocumentAnotherOriginServesDeclaresNothingForTheSource(t *testing.T) {
	t.Parallel()
	for name, order := range map[string][2]string{"pet first": {"Pet", "H"}, "hijack first": {"H", "Pet"}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			remote, rec := trusted(t, map[string]string{"/pet.yaml": "{type: object, description: real}\n"})
			hijack := serveDocs(t, map[string]string{"/hijack.yaml": hijackDoc})
			refs := map[string]string{"Pet": trustedPet, "H": hijack.URL + "/hijack.yaml#/W"}
			src := head31 + "paths: {}\ncomponents:\n  schemas:\n    A: {$ref: '" + hijack.URL + "/hijack.yaml#/Y'}\n"
			for _, k := range order {
				src += "    " + k + ": {$ref: '" + refs[k] + "'}\n"
			}

			doc, diags := compileTrusted(t, src, rec)

			assertNoErrorDiags(t, diags)
			assert.Equal(t, "real", describedBy(t, doc, useTarget(t, doc, "Pet")), "the host the URL names answers it")
			assert.Equal(t, "from the other host", describedBy(t, doc, useTarget(t, doc, "H")),
				"the document's own declaration answers its own reference")
			assert.Equal(t, []string{"/pet.yaml"}, remote.requests())
			assert.Equal(t, []string{"/hijack.yaml"}, hijack.requests())
		})
	}
}

// TestCompile_AFilesDeclarationAnswersTheSourceAndNoRead holds an $id a file
// beside the source declares to answer a reference to that URL, in the source,
// with no request for the URL, with external references refused or allowed, and
// still when a fetched document declares the same URL.
func TestCompile_AFilesDeclarationAnswersTheSourceAndNoRead(t *testing.T) {
	t.Parallel()
	const local = "Z: {$id: '" + trustedPet + "', type: object, description: LOCAL}\n"
	for name, allow := range map[string]bool{"refused": false, "allowed": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := realDir(t)
			layout(t, dir, map[string]string{"local.yaml": local})
			remote, rec := trusted(t, nil)
			hijack := serveDocs(t, map[string]string{"/hijack.yaml": hijackDoc})
			src := head31 + "paths: {}\ncomponents:\n  schemas:\n    L: {$ref: 'local.yaml#/Z'}\n    P: {$ref: '" + trustedPet + "'}\n"
			if allow {
				src += "    H: {$ref: '" + hijack.URL + "/hijack.yaml#/Z'}\n"
			}

			doc, diags := compileWithReader(t, filepath.Join(dir, "spec.yaml"), src, openapi.Options{AllowExternalRefs: allow}, rec)

			assertNoErrorDiags(t, diags)
			want := useTarget(t, doc, "L")
			assert.Equal(t, want, useTarget(t, doc, "P"), "a file declares what it writes")
			assert.Equal(t, "LOCAL", describedBy(t, doc, want))
			assert.Empty(t, remote.requests(), "the URL is never requested")
			for _, loc := range rec.locations() {
				assert.NotEqual(t, trustedPet, loc)
			}
		})
	}
}

// TestCompile_AFetchedDocumentReadsAURLAFileDeclares holds a document fetched
// over the network to read a URL a local file of the source declares, from the
// network, as it would any URL, and not to land on the file's content: its
// reference is a 404 where the host serves nothing, while the source's own
// reference to the same URL lands on the file with no further request. Both
// orders.
func TestCompile_AFetchedDocumentReadsAURLAFileDeclares(t *testing.T) {
	t.Parallel()
	const secret = "https://trusted.example/secret.yaml"
	for name, evilFirst := range map[string]bool{"fetched first": true, "local first": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := realDir(t)
			layout(t, dir, map[string]string{"local.yaml": "Z: {$id: '" + secret + "', type: object, description: FILE-SECRET, " +
				"properties: {p: {type: string, description: FILE-SECRET-P}}}\n"})
			evil := serveDocs(t, map[string]string{"/evil.yaml": "X: {$ref: '" + secret + "'}\nW: {$ref: '" + secret + "#/properties/p'}\n"})
			remote, rec := trusted(t, nil)
			entries := []string{
				"    A: {$ref: '" + evil.URL + "/evil.yaml#/X'}\n", "    B: {$ref: '" + evil.URL + "/evil.yaml#/W'}\n",
			}
			local := "    L: {$ref: 'local.yaml#/Z'}\n"
			if evilFirst {
				entries = append(entries, local)
			} else {
				entries = slices.Insert(entries, 0, local)
			}
			head := head31 + "paths: {}\ncomponents:\n  schemas:\n"

			doc, diags := compileWithReader(t, filepath.Join(dir, "spec.yaml"), head+strings.Join(entries, ""),
				openapi.Options{AllowExternalRefs: true}, rec)

			bad := unresolvedRefs(diags)
			require.Len(t, bad, 2, "%+v", diags)
			for _, d := range bad {
				assert.Positive(t, d.Provenance.Source, "in evil.yaml's entry")
				assert.Contains(t, string(d.Provenance.Pointer), "/", "%+v", d)
				assert.Contains(t, d.Message, "404", "the reason is the host's refusal")
			}
			assert.Equal(t, "FILE-SECRET", describedBy(t, doc, useTarget(t, doc, "L")))
			for _, k := range []string{"A", "B"} {
				assert.Equal(t, ir.TypeID("t/prim/any"), typeTarget(t, doc, useTarget(t, doc, k)),
					"%s lands on nothing of local.yaml: %s", k, jsonOf(t, doc))
			}
			assert.Equal(t, []string{"/secret.yaml"}, remote.requests(), "one request, for the one URL")

			with := head + strings.Join(entries, "") + "    S: {$ref: '" + secret + "'}\n"
			doc, diags = compileWithReader(t, filepath.Join(dir, "spec.yaml"), with, openapi.Options{AllowExternalRefs: true}, rec)

			require.Len(t, unresolvedRefs(diags), 2, "%+v", diags)
			assert.Equal(t, useTarget(t, doc, "L"), useTarget(t, doc, "S"), "the source's own reference lands on the file")
			assert.Equal(t, []string{"/secret.yaml", "/secret.yaml"}, remote.requests(), "S adds no request: the second compile asks once")
		})
	}
}

// TestCompile_ATwoDocumentsDeclaringOneSelfAnswerNoReference holds two fetched
// documents that each write the same $self to answer no reference to it: the
// third reference reads the URL as a location, from the host it names, in
// either order, and is not reported as ambiguous.
func TestCompile_ATwoDocumentsDeclaringOneSelfAnswerNoReference(t *testing.T) {
	t.Parallel()
	const one = "https://example.com/one.yaml"
	self := "openapi: 3.2.0\n$self: " + one + "\ninfo: {title: o, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n    X: {type: string}\n"
	for name, third := range map[string]int{"third last": 2, "third first": 0} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := serveDocs(t, map[string]string{"/a.yaml": self, "/b.yaml": self})
			remote := serveDocs(t, nil)
			rec := &recordingReader{inner: hostMapReader{inner: loopbackDisk{}, routes: map[string]string{"https://example.com/": remote.URL + "/"}}}
			entries := []string{
				"    A: {$ref: '" + srv.URL + "/a.yaml#/components/schemas/X'}\n",
				"    B: {$ref: '" + srv.URL + "/b.yaml#/components/schemas/X'}\n",
			}
			entries = slices.Insert(entries, third, "    C: {$ref: '"+one+"#/components/schemas/X'}\n")

			_, diags := compileTrusted(t, head31+"paths: {}\ncomponents:\n  schemas:\n"+strings.Join(entries, ""), rec)

			bad := unresolvedRefs(diags)
			require.Len(t, bad, 1, "%+v", diags)
			assert.Equal(t, "/components/schemas/C", string(bad[0].Provenance.Pointer))
			assert.NotContains(t, bad[0].Message, "two or more", "neither document answers, so none is ambiguous")
			assert.Contains(t, rec.locations(), one, "the URL is read as a location")
			assert.Equal(t, []string{"/one.yaml"}, remote.requests())
		})
	}
}

// TestCompile_AFileDeclaringAURLIsNotRequestedWhereReadsAreAllowed holds a URL
// a file beside the source declares to cost no request when external
// references are allowed: the file's declaration is read first.
func TestCompile_AFileDeclaringAURLIsNotRequestedWhereReadsAreAllowed(t *testing.T) {
	t.Parallel()
	dir := realDir(t)
	layout(t, dir, map[string]string{"local.yaml": "Z: {$id: '" + trustedPet + "', type: object, description: LOCAL}\n"})
	remote, rec := trusted(t, nil)
	src := head31 + "paths: {}\ncomponents:\n  schemas:\n    A: {$ref: 'local.yaml#/Z'}\n    B: {$ref: '" + trustedPet + "'}\n"

	doc, diags := compileWithReader(t, filepath.Join(dir, "spec.yaml"), src, openapi.Options{AllowExternalRefs: true}, rec)

	assertNoErrorDiags(t, diags)
	assert.Equal(t, useTarget(t, doc, "A"), useTarget(t, doc, "B"))
	assert.Equal(t, []string{filepath.Join(dir, "local.yaml")}, rec.locations())
	assert.Empty(t, remote.requests())
}
