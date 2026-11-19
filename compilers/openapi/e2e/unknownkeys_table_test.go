// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// This file covers one property: the validation rule table reports every key an
// object it judges neither defines nor admits, whether or not the lowering
// reads that object, and the lowering's census only keeps the entry. A report
// made only where an object lowers misses every object that is kept instead.
package e2e_test

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// findingLine renders one diagnostic as "severity code pointer", the form the
// tests below compare whole lists in.
func findingLine(d ir.Diagnostic) string {
	return fmt.Sprintf("%s %s %s", d.Severity, d.Code, d.Provenance.Pointer)
}

// findingsOf returns every finding carrying one of the codes, rendered by
// findingLine and sorted, so a list is compared whatever order the stages
// reported in.
func findingsOf(diags []ir.Diagnostic, codes ...string) []string {
	var out []string
	for _, d := range diags {
		if slices.Contains(codes, d.Code) {
			out = append(out, findingLine(d))
		}
	}
	slices.Sort(out)
	return out
}

// codesAt returns the code of every finding at the pointer, sorted: one key is
// one finding, so a second code about the same key shows here.
func codesAt(diags []ir.Diagnostic, pointer string) []string {
	var out []string
	for _, d := range diags {
		if string(d.Provenance.Pointer) == pointer {
			out = append(out, d.Code)
		}
	}
	slices.Sort(out)
	return out
}

// messageAt returns the message of the one finding of code at the pointer.
func messageAt(t *testing.T, diags []ir.Diagnostic, code, pointer string) string {
	t.Helper()
	var out []string
	for _, d := range diags {
		if d.Code == code && string(d.Provenance.Pointer) == pointer {
			out = append(out, d.Message)
		}
	}
	require.Len(t, out, 1, "%s at %s", code, pointer)
	return out[0]
}

// unknownKeyLine is the finding the table draws for a key an object does not
// define.
func unknownKeyLine(pointer string) string {
	return "warning " + diag.UnknownObjectKey + " " + pointer
}

// unreferencedComponentsSpec writes one undeclared key on every kind of
// component nothing references, and on a schema, whose keywords are the
// dialect's to judge.
const unreferencedComponentsSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S: {type: string, bogus: 1}
  parameters:
    P: {name: p, in: query, schema: {type: string}, bogus: 1}
  responses:
    R: {description: d, bogus: 1}
  requestBodies:
    B: {content: {application/json: {schema: {type: string}}}, bogus: 1}
  headers:
    H: {schema: {type: string}, bogus: 1}
  examples:
    E: {value: 1, bogus: 1}
  links:
    L: {operationId: x, bogus: 1}
  callbacks:
    C: {"{$request.body#/u}": {post: {responses: {"200": {description: ok}}, bogus: 1}}}
  securitySchemes:
    K: {type: apiKey, name: k, in: header, bogus: 1}
  pathItems:
    I: {get: {responses: {"200": {description: ok}}}, bogus: 1}
