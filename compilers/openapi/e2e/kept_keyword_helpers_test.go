// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// keptKeywordVersions are the OpenAPI versions a case runs at unless it names
// its own.
var keptKeywordVersions = []string{"3.0.3", "3.1.0"}

// The principals a case writes its keyword beside: a use of the string, the
// number, the object and the array of keptKeywordCommon.
const (
	keptStr = "{$ref: '#/components/schemas/Str'}"
	keptNum = "{$ref: '#/components/schemas/Num'}"
	keptObj = "{$ref: '#/components/schemas/Obj'}"
	keptArr = "{$ref: '#/components/schemas/Arr'}"
)

// keptOnly31 are the versions of a case for a keyword 3.0 does not define.
var keptOnly31 = []string{"3.1.0"}

// keptKeywordEntry is one Unmodeled entry a document holds for a keyword,
// with where the keyword is written.
type keptKeywordEntry struct {
	reason  ir.UnmodeledReason
	pointer string
	value   string
}

// keptKeywordsOf returns every Unmodeled entry of doc whose provenance names a
// pointer at or under the component named, by key. It walks the whole
// document, so a keyword kept on a node that moved, a carrier or an alias is
// found where it stands, and a key kept on two nodes shows as two entries.
func keptKeywordsOf(doc *ir.Document, name string) map[string][]keptKeywordEntry {
	return keptKeywordsUnder(doc, "/components/schemas/"+name)
}

// keptKeywordsUnder is keptKeywordsOf for any pointer.
func keptKeywordsUnder(doc *ir.Document, at string) map[string][]keptKeywordEntry {
	out := map[string][]keptKeywordEntry{}
	for _, site := range unmodeledSites(doc) {
		p := string(site.entry.Provenance.Pointer)
		if p != at && !strings.HasPrefix(p, at+"/") {
			continue
		}
		out[site.key] = append(out[site.key], keptKeywordEntry{
			reason: site.entry.Reason, pointer: p, value: string(site.entry.Value),
		})
	}
	return out
}

// underPointer reports whether p is at or beneath at.
func underPointer(p, at string) bool {
	return p == at || strings.HasPrefix(p, at+"/")
}

// requireKeptKeywords holds got to exactly the keys of want, each once, each
// with its reason (an empty reason in want accepts any), and each written at
// or under entry: the pointer of the schema object that writes it, which is
// the entry's for a keyword of an entry. A kept branch (whole) names the entry
// itself.
func requireKeptKeywords(
	t *testing.T, name string, got map[string][]keptKeywordEntry,
	want map[string]ir.UnmodeledReason, entry string, whole bool,
) {
	t.Helper()
	gotKeys := slices.Sorted(func(yield func(string) bool) {
		for key := range got {
			if !yield(key) {
				return
			}
		}
	})
	wantKeys := slices.Sorted(func(yield func(string) bool) {
		for key := range want {
			if !yield(key) {
				return
			}
		}
	})
	require.Equal(t, wantKeys, gotKeys, "%s: the keys kept under Unmodeled", name)
	for _, key := range wantKeys {
		entries := got[key]
		require.Len(t, entries, 1, "%s: %s is kept once", name, key)
		if want[key] != "" {
			assert.Equal(t, want[key], entries[0].reason, "%s: the reason of %s", name, key)
		}
		if whole {
			assert.Equal(t, entry, entries[0].pointer, "%s: %s names the branch it keeps", name, key)
			continue
		}
		assert.True(t, underPointer(entries[0].pointer, entry),
			"%s: %s is written at %s, not at or under the entry %s", name, key, entries[0].pointer, entry)
	}
}

// keptKeywordReasons is the reason of each key of got, for two spellings of
// one schema to be compared with their provenance set aside.
func keptKeywordReasons(got map[string][]keptKeywordEntry) map[string]ir.UnmodeledReason {
	out := map[string]ir.UnmodeledReason{}
	for key, entries := range got {
		out[key] = entries[0].reason
	}
	return out
}

// keptFinding is one finding a schema draws: its severity and code, where it
// is drawn relative to the schema, and words its message holds. An at of
// "a|b" accepts either pointer, for a report anchored at the entry or at its
// keyword.
type keptFinding struct {
	severity ir.Severity
	code     string
	at       string
	words    []string
}

