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
	"github.com/dexpace/morphic/compilers/openapi/internal/compileddoc"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/compilers/openapi/internal/service/servicetest"
	"github.com/dexpace/morphic/ir"
)

// TestRun_ReadsTheFinishedDocumentOnce holds every step reading the finished
// document to one compileddoc.CompiledDocumentView a run: a document whose link
// and paginations those steps read is read through one, built once.
func TestRun_ReadsTheFinishedDocumentOnce(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("../../testdata/conformance/openapi/body-alias.yaml")
	require.NoError(t, err)
	built := 0
	count := func(doc *ir.Document) *compileddoc.CompiledDocumentView {
		built++
		return compileddoc.New(doc)
	}

	doc, diags, err := run(t.Context(), servicetest.Context(t, string(src), service.Policy{}), inferences{},
		compile.NewTypes(), count)
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Empty(t, diags)
	assert.Equal(t, 1, built, "one reading of the finished document a run")
}

// TestRun_AppendsNothingAfterAFirstAsk holds every step reading the finished
// document to the rule compileddoc.CompiledDocumentView keeps its indexes by:
// none appends a property, parameter, binding or header after a first ask. What
// the run's one reading answers, once the run returns, is compared with a fresh
// reading of the document it returned, over the cases each such step reads.
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
			var kept *compileddoc.CompiledDocumentView
			keep := func(doc *ir.Document) *compileddoc.CompiledDocumentView {
				kept = compileddoc.New(doc)
				return kept
			}

			doc, _, err := run(t.Context(), servicetest.Context(t, string(src), service.Policy{}), inferences{},
				compile.NewTypes(), keep)
			require.NoError(t, err)
			require.NotNil(t, kept)
			assert.Empty(t, staleReadings(kept, compileddoc.New(doc)))
		})
	}
}

// staleReadings names each answer kept gives, of a parameter's placements, a
// response's headers or a model's members, that fresh, a reading of the same
// document made afresh, gives otherwise.
func staleReadings(kept, fresh *compileddoc.CompiledDocumentView) []string {
	var stale []string
	for _, operation := range fresh.Operations {
		for _, parameter := range operation.Params {
			if !slices.Equal(kept.Params(operation).Placements(parameter.ID), fresh.Params(operation).Placements(parameter.ID)) {
				stale = append(stale, "placements of "+string(parameter.ID))
			}
		}
		for key, headers := range responseHeaders(operation) {
			for _, header := range headers {
				name := header.EffectiveWireName()
				if !slices.Equal(kept.Header(key, headers, name), fresh.Header(key, headers, name)) {
					stale = append(stale, "header "+name+" of "+string(operation.ID))
				}
			}
		}
	}
	for id := range fresh.Types {
		if !slices.Equal(propertyIDs(kept.Members(id)), propertyIDs(fresh.Members(id))) {
			stale = append(stale, "members of "+string(id))
		}
	}
	return stale
}

// responseHeaders returns the headers of each response operation holds, by the
// key a step reads them under.
func responseHeaders(operation *ir.Operation) map[compileddoc.ResponseKey][]ir.Header {
	out := map[compileddoc.ResponseKey][]ir.Header{}
	for i := range operation.Responses {
		out[compileddoc.ResponseKey{Operation: operation.ID, List: compileddoc.ListResponses, Index: i}] = operation.Responses[i].Headers
	}
	for i := range operation.Errors {
		out[compileddoc.ResponseKey{Operation: operation.ID, List: compileddoc.ListErrors, Index: i}] = operation.Errors[i].Headers
	}
	if operation.Default != nil {
		out[compileddoc.ResponseKey{Operation: operation.ID, List: compileddoc.ListDefault}] = operation.Default.Headers
	}
	return out
}

// propertyIDs returns the IDs of properties, in order.
func propertyIDs(properties []*ir.Property) []ir.PropertyID {
	ids := make([]ir.PropertyID, len(properties))
	for i, property := range properties {
		ids[i] = property.ID
	}
	return ids
}