`

// TestUnknownKeys_UnreferencedComponents holds the table to every component no
// operation reaches. Each kind draws one warning at its key and nothing else;
// a security scheme, which the lowering reads, is reported once and not twice,
// and a schema's keyword stays the dialect's info.
func TestUnknownKeys_UnreferencedComponents(t *testing.T) {
	t.Parallel()
	doc, diags := compileAnnotationSpec(t, "unreferenced", unreferencedComponentsSpec)
	requireNoErrorDiagnostics(t, diags)

	pointers := []string{
		"/components/responses/R/bogus",
		"/components/parameters/P/bogus",
		"/components/examples/E/bogus",
		"/components/requestBodies/B/bogus",
		"/components/headers/H/bogus",
		"/components/links/L/bogus",
		"/components/callbacks/C/{$request.body#~1u}/post/bogus",
		"/components/pathItems/I/bogus",
		"/components/securitySchemes/K/bogus",
	}
	want := make([]string, 1, len(pointers)+1)
	want[0] = "info " + diag.UnknownSchemaKeyword + " /components/schemas/S/bogus"
	for _, p := range pointers {
		want = append(want, unknownKeyLine(p))
		assert.Equal(t, []string{diag.UnknownObjectKey}, codesAt(diags, p), "one finding about the key at %s", p)
	}
	slices.Sort(want)
	assert.Equal(t, want, findingsOf(diags, diag.UnknownObjectKey, diag.UnknownSchemaKeyword),
		"every key once, and a schema's keyword as the dialect's")
	all := make([]string, 0, len(diags))
	for _, d := range diags {
		all = append(all, findingLine(d))
	}
	slices.Sort(all)
	assert.Equal(t, want, all, "nothing else is reported about this document")

	msg := messageAt(t, diags, diag.UnknownObjectKey, "/components/responses/R/bogus")
	assert.Equal(t, `key "bogus" is not defined by the Response Object it is written on and is not an x- extension`, msg)
	assert.NotContains(t, msg, "kept verbatim", "where the key is kept is the lowering's to say, not the table's")

	// What the IR keeps of each key is unchanged: a component nothing uses is
	// kept whole, and the scheme's key on the scheme.
	for _, kind := range []string{
		"responses/R", "parameters/P", "examples/E", "requestBodies/B", "headers/H",
		"links/L", "callbacks/C", "pathItems/I",
	} {
		entry, ok := doc.Unmodeled["openapi:components/"+kind]
		if assert.True(t, ok, "component %s is kept whole; have %v", kind, slices.Sorted(maps.Keys(doc.Unmodeled))) {
			assert.Equal(t, ir.ReasonNoIRHome, entry.Reason, kind)
			assert.Contains(t, string(entry.Value), `"bogus":`, kind)
		}
	}
	scheme, found := findUnmodeled(unmodeledSites(doc), "openapi:bogus", "1")
	require.True(t, found, "the scheme's key is kept")
	assert.Equal(t, ir.ReasonOutOfScope, scheme.entry.Reason)
}

// keptWholeSpec writes an undeclared key at every object the lowering keeps
// verbatim instead of reading: a nested encoding, positional encodings, a link
// entry, components nothing references, and the headers of encodings.
const keptWholeSpec = `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /u:
    post:
      operationId: up
      parameters:
        - name: q
          in: query
          schema: {type: string, xml: {name: s, bogusXml: 1}}
      requestBody:
        content:
          multipart/form-data:
            schema: {type: object, properties: {a: {type: string}}}
            encoding:
              a:
                contentType: text/plain
                encoding: {x: {bogusNested: a, headers: {Content-Type: {schema: {type: string}}}}}
          multipart/mixed:
            schema: {type: array}
            prefixEncoding: [{bogusPrefix: a}]
            itemEncoding: {bogusItem: b, headers: {Content-Type: {schema: {type: string}}}}
      responses:
        "200":
          description: ok
          links:
            L: {operationId: up, bogusLink: 1}
components:
  headers:
    UnusedHeader: {bogusHeader: 1, schema: {type: string}}
  parameters:
    UnusedParam: {name: p, in: query, bogusParam: 1, schema: {type: string}}
  requestBodies:
    UnusedBody:
      content:
        multipart/form-data:
          schema: {type: object, properties: {a: {type: string}}}
          encoding:
            a: {bogusUnusedEnc: x, headers: {Content-Type: {schema: {type: string}}}}
  responses:
    UnusedResp: {description: d, bogusResp: 1}
  links:
    UnusedLink: {operationId: up, bogusCompLink: 1}
  examples:
    UnusedEx: {value: 1, bogusEx: 1}
