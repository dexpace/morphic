// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// TestVerify_APaginationStopIsOneIRDeclares holds Pagination.Stop to the
// declared conditions, each undeclared one at its own index.
func TestVerify_APaginationStopIsOneIRDeclares(t *testing.T) {
	t.Parallel()
	o := ir.Operation{ID: "op/x/S/op", Pagination: &ir.Pagination{
		Stop: []ir.PageStop{ir.PageStopEmpty, "Empty", ir.PageStopNoNext, ir.PageStopShort, ir.PageStopTotal, "never"},
	}}
	assert.Equal(t, []string{
		"doc.Services[0].Groups[0].Operations[0].Pagination.Stop[1]",
		"doc.Services[0].Groups[0].Operations[0].Pagination.Stop[5]",
	}, violationPaths(irverify.Verify(opDoc(o)), "ir/unknown-page-stop"))
}
