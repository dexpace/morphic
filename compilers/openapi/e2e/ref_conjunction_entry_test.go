// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// entryGridKeywords are ten keyword families written beside a $ref: a value
// set of each spelling, a bound of each kind, a union, members, a closure, a
// note, an extension and a keyword no dialect defines.
var entryGridKeywords = map[string]string{
	"enum":          "enum: [a]",
	"const":         "const: a",
	"minLength":     "minLength: 1",
	"maxProperties": "maxProperties: 3",
	"oneOf":         "oneOf: [{type: string}, {type: integer}]",
	"properties":    "properties: {b: {type: integer}}",
	"closure":       "additionalProperties: false",
	"description":   "description: d",
	"extension":     "x-mark: 1",
	"unknown":       "unknownKeyword: 1",
}

// entryGridTargets are six schemas the keywords are written beside a $ref to:
// a string, an object, the top type, a shape naming no kind, an array and a
// union.
var entryGridTargets = map[string]string{
	"string":  "T: {type: string}",
	"object":  "T: {type: object, properties: {a: {type: string}}}",
	"top":     "T: {}",
	"untyped": "T: {properties: {a: {type: string}}}",
	"array":   "T: {type: array, items: {type: string}}",
	"union":   "T: {oneOf: [{type: string}, {type: integer}]}",
}

// saidByTarget are the cells whose keyword the target already says: a string
// conjoined with a oneOf of a string and an integer admits what the string
// does, so the use is the reference, as written alone.
var saidByTarget = map[[2]string]bool{{"string", "oneOf"}: true}

// refEntrySpellings are the four ways an allOf writes a $ref and keywords k as
// its conjuncts: on one entry, the $ref first or last, and on two, the $ref's
// entry first or last.
func refEntrySpellings(k string) map[string]string {
	ref := "$ref: '#/components/schemas/T'"
	return map[string]string{
		"entry":           "{allOf: [{" + ref + ", " + k + "}]}",
		"entry, ref last": "{allOf: [{" + k + ", " + ref + "}]}",
		"split":           "{allOf: [{" + ref + "}, {" + k + "}]}",
		"split, ref last": "{allOf: [{" + k + "}, {" + ref + "}]}",
	}
}

// entryWritten is what a spelling compiled to, as the grid compares it: the
// document with its sources left out and the diagnostics as a sorted list of
// severity, code and pointer, each read at the use's pointer for any entry
// under it, and the IDs and keys it kept the $ref and the keyword under.
type entryWritten struct {
	doc   string
	diags []string
}

// relocater reads an entry's pointer under the use at pointer as the use's,
// and a whole entry's key without its index: one conjunct is written at
// /allOf/0 or /allOf/1 by the spelling alone.
func relocater(pointer string) func(string) string {
	entry := regexp.MustCompile(regexp.QuoteMeta(pointer+"/allOf/") + `[01]\b`)
	key := regexp.MustCompile(`openapi:allOf/[01]\b`)
	return func(s string) string {
		return key.ReplaceAllString(entry.ReplaceAllString(s, pointer), "openapi:allOf")
	}
}

// writtenAt compiles schema written at site beside the target and reads it as
// entryWritten.
func writtenAt(t *testing.T, site conjSite, target, schema string) (entryWritten, *ir.Document) {
	t.Helper()
	doc, diags := compileUnrefused(t, site.write("3.1.0", schema, target))
	relocate := relocater(site.pointer)
	var got entryWritten
	got.doc = relocate(withoutVersion(t, doc))
	for _, diagnostic := range diags {
		got.diags = append(got.diags, fmt.Sprintf("%s %s %s", diagnostic.Severity, diagnostic.Code, relocate(string(diagnostic.Provenance.Pointer))))
	}
	slices.Sort(got.diags)
	return got, doc
}

// keptNaming returns how many Unmodeled entries of doc hold what keyword
// writes: under its own key, or inside a whole entry kept under its path.
func keptNaming(doc *ir.Document, keyword string) int {
	n := 0
	for _, site := range unmodeledSites(doc) {
		switch {
		case site.key == "openapi:"+keyword, strings.HasSuffix(site.key, "/"+keyword):
			n++
		case strings.HasPrefix(site.key, "openapi:allOf") && strings.Contains(string(site.entry.Value), `"`+keyword+`"`):
			n++
		}
	}
	return n
}

// namesTarget reports whether a node of doc other than T's own names T.
func namesTarget(t *testing.T, doc *ir.Document) bool {
	t.Helper()
	others := *doc
	others.Types = maps.Clone(doc.Types)
	delete(others.Types, componentID("T"))
	return strings.Contains(withoutVersion(t, &others), `"target":"`+string(componentID("T"))+`"`)
}