`

// TestUnknownKeys_ObjectsKeptWhole holds the table to objects the lowering
// keeps verbatim: the keys inside a nested encoding, a positional encoding, an
// item encoding, a kept link, a schema's xml and the components nothing uses.
// Each of the eleven keys is reported once, at the key, and the three
// Content-Type entries of a headers map the lowering never reads are reported
// as reserved header names.
func TestUnknownKeys_ObjectsKeptWhole(t *testing.T) {
	t.Parallel()
	_, diags := compileAnnotationSpec(t, "kept-whole", keptWholeSpec)
	requireNoErrorDiagnostics(t, diags)

	const (
		media  = "/paths/~1u/post/requestBody/content/"
		unused = "/components/requestBodies/UnusedBody/content/multipart~1form-data/encoding/a/"
	)
	unknown := []string{
		"/paths/~1u/post/parameters/0/schema/xml/bogusXml",
		media + "multipart~1form-data/encoding/a/encoding/x/bogusNested",
		media + "multipart~1mixed/prefixEncoding/0/bogusPrefix",
		media + "multipart~1mixed/itemEncoding/bogusItem",
		"/paths/~1u/post/responses/200/links/L/bogusLink",
		"/components/responses/UnusedResp/bogusResp",
		"/components/parameters/UnusedParam/bogusParam",
		"/components/examples/UnusedEx/bogusEx",
		unused + "bogusUnusedEnc",
		"/components/headers/UnusedHeader/bogusHeader",
		"/components/links/UnusedLink/bogusCompLink",
	}
	want := make([]string, 0, len(unknown))
	for _, p := range unknown {
		want = append(want, unknownKeyLine(p))
		assert.Equal(t, []string{diag.UnknownObjectKey}, codesAt(diags, p), "one finding about the key at %s", p)
	}
	slices.Sort(want)
	assert.Equal(t, want, findingsOf(diags, diag.UnknownObjectKey), "eleven keys, each once")

	reserved := []string{
		media + "multipart~1form-data/encoding/a/encoding/x/headers/Content-Type",
		media + "multipart~1mixed/itemEncoding/headers/Content-Type",
		unused + "headers/Content-Type",
	}
	wantReserved := make([]string, 0, len(reserved))
	for _, p := range reserved {
		wantReserved = append(wantReserved, "warning "+diag.ReservedHeaderName+" "+p)
		assert.Equal(t, []string{diag.ReservedHeaderName}, codesAt(diags, p), "one finding about the entry at %s", p)
	}
	slices.Sort(wantReserved)
	assert.Equal(t, wantReserved, findingsOf(diags, diag.ReservedHeaderName), "three entries, each once")
}

// TestUnknownKeys_InsideAKeptSchema holds the table to the xml and externalDocs
// objects of a schema, lowered and kept verbatim alike. A keyword of a schema
// kept inside a validation-only keyword is the dialect's, not the table's.
func TestUnknownKeys_InsideAKeptSchema(t *testing.T) {
	t.Parallel()
	_, diags := compileAnnotationSpec(t, "kept-schema", `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Lowered: {type: string, xml: {name: s, bogusXml: 1}, externalDocs: {url: 'https://example.com', bogusDocs: 1}}
    Kept:
      type: string
      not: {type: integer, xml: {name: s, bogusXml: 1}, externalDocs: {url: 'https://example.com', bogusDocs: 1}}
`)
	requireNoErrorDiagnostics(t, diags)

	pointers := []string{
		"/components/schemas/Lowered/xml/bogusXml",
		"/components/schemas/Lowered/externalDocs/bogusDocs",
		"/components/schemas/Kept/not/xml/bogusXml",
		"/components/schemas/Kept/not/externalDocs/bogusDocs",
	}
	want := make([]string, 0, len(pointers))
	for _, p := range pointers {
		want = append(want, unknownKeyLine(p))
		assert.Equal(t, []string{diag.UnknownObjectKey}, codesAt(diags, p), "one finding about the key at %s", p)
	}
	slices.Sort(want)
	assert.Equal(t, want, findingsOf(diags, diag.UnknownObjectKey), "four keys, each once")
	assert.Equal(t, []ir.Severity{ir.SeverityInfo},
		diagsAt(diags, diag.ValidationOnlyKeyword, "/components/schemas/Kept"))
	for _, p := range []string{"type", "xml", "externalDocs"} {
		assert.Empty(t, codesAt(diags, "/components/schemas/Kept/not/"+p),
			"a keyword of the kept schema is the dialect's, and a defined one draws nothing")
	}
}

// objectKinds is one undeclared key at each kind of object a document is
// written in, for each minor of OpenAPI that reads the key the same way.
const objectKinds = `info: {title: T, version: "1", bogusInfo: 1}
bogusRoot: 1
paths:
  /a:
    bogusItem: 1
    post:
      operationId: p
      bogusOp: 1
      parameters:
        - {name: q, in: query, schema: {type: string}, bogusParam: 1}
        - {$ref: '#/components/parameters/P', bogusRef: 1}
      requestBody:
        bogusBody: 1
        content:
          application/json:
            bogusMedia: 1
            schema: {type: string, xml: {name: s, bogusXml: 1}, externalDocs: {url: 'https://example.com', bogusDocs: 1}}
      responses:
        "200":
          description: ok
          bogusResp: 1
          headers:
            X-H: {schema: {type: string}, bogusHeader: 1}
