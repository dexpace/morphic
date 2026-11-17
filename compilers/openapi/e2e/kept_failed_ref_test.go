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
