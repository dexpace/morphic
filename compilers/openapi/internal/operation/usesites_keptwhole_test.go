// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestPromotion_ReadsAKeyInABranchKeptWhole holds a promotion to the keys an
// allOf entry writes where its carrier keeps the entry whole, for a closure
// no use of its string types: a parameter, a
// content parameter, a header and a property, the $ref first and the entry
// first, are each redacted and deprecated, and keep the entry beside each key
// a promotion reads from it, and a key no promotion reads only inside it. A cursor and a next cursor marked the same way page their
// operation.
func TestPromotion_ReadsAKeyInABranchKeptWhole(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, specFrom(t, specsDir, "promoted_keys_in_a_branch_kept_whole"))
	for _, diagnostic := range diags {
		assert.Equal(t, "openapi/degraded-construct info", diagnostic.Code+" "+string(diagnostic.Severity), diagnostic.Message)
	}
	facts := operationTree(t, doc, "facts")
	params, headers := valueAtPath(facts, "params"), valueAtPath(facts, "responses", "0", "headers")
	body := tree(t, doc.Types[ir.TypeID("type/anonymous/paths/~1facts/get/responses/200/content/application~1json/schema")])
	properties := valueAtPath(body, "properties")
	for _, c := range []struct {
		name, scope, entry string
		carrier            any
	}{
		{"ref_first", "schema/", "allOf/1", namedItem(t, params, "ref_first")},
		{"branch_first", "schema/", "allOf/0", namedItem(t, params, "branch_first")},
		{"content_ref_first", "content/application~1json/schema/", "allOf/1", namedItem(t, params, "content_ref_first")},
		{"content_branch_first", "content/application~1json/schema/", "allOf/0",
			namedItem(t, params, "content_branch_first")},
		{"H-Ref-First", "schema/", "allOf/1", namedItem(t, headers, "H-Ref-First")},
		{"H-Branch-First", "schema/", "allOf/0", namedItem(t, headers, "H-Branch-First")},
		{"property ref_first", "", "allOf/1", namedItem(t, properties, "ref_first")},
		{"property branch_first", "", "allOf/0", namedItem(t, properties, "branch_first")},
	} {
		assert.Equal(t, true, valueAtPath(c.carrier, "secret"), c.name)
		assert.Equal(t, "old", valueAtPath(c.carrier, "deprecation", "message"), c.name)
		held := entries(t, valueAtPath(c.carrier, "unmodeled"))
		kept := make([]string, 0, len(held))
		for key := range held {
			kept = append(kept, key)
		}
		slices.Sort(kept)
		want := []string{
			"openapi:" + c.scope + c.entry, "openapi:" + c.scope + "x-deprecated-reason",
			"openapi:" + c.scope + "x-sensitive", "openapi:" + c.scope + "x-stability",
		}
		assert.Equal(t, want, kept, c.name)
	}

	for _, name := range []string{"pagesRefFirst", "pagesBranchFirst", "pagesContentRefFirst", "pagesContentBranchFirst"} {
		operation := operationTree(t, doc, name)
		if !assert.NotNil(t, valueAtPath(operation, "pagination"), name) {
			continue
		}
		assert.Equal(t, "cursor", valueAtPath(operation, "pagination", "strategy"), name)
		assert.Equal(t, valueAtPath(namedItem(t, valueAtPath(operation, "params"), "cursor"), "id"), valueAtPath(operation, "pagination", "inputCursor", "param"), name)
		assert.NotNil(t, valueAtPath(operation, "pagination", "nextCursor", "segments"), name)
	}
}
