// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestCheckInferred_VisitingCostsNoAllocation holds the check's cost for a
// marker in the form ir.JoinInferred writes at nothing, on either carrier and
// with as many names as the compiler writes on one node: it judges the list as
// it reads it, where comparing against the join's rendering costs a string. It
// is not run in parallel: AllocsPerRun measures the process.
func TestCheckInferred_VisitingCostsNoAllocation(t *testing.T) {
	doc := &ir.Document{Types: ir.TypeRegistry{
		"t/pet": &ir.Model{
			ID:         "t/pet",
			Provenance: ir.Provenance{Inferred: "group-path-prefix,streaming-media-type,extension-promotion"},
			Properties: []ir.Property{
				{ID: "p/pet/a", Type: ir.TypeRef{Target: "t/pet", Nullable: true, Inferred: "nullable-widening"}},
				{ID: "p/pet/b", Provenance: ir.Provenance{Inferred: "extension-promotion"}},
			},
		},
	}}
	check := checkInferred(doc)
	assert.Empty(t, irtest.AllocatingVisits(doc, check))
	assert.Empty(t, check.report(declarations{}), "every marker is in the joined form")
}