// keptInfo is an info of the code at the pointer, naming the words.
func keptInfo(code, at string, words ...string) keptFinding {
	return keptFinding{severity: ir.SeverityInfo, code: code, at: at, words: words}
}

// keptDegraded is the info a keyword kept for want of a home draws.
func keptDegraded(at string, words ...string) keptFinding {
	return keptInfo(diag.DegradedConstruct, at, words...)
}

// keptValidationOnly is the info a validation-only keyword of an entry draws at
// the entry.
func keptValidationOnly(words ...string) keptFinding {
	return keptInfo(diag.ValidationOnlyKeyword, "/allOf/{i}", words...)
}

// keptFindingsUnder holds the findings of diags drawn at or under the
// component named to want, as a list: none is drawn twice and none is drawn
// that want does not name. Each pointer is read relative to the component.
func keptFindingsUnder(t *testing.T, diags []ir.Diagnostic, name string, want []keptFinding) {
	t.Helper()
	prefix := "/components/schemas/" + name
	var got []ir.Diagnostic
	for _, d := range diags {
		if underPointer(string(d.Provenance.Pointer), prefix) {
			got = append(got, d)
		}
	}
	line := func(sev ir.Severity, code, at string) string { return fmt.Sprintf("%s %s %s", sev, code, at) }
	slices.SortFunc(got, func(a, b ir.Diagnostic) int {
		return strings.Compare(
			line(a.Severity, a.Code, string(a.Provenance.Pointer)), line(b.Severity, b.Code, string(b.Provenance.Pointer)))
	})
	gotLines := make([]string, 0, len(got))
	for _, d := range got {
		gotLines = append(gotLines, line(d.Severity, d.Code, strings.TrimPrefix(string(d.Provenance.Pointer), prefix)))
	}
	matched := make([]bool, len(got))
	for _, w := range want {
		found := false
		for i, d := range got {
			rel := strings.TrimPrefix(string(d.Provenance.Pointer), prefix)
			if matched[i] || d.Severity != w.severity || d.Code != w.code || !slices.Contains(strings.Split(w.at, "|"), rel) {
				continue
			}
			matched[i], found = true, true
			for _, word := range w.words {
				assert.Contains(t, d.Message, word, "%s: the message at %s", name, rel)
			}
			break
		}
		require.True(t, found, "%s: the finding %s is drawn; the document draws %v",
			name, line(w.severity, w.code, w.at), gotLines)
	}
	for i := range got {
		assert.True(t, matched[i], "%s: the finding %s is not one the schema should draw", name, gotLines[i])
	}
}

// keptKeywordCase is one keyword written on an entry of an allOf beside a
// principal, with what it must be kept as. The same keyword is written
// beside the allOf too, which a twin case holds to the same keys and reasons.
type keptKeywordCase struct {
	name string
	// principal is the first conjunct, as YAML; the keyword is written on an
	// entry beside it, and beside the allOf in the twin.
	principal string
	// keyword is the keywords of the entry, as the body of a flow mapping.
	keyword string
	// versions the case runs at, both of keptKeywordVersions where empty.
	versions []string
	// wrap is the schema holding the allOf, with %s for it, where the allOf
	// is not the component; pos is the pointer from the component to the allOf.
	wrap, pos string
	// extra are components the case needs beside the common ones.
	extra []string
	// want is each key kept with its reason, "{i}" standing for the entry's
	// index in the allOf; an empty reason accepts any.
	want map[string]ir.UnmodeledReason
	// whole marks a keyword whose entry is kept whole, so the key's provenance
	// is the entry's own pointer.
	whole bool
	// finds is the findings the schema draws, "{i}" as in want.
	finds []keptFinding
	// twin holds the beside spelling to the same keys and reasons.
	twin bool
	// check holds what else the lowering must say of the schema named.
	check func(t *testing.T, doc *ir.Document, name string)
}

// keptKeywordCommon are the components every case reads, whatever it writes.
var keptKeywordCommon = []string{
	"Str: {type: string}",
	"Num: {type: number}",
	"Obj: {type: object, properties: {a: {type: string}}}",
	"Arr: {type: array, items: {type: string}}",
}