components:
  parameters:
    P: {name: p, in: query, schema: {type: string}}
`

// TestUnknownKeys_ReportedOnceInEveryObjectAndMinor holds each undeclared key to
// one finding at the key in OpenAPI 3.0, 3.1 and 3.2, in every kind of object
// the lowering reads, and to no other code about it: a key the lowering also
// keeps is the table's to report and nobody else's.
func TestUnknownKeys_ReportedOnceInEveryObjectAndMinor(t *testing.T) {
	t.Parallel()
	pointers := []string{
		"/info/bogusInfo", "/bogusRoot", "/paths/~1a/bogusItem", "/paths/~1a/post/bogusOp",
		"/paths/~1a/post/parameters/0/bogusParam", "/paths/~1a/post/parameters/1/bogusRef",
		"/paths/~1a/post/requestBody/bogusBody",
		"/paths/~1a/post/requestBody/content/application~1json/bogusMedia",
		"/paths/~1a/post/requestBody/content/application~1json/schema/xml/bogusXml",
		"/paths/~1a/post/requestBody/content/application~1json/schema/externalDocs/bogusDocs",
		"/paths/~1a/post/responses/200/bogusResp",
		"/paths/~1a/post/responses/200/headers/X-H/bogusHeader",
	}
	want := make([]string, 0, len(pointers))
	for _, p := range pointers {
		want = append(want, unknownKeyLine(p))
	}
	slices.Sort(want)

	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			_, diags := compileAnnotationSpec(t, "kinds", "openapi: "+version+"\n"+objectKinds)
			requireNoErrorDiagnostics(t, diags)

			assert.Equal(t, want, findingsOf(diags, diag.UnknownObjectKey), "each key once, nothing more")
			for _, p := range pointers {
				assert.Equal(t, []string{diag.UnknownObjectKey}, codesAt(diags, p), "no second code about the key at %s", p)
			}
		})
	}
}

// TestUnknownKeys_ExtensionsAndDefinedKeysDrawNothing is the control the rows
// above need: a key the object defines, and an x- extension, draw no finding in
// any of the same objects.
func TestUnknownKeys_ExtensionsAndDefinedKeysDrawNothing(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			_, diags := compileAnnotationSpec(t, "defined", "openapi: "+version+`
info: {title: T, version: "1", x-a: 1}
x-root: 1
paths:
  /a:
    x-item: 1
    post:
      operationId: p
      x-op: 1
      parameters:
        - {name: q, in: query, schema: {type: string, xml: {name: s, x-xml: 1}}, x-param: 1}
      requestBody:
        x-body: 1
        content:
          application/json: {x-media: 1, schema: {type: string}}
      responses:
        "200":
          description: ok
          x-resp: 1
          headers:
            X-H: {schema: {type: string}, x-header: 1}
`)
			requireNoErrorDiagnostics(t, diags)
			assert.Empty(t, findingsOf(diags, diag.UnknownObjectKey, diag.UnknownSchemaKeyword))
		})
	}
}

// TestUnknownKeys_LaterMinorKeyIsTheLaterVersionsFinding holds a key OpenAPI
// defines in a later minor than the document's to its own finding: the document
// is told the key is a later version's, once, and is not also told it is
// undefined.
func TestUnknownKeys_LaterMinorKeyIsTheLaterVersionsFinding(t *testing.T) {
	t.Parallel()
	_, diags := compileAnnotationSpec(t, "later", `openapi: 3.1.0
info: {title: T, version: "1"}
tags:
  - {name: t, summary: s, kind: nav, parent: p}
paths:
  /a:
    query: {operationId: q, responses: {"200": {description: ok}}}
    additionalOperations:
      PURGE: {operationId: purge, responses: {"200": {description: ok}}}
components:
  examples:
    E: {dataValue: 1, serializedValue: "1"}