// TestRefConjunction_AnEntrysRefAndKeywordsLowerAsTwoEntries holds an allOf
// entry writing a $ref beside a keyword to the composition listing the two on
// entries of their own, for ten keyword families over six targets, at a
// component and at a property, each in both orders: one document, up to the
// entries' pointers and a whole entry's index, and one diagnostic list up to
// message text. In each, the $ref is either the node a use names or kept, on
// one node, and the keyword is kept at most once, else changes what compiles
// or is one the target says already.
func TestRefConjunction_AnEntrysRefAndKeywordsLowerAsTwoEntries(t *testing.T) {
	t.Parallel()
	for _, site := range []conjSite{conjSites[0], conjSites[1]} {
		for tname, target := range entryGridTargets {
			bare, _ := writtenAt(t, site, target, "{allOf: [{$ref: '#/components/schemas/T'}]}")
			for kname, k := range entryGridKeywords {
				name := site.name + "/" + tname + "/" + kname
				keyword, _, _ := strings.Cut(k, ":")
				spellings := refEntrySpellings(k)
				want, _ := writtenAt(t, site, target, spellings["split"])
				for spelling, schema := range spellings {
					got, doc := writtenAt(t, site, target, schema)
					assert.Empty(t, cmp.Diff(want, got, cmp.AllowUnexported(entryWritten{})), "%s, %s", name, spelling)
					refs := keptNaming(doc, "$ref")
					assert.LessOrEqual(t, refs, 1, "%s, %s: the $ref is kept on one node", name, spelling)
					assert.NotEqual(t, refs == 1, namesTarget(t, doc), "%s, %s: the $ref is named or kept", name, spelling)
					kept := keptNaming(doc, keyword)
					assert.LessOrEqual(t, kept, 1, "%s, %s: %s is kept once", name, spelling, keyword)
					if saidByTarget[[2]string{tname, kname}] {
						assert.Equal(t, bare.doc, got.doc, "%s, %s: the use is the reference", name, spelling)
					} else if kept == 0 {
						assert.NotEqual(t, bare.doc, got.doc, "%s, %s: %s, kept nowhere, is typed", name, spelling, keyword)
					}
				}
			}
		}
	}
}

// TestRefConjunction_AnEntrysRestIsReadInItsDialect holds an identifier an
// allOf entry writes beside its $ref, in a dialect ignoring every keyword
// beside one, to that dialect's reading: the entry's rest writes no reference
// a conjunct reads, and is still written beside it.
func TestRefConjunction_AnEntrysRestIsReadInItsDialect(t *testing.T) {
	t.Parallel()
	_, diags := compileUnrefused(t, admissionSpec("3.1.0", "", "T: {type: object, properties: {a: {type: string}}}",
		"Use: {$schema: 'http://json-schema.org/draft-07/schema#', "+
			"allOf: [{$ref: '#/components/schemas/T', $id: 'urn:x', description: d}]}"))
	var found []string
	for _, diagnostic := range diags {
		if diagnostic.Provenance.Pointer == "/components/schemas/Use/allOf/0/$id" {
			found = append(found, diagnostic.Message)
		}
	}
	require.Len(t, found, 1)
	assert.Contains(t, found[0], "is read for nothing, as this schema's dialect ignores the keywords beside a $ref")
}

// TestRefConjunction_AValueSetBesideAnEntrysRefIsReportedOnce holds that a
// value set written beside the $ref of an allOf entry keeps the $ref once and
// reports it once, as beside a $ref at the position.
func TestRefConjunction_AValueSetBesideAnEntrysRefIsReportedOnce(t *testing.T) {
	t.Parallel()
	for name, schema := range map[string]string{
		"beside the $ref":       "{$ref: '#/components/schemas/Str', enum: [a]}",
		"beside an entry's ref": "{allOf: [{$ref: '#/components/schemas/Str', enum: [a]}]}",
	} {
		doc, diags := compileUnrefused(t, admissionSpec("3.1.0", "", "Str: {type: string}", "Use: "+schema))
		enum, ok := kindNode(t, doc, componentID("Use")).(*ir.Enum)
		require.True(t, ok, name)
		assert.Contains(t, enum.Unmodeled, "openapi:$ref", name)
		assert.Equal(t, 1, degradedInfosUnder(diags, "/components/schemas/Use"), "%s: one report for one $ref kept", name)
	}
}

// TestRefConjunction_AnEntrysRefIsKeptOnOneNode holds that a $ref an
// election passes over is kept on one node of the document, whether a union
// is written beside it or beside its entry.
func TestRefConjunction_AnEntrysRefIsKeptOnOneNode(t *testing.T) {
	t.Parallel()
	union := "oneOf: [{type: string}, {type: integer}]"
	for name, schema := range map[string]string{
		"beside the $ref":       "{$ref: '#/components/schemas/Top', " + union + "}",
		"beside an entry's ref": "{allOf: [{$ref: '#/components/schemas/Top', " + union + "}]}",
	} {
		doc, diags := compileUnrefused(t, admissionSpec("3.1.0", "", "Top: {}", "Use: "+schema))
		holders := 0
		for id, td := range doc.Types {
			if _, kept := td.Common().Unmodeled["openapi:$ref"]; kept && strings.Contains(string(id), "/Use") {
				holders++
			}
		}
		assert.Equal(t, 1, holders, "%s: the $ref is kept on one node", name)
		assert.Equal(t, 1, degradedInfosUnder(diags, "/components/schemas/Use"), "%s: and reported once", name)
	}
}