// keptIndexed returns s with the entry's index written in.
func keptIndexed(s string, i int) string { return strings.ReplaceAll(s, "{i}", strconv.Itoa(i)) }

// spellings returns the three schemas of the case: E writes the keyword on the
// entry after the principal, S on the entry before it, and B beside the allOf.
func (c keptKeywordCase) spellings() (schemas []string) {
	wrap := c.wrap
	if wrap == "" {
		wrap = "%s"
	}
	forms := map[string]string{
		"E": "{allOf: [" + c.principal + ", {" + c.keyword + "}]}",
		"S": "{allOf: [{" + c.keyword + "}, " + c.principal + "]}",
		"B": "{allOf: [" + c.principal + "], " + c.keyword + "}",
	}
	for _, name := range []string{"E", "S", "B"} {
		schemas = append(schemas, name+": "+fmt.Sprintf(wrap, forms[name]))
	}
	return slices.Concat(keptKeywordCommon, c.extra, schemas)
}

// runKept compiles the case at each version, in both orders of the
// components, and holds the entry after the principal and the entry before it
// to the same keys, reasons, findings and provenance.
func runKept(t *testing.T, c keptKeywordCase) {
	t.Helper()
	versions := c.versions
	if len(versions) == 0 {
		versions = keptKeywordVersions
	}
	for _, version := range versions {
		t.Run(c.name+"/"+version, func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, c.spellings(), func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "kept", admissionSpec(version, "", schemas...))
				for _, spelling := range []struct {
					name  string
					index int
				}{{"E", 1}, {"S", 0}} {
					entry := "/components/schemas/" + spelling.name + c.pos + "/allOf/" + strconv.Itoa(spelling.index)
					want := map[string]ir.UnmodeledReason{}
					for key, reason := range c.want {
						want[keptIndexed(key, spelling.index)] = reason
					}
					finds := make([]keptFinding, 0, len(c.finds))
					for _, f := range c.finds {
						f.at = keptIndexed(c.pos+f.at, spelling.index)
						finds = append(finds, f)
					}
					got := keptKeywordsOf(doc, spelling.name)
					requireKeptKeywords(t, spelling.name, got, want, entry, c.whole)
					keptFindingsUnder(t, diags, spelling.name, finds)
					if c.check != nil {
						c.check(t, doc, spelling.name)
					}
				}
				if c.twin {
					assert.Equal(t, keptKeywordReasons(keptKeywordsOf(doc, "E")), keptKeywordReasons(keptKeywordsOf(doc, "B")),
						"the keyword kept beside the allOf and on an entry has one key and one reason")
				}
			})
		})
	}
}

// runKeptCases runs each case as a subtest.
func runKeptCases(t *testing.T, cases []keptKeywordCase) {
	t.Helper()
	for _, c := range cases {
		runKept(t, c)
	}
}

// runKeptNullableCases compiles a schema writing a nullable no policy applies,
// on its own and on an entry beside a use of a string, with the
// nullable-inference policy on and off, and hands each document to check.
func runKeptNullableCases(t *testing.T, check func(t *testing.T, doc *ir.Document)) {
	t.Helper()
	values := map[string]string{"string": "'true'", "number": "1", "list": "[true]", "word": "'yes'"}
	for _, off := range []bool{false, true} {
		for label, value := range values {
			t.Run(fmt.Sprintf("%s/off=%t", label, off), func(t *testing.T) {
				t.Parallel()
				schemas := append(slices.Clone(keptKeywordCommon),
					"Own: {type: string, nullable: "+value+"}",
					"Entry: {allOf: ["+keptStr+", {nullable: "+value+"}]}")
				opts := compilers.Options{FormatOptions: openapi.Options{
					NullableInference: openapi.NullableInference{Disabled: off},
				}}
				doc, diags, err := openapi.New().Compile(t.Context(),
					[]compilers.Source{{Path: "nullable.yaml", Data: []byte(admissionSpec("3.1.0", "", schemas...))}}, opts)
				require.NoError(t, err)
				requireVerifies(t, doc, diags)
				check(t, doc)
			})
		}
	}
}
