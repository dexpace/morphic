// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestCheckDocRefs_VisitingCostsNoAllocation holds the check's cost at nothing
// on the path every document takes, a Docs with no references, and on one whose
// spans are sound. Every Docs in a document is read, so a copy or a string here
// is paid per node. It is not run in parallel: AllocsPerRun measures the process.
func TestCheckDocRefs_VisitingCostsNoAllocation(t *testing.T) {
	doc := &ir.Document{Types: ir.TypeRegistry{
		"t/plain": &ir.Model{ID: "t/plain", Docs: ir.Docs{Description: "no references"}},
		"t/refs": &ir.Model{ID: "t/refs", Docs: ir.Docs{
			Description: "see Pet",
			Refs:        []ir.DocRef{{StartByte: 4, EndByte: 7, Type: "t/plain"}},
		}},
	}}
	check := checkDocRefs(doc)
	assert.Empty(t, irtest.AllocatingVisits(doc, check))
	assert.Empty(t, check.report(declarations{}), "every span is sound")
}
