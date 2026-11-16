// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapi

import (
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering/loweringtest"
	"github.com/dexpace/morphic/compilers/openapi/internal/whole"
	"github.com/dexpace/morphic/ir"
)

// TestRun_ReadsTheFinishedDocumentOnce holds every step reading the finished
// document to one whole.Document a run: a document whose link and
// paginations those steps read is read through one, built once.
func TestRun_ReadsTheFinishedDocumentOnce(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("../../testdata/conformance/openapi/body-alias.yaml")
	require.NoError(t, err)
	built := 0
	count := func(doc *ir.Document) *whole.Document {
		built++
		return whole.New(doc)
	}

	doc, diags, err := run(t.Context(), loweringtest.Context(t, string(src), lowering.Policy{}), inferences{},
		compile.NewTypes(), count)
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Empty(t, diags)
	assert.Equal(t, 1, built, "one reading of the finished document a run")
}

// TestRun_AppendsNothingAfterAFirstAsk holds every step reading the finished
// document to the rule whole.Document keeps its indexes by: none appends a
// property, parameter, binding or header after a first ask. What the run's
// one reading answers, once the run returns, is compared with a fresh reading
// of the document it returned, over the cases each such step reads.
func TestRun_AppendsNothingAfterAFirstAsk(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"body-alias", "response-links", "extension-pagination", "extension-pagination-root",
		"pagination-inferred", "pagination-flag", "pagination-body", "pagination-links", "param-choices",
		"stream-terminator", "http-date-inference", "replacement-inference", "idempotency-inference",
		"secret-inference",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src, err := os.ReadFile("../../testdata/conformance/openapi/" + name + ".yaml")
			require.NoError(t, err)
			var kept *whole.Document
			keep := func(doc *ir.Document) *whole.Document {
				kept = whole.New(doc)
				return kept
			}

			doc, _, err := run(t.Context(), loweringtest.Context(t, string(src), lowering.Policy{}), inferences{},
				compile.NewTypes(), keep)
			require.NoError(t, err)
			require.NotNil(t, kept)
			assert.Empty(t, staleReadings(kept, whole.New(doc)))
		})
	}
}

// staleReadings names each answer kept gives, of a parameter's placements, a
// response's headers or a model's members, that fresh, a reading of the same
// document made afresh, gives otherwise.
func staleReadings(kept, fresh *whole.Document) []string {
	var stale []string
	for _, op := range fresh.Ops {
		for _, p := range op.Params {
			if !slices.Equal(kept.Params(op).Placements(p.ID), fresh.Params(op).Placements(p.ID)) {
				stale = append(stale, "placements of "+string(p.ID))
			}
		}
		for key, headers := range responseHeaders(op) {
			for _, h := range headers {
				name := h.EffectiveWireName()
				if !slices.Equal(kept.Header(key, headers, name), fresh.Header(key, headers, name)) {
					stale = append(stale, "header "+name+" of "+string(op.ID))
				}
			}
		}
	}
	for id := range fresh.Types {
		if !slices.Equal(propIDs(kept.Members(id)), propIDs(fresh.Members(id))) {
			stale = append(stale, "members of "+string(id))
		}
	}
	return stale
}

// responseHeaders returns the headers of each response op holds, by the key a
// step reads them under.
func responseHeaders(op *ir.Operation) map[whole.ResponseKey][]ir.Header {
	out := map[whole.ResponseKey][]ir.Header{}
	for i := range op.Responses {
		out[whole.ResponseKey{Op: op.ID, List: whole.ListResponses, Index: i}] = op.Responses[i].Headers
	}
	for i := range op.Errors {
		out[whole.ResponseKey{Op: op.ID, List: whole.ListErrors, Index: i}] = op.Errors[i].Headers
	}
	if op.Default != nil {
		out[whole.ResponseKey{Op: op.ID, List: whole.ListDefault}] = op.Default.Headers
	}
	return out
}

// propIDs returns the IDs of props, in order.
func propIDs(props []*ir.Property) []ir.PropID {
	ids := make([]ir.PropID, len(props))
	for i, p := range props {
		ids[i] = p.ID
	}
	return ids
}
