// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// This file covers one property: a key a mapping writes twice is read as its
// last copy wherever it is written, a kept construct included, and is reported
// once, as a warning, by the rule table. A document load accepts with a warning
// does not stop compiling because the repeat sits where the lowering keeps a
// value verbatim instead of reading it.
package e2e_test

import (
	"encoding/json/jsontext"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// duplicateKeyCode is the finding load's table draws for a key written twice.
const duplicateKeyCode = diag.Validation + "/validation-duplicate-key"

// repeatedKeysSpec writes a key twice in an extension of the root, in an
// extension of an encoding entry, in an undefined key of that entry and in a
// nested encoding's own field. The first copy of each is a wrong value, so a
// reader that kept it, or kept nothing, shows.
const repeatedKeysSpec = `openapi: 3.2.0
info: {title: T, version: "1"}
x-limits: {rate: 1, rate: 2}
paths:
  /u:
    post:
      requestBody:
        content:
          multipart/form-data:
            schema: {type: object, properties: {a: {type: string}}}
            encoding:
              a:
                contentType: text/plain
                bogus: {k: 1, k: 2}
                encoding: {x: {contentType: text/plain, contentType: image/png}}
      responses: {"204": {description: ok}}
`

// compileNamed compiles src as the source named path, so its extension decides
// whether it is read as JSON or as YAML.
func compileNamed(t *testing.T, path, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: path, Data: []byte(src)}}, compilers.Options{})
	require.NoError(t, err)
	requireVerifies(t, doc, diags)
	return doc, diags
}

// duplicateWarnings returns the repeated-key findings, in pointer order.
func duplicateWarnings(diags []ir.Diagnostic) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, diagnostic := range diags {
		if diagnostic.Code == duplicateKeyCode {
			out = append(out, diagnostic)
		}
	}
	slices.SortFunc(out, func(a, b ir.Diagnostic) int {
		return strings.Compare(string(a.Provenance.Pointer), string(b.Provenance.Pointer))
	})
	return out
}

// requireNoRefusal fails when a repeat refused the document, or was announced
// as unserializable, as it was before a repeat was kept as its last copy.
func requireNoRefusal(t *testing.T, diags []ir.Diagnostic) {
	t.Helper()
	requireNoErrorDiagnostics(t, diags)
	for _, diagnostic := range diags {
		assert.NotEqual(t, diag.UnpreservableConstruct, diagnostic.Code, "a repeated key is not unpreservable: %+v", diagnostic)
		assert.NotContains(t, diagnostic.Message, "could not be serialized", "an extension with a repeated key converts: %+v", diagnostic)
	}
}

// TestCompile_ARepeatedKeyIsKeptAsItsLastCopy compiles a document writing a key
// twice in an extension and in the entries a multipart part keeps verbatim. It
// compiles, each repeat is one warning at its later copy and nothing else, and
// every kept value is the last copy, the value the typed IR beside it reads.
func TestCompile_ARepeatedKeyIsKeptAsItsLastCopy(t *testing.T) {
	t.Parallel()
	doc, diags := compileAnnotationSpec(t, "repeated", repeatedKeysSpec)
	requireNoRefusal(t, diags)

	const part = "/paths/~1u/post/requestBody/content/multipart~1form-data/encoding/a/"
	warnings := duplicateWarnings(diags)
	require.Len(t, warnings, 3, "one warning per repeated key: %+v", warnings)
	wantPointers := []jsontext.Pointer{jsontext.Pointer(part + "bogus/k"), jsontext.Pointer(part + "encoding/x/contentType"), "/x-limits/rate"}
	for i, diagnostic := range warnings {
		assert.Equal(t, wantPointers[i], diagnostic.Provenance.Pointer)
		assert.Equal(t, ir.SeverityWarning, diagnostic.Severity)
		assert.Contains(t, diagnostic.Message, "more than once")
		assert.Contains(t, diagnostic.Message, "last copy")
	}
	assert.Equal(t, 14, warnings[0].Provenance.Position.Line, "the later copy of bogus.k")
	assert.Equal(t, 3, warnings[2].Provenance.Position.Line, "the later copy of x-limits.rate")

	limits := unmodeledEntry(t, doc.Unmodeled, "openapi:x-limits")
	assert.JSONEq(t, `{"rate":2}`, string(limits.Value))
	assert.Equal(t, ir.ReasonVendorExtension, limits.Reason)

	operations := allOperations(doc)
	require.Len(t, operations, 1)
	require.NotNil(t, operations[0].Request)
	kept := operations[0].Request.Contents[0].Unmodeled
	assert.JSONEq(t, `{"k":2}`, string(unmodeledEntry(t, kept, "openapi:encoding/a/bogus").Value))
	assert.JSONEq(t, `{"x":{"contentType":"image/png"}}`,
		string(unmodeledEntry(t, kept, "openapi:encoding/a/encoding").Value))
}

