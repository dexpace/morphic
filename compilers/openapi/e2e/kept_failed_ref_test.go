// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// keptFailedRefObject is an entry beside a failed $ref that declares a member.
const keptFailedRefObject = "{type: object, properties: {b: {type: string}}}"

// keptBesideARef is the warnings a 3.0 document draws for each keyword written
// beside a $ref at the pointer given, which 3.0 ignores and 3.1 applies.
func keptBesideARef(version, entry string, keywords ...string) []keptFinding {
	if version != "3.0.3" {
		return nil
	}
	var out []keptFinding
	for _, keyword := range keywords {
		out = append(out, keptFinding{
			severity: ir.SeverityWarning, code: diag.LaterVersionConstruct, at: entry + "/" + keyword,
		})
	}
	return out
}

func TestKeptFailedRef_TheFailureIsReportedOnceWhereTheRefIsWritten(t *testing.T) {
	t.Parallel()
	for _, version := range keptKeywordVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			schemas := []string{
				"Y: {allOf: [{$ref: '#/components/schemas/Ghost', minLength: 2}, " + keptFailedRefObject + "]}",
				"Z: {allOf: [{$ref: '#/components/schemas/Ghost', x-a: 1}, " + keptFailedRefObject + "]}",
			}
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				_, diags := compileAnnotationSpec(t, "failed", admissionSpec(version, "", schemas...))
				var errs []ir.Diagnostic
				for _, d := range diags {
					if d.Severity == ir.SeverityError {
						errs = append(errs, d)
					}
				}
				require.Len(t, errs, 2, "one error for each failed $ref: %v", errs)
				for _, d := range errs {
					require.Equal(t, diag.UnresolvedRef, d.Code)
				}
			})
		})
	}
}

func TestKeptFailedRef_AUnionBranchWithAFailedRefIsKeptWhole(t *testing.T) {
	t.Parallel()
	for _, version := range keptKeywordVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			schemas := []string{
				"YUnion: {type: object, properties: {b: {type: string}}, " +
					"oneOf: [{$ref: '#/components/schemas/Ghost', description: why}]}",
			}
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, _ := compileAnnotationSpec(t, "union", admissionSpec(version, "", schemas...))
				requireKeptKeywords(t, "YUnion", keptKeywordsOf(doc, "YUnion"), degradedKept("openapi:oneOf"),
					"/components/schemas/YUnion/oneOf", false)
			})
		})
	}
}

func TestKeptFailedRef_AResolvedRefKeepsWhatIsWrittenBesideIt(t *testing.T) {
	t.Parallel()
	for _, version := range keptKeywordVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			schemas := []string{
				"Ok: {type: object}",
				"YOk: {allOf: [{$ref: '#/components/schemas/Ok', description: why, properties: {a: {type: string}}, minLength: 2}]}",
			}
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "resolved", admissionSpec(version, "", schemas...))
				// What the entry writes beside its $ref is a conjunct of the
				// composition: the member merges, the note is the model's, and
				// only the bound a model cannot hold is kept.
				requireKeptKeywords(t, "YOk", keptKeywordsOf(doc, "YOk"),
					degradedKept("openapi:minLength"), "/components/schemas/YOk/allOf/0", false)
				yok := requireModelNode(t, doc, componentID("YOk"))
				assert.Equal(t, []string{"a"}, propertyNames(yok), "the member beside the $ref merges")
				assert.Equal(t, "why", yok.Docs.Description)
				keptFindingsUnder(t, diags, "YOk", append([]keptFinding{
					keptDegraded("/allOf/0", "minLength"),
				}, keptBesideARef(version, "/allOf/0", "description", "properties", "minLength")...))
			})
		})
	}
}

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

func TestKeptFailedRef_ARefOnALoopIsKeptLikeOneThatNamesNothing(t *testing.T) {
	t.Parallel()
	for _, version := range keptKeywordVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			// Self's first entry names itself: the chain of $refs it starts leads
			// back to it, and lands on no schema. Into's entry names a schema that
			// is on a loop, which is itself declared and so resolves.
			schemas := []string{
				"LA: {$ref: '#/components/schemas/LB'}",
				"LB: {$ref: '#/components/schemas/LA'}",
				"Self: {allOf: [{$ref: '#/components/schemas/Self/allOf/0', minLength: 2}, " + keptFailedRefObject + "]}",
				"Into: {allOf: [{$ref: '#/components/schemas/LA', minLength: 2}, " + keptFailedRefObject + "]}",
			}
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "loop", admissionSpec(version, "", schemas...))
				got := keptKeywordsOf(doc, "Self")
				require.Equal(t, degradedKept("openapi:$ref", "openapi:minLength"), keptKeywordReasons(got), "Self")
				assert.Equal(t, `"#/components/schemas/Self/allOf/0"`, got["openapi:$ref"][0].value)
				assert.Empty(t, keptKeywordsOf(doc, "Into"), "Into: a $ref to a declared schema is lowered, not kept")
				assert.NotNil(t, requireModelOf(t, doc, "Into").Base, "Into: the reference is its base")
				var cycles int
				for _, d := range diags {
					if d.Severity == ir.SeverityError {
						require.Equal(t, diag.CyclicRef, d.Code, "a loop is reported by the cycle's own finding")
						cycles++
					}
				}
				assert.Equal(t, 2, cycles, "each loop is reported once")
			})
		})
	}
}
