// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// TestInteractions_PageStopWithoutFlag holds the stop no_more to the flag it
// reads: a pagination listing it while HasMore is nil is reported at the
// stop's own index, one naming the flag is not, and a flag listed with no stop
// is legal, as TotalCount is with no total stop.
func TestInteractions_PageStopWithoutFlag(t *testing.T) {
	t.Parallel()
	const stop = "doc.Services[0].Groups[0].Operations[0].Pagination.Stop"
	flag := func() *ir.PropPath { return &ir.PropPath{Segments: []ir.PropID{"p/x/Page/properties/has_more"}} }
	for _, tc := range []struct {
		name string
		page ir.Pagination
		want []string
	}{
		{"no_more alone and no flag", ir.Pagination{Stop: []ir.PageStop{ir.PageStopNoMore}}, []string{stop + "[0]"}},
		{"no_more among others and no flag",
			ir.Pagination{Stop: []ir.PageStop{ir.PageStopNoNext, ir.PageStopNoMore}}, []string{stop + "[1]"}},
		{"no_more and its flag", ir.Pagination{HasMore: flag(), Stop: []ir.PageStop{ir.PageStopNoMore}}, nil},
		{"a flag and no stop", ir.Pagination{HasMore: flag()}, nil},
		{"a flag beside another stop", ir.Pagination{HasMore: flag(), Stop: []ir.PageStop{ir.PageStopNoNext}}, nil},
		{"neither", ir.Pagination{Stop: []ir.PageStop{ir.PageStopNoNext}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vs := irverify.Verify(opDoc(ir.Operation{ID: "op/x/S/a", Pagination: &tc.page}))
			assert.Equal(t, tc.want, violationPaths(vs, "ir/page-stop-without-flag"))
			assert.NotContains(t, violationCodes(vs), "ir/unknown-page-stop", "no_more is a declared stop")
		})
	}

	vs := irverify.Verify(opDoc(ir.Operation{ID: "op/x/S/a", Pagination: &ir.Pagination{Stop: []ir.PageStop{ir.PageStopNoMore}}}))
	for _, v := range vs {
		if v.Code == "ir/page-stop-without-flag" {
			assert.Equal(t, `pagination stops on "no_more" and names no flag to read`, v.Message)
			return
		}
	}
	require.Fail(t, "no ir/page-stop-without-flag violation to read the message of")
}