// TestCompile_ARepeatedDefaultIsKeptAsItsLastCopy holds a repeat inside a value
// a schema keeps verbatim: it compiles, with the one warning at the repeat, and
// keeps the last copy.
func TestCompile_ARepeatedDefaultIsKeptAsItsLastCopy(t *testing.T) {
	t.Parallel()
	doc, diags := compileAnnotationSpec(t, "repeated-default", mergedAndRepeatedSpec)
	requireNoRefusal(t, diags)

	assert.Empty(t, diagsAt(diags, diag.UnpreservableConstruct, "/components/schemas/D/default"))
	entry := unmodeledEntry(t, typeByName(doc, "D").Common().Unmodeled, "openapi:default")
	assert.JSONEq(t, `{"a":2}`, string(entry.Value))
	warnings := duplicateWarnings(diags)
	require.Len(t, warnings, 1, "%+v", warnings)
	assert.Equal(t, jsontext.Pointer("/components/schemas/D/default/a"), warnings[0].Provenance.Pointer)
	assert.Equal(t, ir.SeverityWarning, warnings[0].Severity)
}

// TestCompile_ARepeatedKeyIsReadAsItsLastCopyInEverySpelling holds the rule at
// the root and in a schema, in YAML and in JSON, through an alias and through a
// merge key. Each repeat is one warning at its later copy; a key a merge source
// repeats is reported where the source writes it, once, however many mappings
// merge it; and the value kept is the copy the model reads, the last, bar a
// merge source's, which the model reads at its first
// (docs/parser-library.md#merge-keys).
func TestCompile_ARepeatedKeyIsReadAsItsLastCopyInEverySpelling(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n"
	const jsonHead = "{\n  \"openapi\": \"3.1.0\",\n  \"info\": {\"title\": \"T\", \"version\": \"1\"},\n  \"paths\": {},\n"
	for _, tc := range []struct {
		name, path, src string
		// warned is where the one warning sits, or empty for a control.
		warned jsontext.Pointer
		// line is the line of the later copy.
		line int
		// root is the kept value of the root extension x-limits, schema the
		// kept default of schema D; either empty is not asserted.
		root, schema string
	}{
		{name: "root in YAML", path: "a.yaml", src: head + "x-limits: {rate: 1, rate: 2}\n",
			warned: "/x-limits/rate", line: 4, root: `{"rate":2}`},
		{name: "root in JSON", path: "a.json", src: jsonHead + "  \"x-limits\": {\n    \"rate\": 1,\n    \"rate\": 2\n  }\n}\n",
			warned: "/x-limits/rate", line: 7, root: `{"rate":2}`},
		{name: "a key written as an alias", path: "a.yaml", src: head + "x-limits: {&k rate: 1, *k : 2}\n",
			warned: "/x-limits/rate", line: 4, root: `{"rate":2}`},
		{name: "a merge source that repeats a key", path: "a.yaml",
			src:    head + "x-base: &b {rate: 1, rate: 2}\nx-limits: {<<: *b}\n",
			warned: "/x-base/rate", line: 4, root: `{"rate":1}`},
		{name: "an own key over a merged one is no repeat", path: "a.yaml",
			src: head + "x-limits: {<<: {rate: 1}, rate: 2}\n", root: `{"rate":2}`},
		{name: "a schema default in YAML", path: "a.yaml",
			src:    head + "components:\n  schemas:\n    D: {type: object, default: {a: 1, a: 2}}\n",
			warned: "/components/schemas/D/default/a", line: 6, schema: `{"a":2}`},
		{name: "a schema default in JSON", path: "a.json",
			src: jsonHead + "  \"components\": {\"schemas\": {\"D\": {\"type\": \"object\", \"default\": {\n" +
				"    \"a\": 1,\n    \"a\": 2\n  }}}}\n}\n",
			warned: "/components/schemas/D/default/a", line: 7, schema: `{"a":2}`},
		{name: "a default written once", path: "a.yaml",
			src:    head + "components:\n  schemas:\n    D: {type: object, default: {a: 1, b: 2}}\n",
			schema: `{"a":1,"b":2}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileNamed(t, tc.path, tc.src)
			requireNoRefusal(t, diags)

			warnings := duplicateWarnings(diags)
			if tc.warned == "" {
				assert.Empty(t, warnings, "no key is written twice")
			} else if assert.Len(t, warnings, 1, "reported once: %+v", warnings) {
				assert.Equal(t, tc.warned, warnings[0].Provenance.Pointer)
				assert.Equal(t, ir.SeverityWarning, warnings[0].Severity)
				assert.Equal(t, tc.line, warnings[0].Provenance.Position.Line)
				assert.Equal(t, []string{duplicateKeyCode}, codesAt(diags, string(tc.warned)), "no second code about the key")
			}
			if tc.root != "" {
				assert.JSONEq(t, tc.root, string(unmodeledEntry(t, doc.Unmodeled, "openapi:x-limits").Value))
			}
			if tc.schema != "" {
				assert.JSONEq(t, tc.schema,
					string(unmodeledEntry(t, typeByName(doc, "D").Common().Unmodeled, "openapi:default").Value))
			}
		})
	}
}