`)
	requireNoErrorDiagnostics(t, diags)

	for _, p := range []string{
		"/tags/0/summary", "/tags/0/kind", "/tags/0/parent", "/paths/~1a/query",
		"/paths/~1a/additionalOperations", "/components/examples/E/dataValue",
		"/components/examples/E/serializedValue",
	} {
		assert.Equal(t, []string{diag.LaterVersionConstruct}, codesAt(diags, p), "the later minor's key at %s", p)
	}
	assert.Empty(t, findingsOf(diags, diag.UnknownObjectKey), "a key a later minor defines is not undefined")
}

// TestUnknownKeys_MergedKeyIsPlacedWhereItIsWritten holds the one finding of a
// key a path item takes from a merge source to the place the table puts every
// merged field: where the key is written, with the message naming the place the
// object reads it. The operation still keeps the entry (GitHub #1945 asks which
// of the two coordinates an entry should record).
func TestUnknownKeys_MergedKeyIsPlacedWhereItIsWritten(t *testing.T) {
	t.Parallel()
	doc, diags := compileAnnotationSpec(t, "merged", mergedAndRepeatedSpec)

	var found []ir.Diagnostic
	for _, d := range diags {
		if d.Code == diag.UnknownObjectKey {
			found = append(found, d)
		}
	}
	require.Len(t, found, 1, "one finding for the merged key: %+v", found)
	assert.Equal(t, ir.SeverityWarning, found[0].Severity)
	assert.Equal(t, "/x-base/mergedBogus", string(found[0].Provenance.Pointer))
	assert.True(t, strings.HasSuffix(found[0].Message, "read at /paths/~1merged/mergedBogus"),
		"the message names where the path item reads it: %s", found[0].Message)

	op, ok := opByName(doc, "m")
	require.True(t, ok)
	entry := unmodeledEntry(t, op.Unmodeled, "openapi:pathItem/mergedBogus")
	assert.JSONEq(t, `1`, string(entry.Value))
	assert.Equal(t, ir.ReasonOutOfScope, entry.Reason)
}

// mergedAndRepeatedSpec holds a key merged into a path item, an encoding entry
// no part of an open body names, and a repeated key in a default.
const mergedAndRepeatedSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
x-base: &base {mergedBogus: 1}
paths:
  /merged:
    <<: *base
    get: {operationId: m, responses: {"200": {description: ok}}}
  /open:
    post:
      operationId: open
      requestBody:
        content:
          multipart/form-data:
            schema: {type: object, properties: {a: {type: string}}, additionalProperties: true}
            encoding: {extra: {contentType: text/plain}}
      responses: {"204": {description: ok}}
components:
  schemas:
    D: {type: object, default: {a: 1, a: 2}}
`

// inSchemaKeywordPosition reports whether the pointer names a keyword of a
// Schema Object, whose keywords are the dialect's to judge, rather than a key
// of one of the OpenAPI objects a schema holds (xml, externalDocs,
// discriminator).
func inSchemaKeywordPosition(pointer string) bool {
	segments := strings.Split(pointer, "/")
	if len(segments) >= 3 && slices.Contains([]string{"xml", "externalDocs", "discriminator"}, segments[len(segments)-2]) {
		return false
	}
	return slices.ContainsFunc(segments, func(s string) bool {
		return slices.Contains([]string{
			"schemas", "schema", "definitions", "$defs", "properties", "items", "not", "allOf", "anyOf", "oneOf",
			"additionalProperties", "patternProperties", "prefixItems", "contains", "if", "then", "else",
		}, s)
	})
}

// corpusKeptKeys compiles the spec at path as the engine would, or reports
// that it is none this compiler reads.
func corpusKeptKeys(t *testing.T, path string) (*ir.Document, []ir.Diagnostic, bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	src := compilers.Source{Path: path, Data: data}
	if _, _, ok := openapi.New().Detect(t.Context(), src, compilers.Options{}); !ok {
		return nil, nil, false
	}
	doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{src},
		compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
	if err != nil || doc == nil {
		return nil, nil, false // a refused document keeps nothing to compare
	}
	return doc, diags, true
}

