// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

//go:build pending

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// keptUnresolved is the error a $ref naming nothing draws where it is written.
func keptUnresolved(at, target string) keptFinding {
	return keptFinding{severity: ir.SeverityError, code: diag.UnresolvedRef, at: at, words: []string{target}}
}

// requireKeptFailedRef holds the schema named to keep, under the entry at
// entry, the $ref that failed and each keyword written beside it, once each,
// and to draw the error of the failure and one info naming the keywords the
// $ref's neighbours were kept as.
func requireKeptFailedRef(t *testing.T, doc *ir.Document, diags []ir.Diagnostic, version, name, entry, target string) {
	t.Helper()
	at := "/components/schemas/" + name + entry
	got := keptKeywordsUnder(doc, at)
	kept := got["openapi:$ref"]
	require.Len(t, kept, 1, "%s: the $ref that failed is kept once", name)
	assert.Equal(t, `"`+target+`"`, kept[0].value, "%s: the reference as written", name)
	assert.Equal(t, ir.ReasonDegradedLowering, kept[0].reason, name)
	for key, want := range map[string]string{"openapi:properties": `{"a":{"type":"string"}}`, "openapi:minLength": "2"} {
		entries := got[key]
		require.Len(t, entries, 1, "%s: %s is kept once", name, key)
		assert.Equal(t, ir.ReasonDegradedLowering, entries[0].reason, "%s: %s", name, key)
		assert.Equal(t, want, entries[0].value, "%s: %s", name, key)
		assert.True(t, underPointer(entries[0].pointer, at), "%s: %s is written in the conjunct", name, key)
	}
	var described bool
	if entries, ok := got["openapi:description"]; ok {
		require.Len(t, entries, 1, "%s: the description is kept once", name)
		described = true
	}
	described = described || kindNode(t, doc, componentID(name)).Common().Docs.Description == "why"
	assert.True(t, described, "%s: the description beside the failed $ref reaches the document", name)
	for key := range got {
		assert.Contains(t, []string{"openapi:$ref", "openapi:properties", "openapi:minLength", "openapi:description"}, key,
			"%s: nothing else is kept", name)
	}
	want := append([]keptFinding{
		keptUnresolved(entry, target),
		keptDegraded(entry, "properties", "minLength"),
	}, keptBesideARef(version, entry, "description", "properties", "minLength")...)
	keptFindingsUnder(t, diags, name, want)
}

func TestKeptFailedRef_EveryKeyBesideAFailedRefIsKeptAndTheFailureReportedOnce(t *testing.T) {
	t.Parallel()
	const ghost = "{$ref: '#/components/schemas/Ghost', description: why, properties: {a: {type: string}}, minLength: 2}"
	for _, version := range keptKeywordVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			schemas := []string{
				"Y: {allOf: [" + ghost + ", " + keptFailedRefObject + "]}",
				"YRev: {allOf: [" + keptFailedRefObject + ", " + ghost + "]}",
				"YAlone: {allOf: [" + ghost + "]}",
			}
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "failed", admissionSpec(version, "", schemas...))
				const target = "#/components/schemas/Ghost"
				requireKeptFailedRef(t, doc, diags, version, "Y", "/allOf/0", target)
				requireKeptFailedRef(t, doc, diags, version, "YRev", "/allOf/1", target)
				requireKeptFailedRef(t, doc, diags, version, "YAlone", "/allOf/0", target)
				for _, name := range []string{"Y", "YRev"} {
					_, ok := propByWire(requireModelOf(t, doc, name), "b")
					assert.True(t, ok, "%s: the member the other conjunct declares is typed", name)
				}
			})
		})
	}
}

func TestKeptFailedRef_SeveralFailedRefsAreKeptByAscendingReferenceText(t *testing.T) {
	t.Parallel()
	for _, version := range keptKeywordVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			schemas := []string{
				"First: {allOf: [{$ref: '#/components/schemas/Ghost'}, {$ref: '#/components/schemas/Ghost2'}, " + keptFailedRefObject + "]}",
				"Second: {allOf: [{$ref: '#/components/schemas/Ghost2'}, {$ref: '#/components/schemas/Ghost'}, " + keptFailedRefObject + "]}",
			}
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "ghosts", admissionSpec(version, "", schemas...))
				for _, name := range []string{"First", "Second"} {
					got := keptKeywordsOf(doc, name)
					require.Equal(t, degradedKept("openapi:$ref/0", "openapi:$ref/1"), keptKeywordReasons(got), name)
					assert.Equal(t, `"#/components/schemas/Ghost"`, got["openapi:$ref/0"][0].value, name)
					assert.Equal(t, `"#/components/schemas/Ghost2"`, got["openapi:$ref/1"][0].value, name)
				}
				keptFindingsUnder(t, diags, "First", []keptFinding{
					keptUnresolved("/allOf/0", "Ghost"), keptUnresolved("/allOf/1", "Ghost2"),
				})
				keptFindingsUnder(t, diags, "Second", []keptFinding{
					keptUnresolved("/allOf/0", "Ghost2"), keptUnresolved("/allOf/1", "Ghost"),
				})
			})
		})
	}
}

func TestKeptFailedRef_ARefInsideAnEntryOrAtAnItemIsKept(t *testing.T) {
	t.Parallel()
	const ghost = "{$ref: '#/components/schemas/Ghost', minLength: 2}"
	for _, version := range keptKeywordVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			schemas := []string{
				"Nest: {allOf: [{allOf: [" + ghost + "]}, " + keptFailedRefObject + "]}",
				"Items: {type: array, items: {allOf: [" + ghost + "]}}",
			}
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "nested", admissionSpec(version, "", schemas...))
				for name, entry := range map[string]string{"Nest": "/allOf/0/allOf/0", "Items": "/items/allOf/0"} {
					got := keptKeywordsUnder(doc, "/components/schemas/"+name+entry)
					require.Equal(t, degradedKept("openapi:$ref", "openapi:minLength"), keptKeywordReasons(got), name)
					keptFindingsUnder(t, diags, name, append([]keptFinding{
						keptUnresolved(entry, "Ghost"), keptDegraded(entry, "minLength"),
					}, keptBesideARef(version, entry, "minLength")...))
				}
			})
		})
	}
}

func TestKeptFailedRef_ARefThatLoopsIsKeptLikeOneThatNamesNothing(t *testing.T) {
	t.Parallel()
	for _, version := range keptKeywordVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			schemas := []string{
				"LA: {$ref: '#/components/schemas/LB'}",
				"LB: {$ref: '#/components/schemas/LA'}",
				"Loop: {allOf: [{$ref: '#/components/schemas/LA', minLength: 2}, " + keptFailedRefObject + "]}",
			}
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "loop", admissionSpec(version, "", schemas...))
				got := keptKeywordsOf(doc, "Loop")
				require.Equal(t, degradedKept("openapi:$ref", "openapi:minLength"), keptKeywordReasons(got), "Loop")
				assert.Equal(t, `"#/components/schemas/LA"`, got["openapi:$ref"][0].value)
				var cycles int
				for _, d := range diags {
					if d.Severity == ir.SeverityError {
						require.Equal(t, diag.CyclicRef, d.Code, "the loop is reported by the cycle's own finding")
						cycles++
					}
				}
				assert.Equal(t, 1, cycles, "the loop is reported once")
			})
		})
	}
}
