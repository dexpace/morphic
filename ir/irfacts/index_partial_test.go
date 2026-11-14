// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irfacts_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
	"github.com/dexpace/morphic/ir/irtest"
)

// nestedGroupsDoc returns a document with one operation, nested depth groups
// down in a service's groups.
func nestedGroupsDoc(depth int) *ir.Document {
	g := ir.OperationGroup{ID: "g/x/leaf", Operations: []ir.Operation{{ID: "op/x/o"}}}
	for i := range depth {
		g = ir.OperationGroup{ID: ir.GroupID(fmt.Sprintf("g/x/n%d", i)), Groups: []ir.OperationGroup{g}}
	}
	doc := irtest.DocOf(irtest.ModelOf("t/x/M", nil))
	doc.Services = []ir.Service{{ID: "s/x", Groups: []ir.OperationGroup{g}}}
	return doc
}

// TestIndex_PartialSaysTheFactsReadPartOfTheDocument holds the index to telling
// a caller when what it derives from operations, Usage and Correlation, answers
// for part of the document: operations nested past the groups' depth bound are
// not read.
func TestIndex_PartialSaysTheFactsReadPartOfTheDocument(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		depth int
		want  bool
	}{
		{"130 groups nested one in another", 130, true},
		{"3 groups", 3, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, irfacts.New(nestedGroupsDoc(tc.depth)).Partial(), "asked first")

			facts := irfacts.New(nestedGroupsDoc(tc.depth))
			facts.Usage("t/x/M")
			facts.Correlation("op/x/o")
			assert.Equal(t, tc.want, facts.Partial(), "asked after the facts that read operations")
		})
	}
}