// TestUnknownKeys_EveryKeptKeyIsReported holds the census and the table to each
// other over the whole corpus: an object key the lowering kept under
// out_of_scope has an unknown-object-key finding at the key, one naming the
// place a merged key is read at, or for a key an inline merge source writes,
// one naming the key at no pointer. An object missing a field the model also
// misses would keep the key and report nothing.
func TestUnknownKeys_EveryKeptKeyIsReported(t *testing.T) {
	t.Parallel()
	// Entries the other readers answer for with a finding of their own: a
	// schema's keyword, an encoding entry naming no property, an entry another
	// construct holds, and the keys later minors define. A field load reports
	// as having no effect answers for each entry it holds.
	otherCodes := []string{
		diag.UnknownSchemaKeyword, diag.UnknownEncodingKey, diag.UnknownKeyBudget,
		diag.ValidationOnlyKeyword, diag.LaterVersionConstruct, diag.InapplicableLocationKeyword,
		diag.InvalidLocationKeyword, diag.Validation + "/validation-no-effect",
		"openapi/unknown-key-entry-already-claimed", "openapi/unknown-key-entry-unreachable",
	}
	var swept int
	for _, path := range corpusSpecs(t) {
		doc, diags, ok := corpusKeptKeys(t, path)
		if !ok {
			continue
		}
		swept++
		for _, site := range unmodeledSites(doc) {
			p := string(site.entry.Provenance.Pointer)
			last := p[strings.LastIndex(p, "/")+1:]
			if site.entry.Reason != ir.ReasonOutOfScope || p == "" || strings.HasPrefix(last, "x-") ||
				strings.HasPrefix(last, "$") || !strings.HasPrefix(site.key, "openapi:") ||
				inSchemaKeywordPosition(p) || p == "/jsonSchemaDialect" {
				continue
			}
			if slices.ContainsFunc(diags, func(d ir.Diagnostic) bool {
				at := string(d.Provenance.Pointer)
				return at == p && slices.Contains(otherCodes, d.Code) ||
					d.Code == diag.Validation+"/validation-no-effect" && strings.HasPrefix(p, at+"/")
			}) {
				continue
			}
			// A key merged in from a source written inline is placed where it is
			// written, which no pointer reaches, so its finding names the key alone.
			inline := fmt.Sprintf("key %q ", last)
			reported := slices.ContainsFunc(diags, func(d ir.Diagnostic) bool {
				return d.Code == diag.UnknownObjectKey && (string(d.Provenance.Pointer) == p ||
					strings.HasSuffix(d.Message, "read at "+p) ||
					d.Provenance.Pointer == "" && strings.HasPrefix(d.Message, inline))
			})
			assert.True(t, reported, "%s: %s kept as %s at %s has no unknown-object-key finding",
				path, site.path, site.key, p)
		}
	}
	require.Positive(t, swept, "the sweep compiled no OpenAPI spec")
}

// TestKeywordLists_AMutatedCopyChangesNoReader is #1316's reader-side half:
// the list a caller collects from the seam is its own, so overwriting it
// leaves the lowering still keeping each dialect keyword and the census still
// leaving each decided keyword alone.
func TestKeywordLists_AMutatedCopyChangesNoReader(t *testing.T) {
	t.Parallel()
	mine := slices.Collect(annotation.DialectKeywords())
	require.NotEmpty(t, mine)
	for i := range mine {
		mine[i] = "x-overwritten"
	}

	doc, diags := compileAnnotationSpec(t, "dialect", `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    A:
      $id: 'urn:example:a'
      $schema: 'https://json-schema.org/draft/2020-12/schema'
      $vocabulary: {'https://json-schema.org/draft/2020-12/vocab/core': true}
      $comment: not for end users
      type: string
`)
	requireNoErrorDiagnostics(t, diags)
	got := typeByName(doc, "A").Common().Unmodeled
	for _, keyword := range []string{"$id", "$schema", "$vocabulary"} {
		entry := unmodeledEntry(t, got, "openapi:"+keyword)
		assert.Equal(t, ir.ReasonOutOfScope, entry.Reason, keyword)
	}
	assert.NotContains(t, got, "openapi:$comment", "a decided keyword is left to its own reader")
	assert.Len(t, slices.Collect(annotation.DialectKeywords()), 3)
}
