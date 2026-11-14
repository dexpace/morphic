// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

const (
	labelBlank      = "ir/version-label-blank"
	labelRepeated   = "ir/version-label-repeated"
	labelUndeclared = "ir/version-label-undeclared"
	outOfOrder      = "ir/availability-out-of-order"

	modelAvailability = "doc.Types[t/x/M].Availability"
)

// versionedModel returns scenarioBase declaring versions v1 to v3, with the
// availability a case sets on its model.
func versionedModel(avail *ir.Availability) *ir.Document {
	doc := scenarioBase()
	doc.Versions = []string{"v1", "v2", "v3"}
	doc.Types["t/x/M"].Common().Availability = avail
	return doc
}

// TestVerify_VersionListIsHeldToItsLabels pins the two rules of the list
// itself: a blank entry names no version, and an entry repeating an earlier one
// is reported at the later index, naming the earlier. A first entry is read like
// any other.
func TestVerify_VersionListIsHeldToItsLabels(t *testing.T) {
	t.Parallel()
	doc := scenarioBase()
	doc.Versions = []string{"v1", "v1", ""}
	assert.Equal(t, []string{"doc.Versions[1]"}, scenarioCodes(doc, labelRepeated))
	assert.Equal(t, []string{"doc.Versions[2]"}, scenarioCodes(doc, labelBlank))

	first := scenarioBase()
	first.Versions = []string{"", "v1"}
	assert.Equal(t, []string{"doc.Versions[0]"}, scenarioCodes(first, labelBlank), "the first entry is read too")

	messages := map[string]string{}
	for _, v := range verifyDeclared(doc) {
		messages[v.Code] = v.Message
	}
	assert.Equal(t, `version label "v1" is listed here and at index 0`, messages[labelRepeated])
	assert.Equal(t, "version label is blank, so no order places it", messages[labelBlank])

	valid := scenarioBase()
	valid.Versions = []string{"v1", "v2"}
	assert.Empty(t, scenarioCodes(valid, labelRepeated))
	assert.Empty(t, scenarioCodes(valid, labelBlank))
}

// TestVerify_AvailabilityLabelsAreDeclared pins that every label an
// Availability writes is one Document.Versions declares: Added and Removed by
// entry, the Deprecated label when it is non-empty, and the Version of each
// rename, type change and optionality change, a blank one included.
func TestVerify_AvailabilityLabelsAreDeclared(t *testing.T) {
	t.Parallel()
	doc := versionedModel(&ir.Availability{
		Added:           []string{"v9"},
		Deprecated:      "v7",
		RenamedFrom:     []ir.VersionedName{{Version: "", Name: "b"}},
		TypeChangedFrom: []ir.VersionedType{{Version: "v8", Type: ir.TypeRef{Target: "t/prim/string"}}},
		RequiredChanged: []ir.VersionedBool{{Version: "v6"}},
	})
	assert.ElementsMatch(t, []string{
		modelAvailability + ".Added[0]",
		modelAvailability + ".Deprecated",
		modelAvailability + ".RenamedFrom[0].Version",
		modelAvailability + ".TypeChangedFrom[0].Version",
		modelAvailability + ".RequiredChanged[0].Version",
	}, scenarioCodes(doc, labelUndeclared))

	removed := versionedModel(&ir.Availability{Removed: []string{"v5"}})
	assert.Equal(t, []string{modelAvailability + ".Removed[0]"}, scenarioCodes(removed, labelUndeclared))

	valid := versionedModel(&ir.Availability{
		Added:           []string{"v1"},
		Removed:         []string{"v2"},
		Deprecated:      "",
		RenamedFrom:     []ir.VersionedName{{Version: "v1", Name: "b"}},
		TypeChangedFrom: []ir.VersionedType{{Version: "v2", Type: ir.TypeRef{Target: "t/prim/string"}}},
		RequiredChanged: []ir.VersionedBool{{Version: "v3"}},
	})
	assert.Empty(t, scenarioCodes(valid, labelUndeclared), "every label declared, Deprecated empty")

	deprecated := versionedModel(&ir.Availability{Deprecated: "v3"})
	assert.Empty(t, scenarioCodes(deprecated, labelUndeclared), "a declared Deprecated label")
}

// TestVerify_AvailabilityLabelMessage pins the wording of the undeclared label.
func TestVerify_AvailabilityLabelMessage(t *testing.T) {
	t.Parallel()
	doc := versionedModel(&ir.Availability{Added: []string{"v9"}})
	var got []string
	for _, v := range verifyDeclared(doc) {
		if v.Code == labelUndeclared {
			got = append(got, v.Message)
		}
	}
	assert.Equal(t, []string{`version label "v9" is not one Document.Versions declares`}, got)
}

// TestVerify_AddedAndRemovedAlternateInVersionOrder pins the ordering rule: with
// every label declared, Added and Removed each ascend strictly in Versions order
// and, merged, alternate. Either list may come first, so a Removed first says
// the entity is there from the first version.
func TestVerify_AddedAndRemovedAlternateInVersionOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		avail ir.Availability
		want  []string // empty: no violation
	}{
		{"added out of order", ir.Availability{Added: []string{"v2", "v1"}}, []string{modelAvailability + ".Added[1]"}},
		{"removed at the version it was added", ir.Availability{Added: []string{"v1"}, Removed: []string{"v1"}},
			[]string{modelAvailability + ".Removed[0]"}},
		{"two adds in a row", ir.Availability{Added: []string{"v1", "v2"}}, []string{modelAvailability + ".Added[1]"}},
		{"removed repeating itself", ir.Availability{Removed: []string{"v1", "v1"}},
			[]string{modelAvailability + ".Removed[1]"}},
		{"added and removed alternating", ir.Availability{Added: []string{"v1", "v3"}, Removed: []string{"v2"}}, nil},
		{"removed first, then added", ir.Availability{Added: []string{"v3"}, Removed: []string{"v2"}}, nil},
		{"removed alone", ir.Availability{Removed: []string{"v2"}}, nil},
		{"added alone", ir.Availability{Added: []string{"v2"}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := versionedModel(&tc.avail)
			assert.Equal(t, tc.want, scenarioCodes(doc, outOfOrder))
			assert.Empty(t, scenarioCodes(doc, labelUndeclared), "every label is declared")
		})
	}
}

// TestVerify_OrderIsReadOnlyWhereEveryLabelIsDeclared pins that an undeclared
// label is reported alone: a list that places no label in Versions has no order
// to be out of.
func TestVerify_OrderIsReadOnlyWhereEveryLabelIsDeclared(t *testing.T) {
	t.Parallel()
	doc := versionedModel(&ir.Availability{Added: []string{"v9", "v1"}})
	assert.NotEmpty(t, scenarioCodes(doc, labelUndeclared))
	assert.Empty(t, scenarioCodes(doc, outOfOrder))
}

// TestVerify_EveryAvailabilityCarrierIsHeldToTheLabels writes an undeclared
// label on each node that carries an Availability, so the check reads the
// struct and not one of the nodes embedding it.
func TestVerify_EveryAvailabilityCarrierIsHeldToTheLabels(t *testing.T) {
	t.Parallel()
	avail := func() *ir.Availability { return &ir.Availability{Added: []string{"v9"}} }
	for _, tc := range []struct {
		name  string
		place func(doc *ir.Document)
		path  string
	}{
		{"type", func(d *ir.Document) { d.Types["t/x/M"].Common().Availability = avail() },
			"doc.Types[t/x/M].Availability.Added[0]"},
		{"property", func(d *ir.Document) { d.Types["t/x/M"].(*ir.Model).Properties[0].Availability = avail() },
			"doc.Types[t/x/M].Properties[0].Availability.Added[0]"},
		{"operation", func(d *ir.Document) { scenarioOperation(d).Availability = avail() },
			scenarioOp + ".Availability.Added[0]"},
		{"parameter", func(d *ir.Document) { scenarioOperation(d).Params[0].Availability = avail() },
			scenarioOp + ".Params[0].Availability.Added[0]"},
		{"enum member", func(d *ir.Document) { d.Types["t/x/E"].(*ir.Enum).Members[0].Availability = avail() },
			"doc.Types[t/x/E].Members[0].Availability.Added[0]"},
		{"variant", func(d *ir.Document) {
			d.Types["t/x/U"] = &ir.Union{ID: "t/x/U", Name: named("u"), Variants: []ir.Variant{{
				Name: named("a"), Type: ir.TypeRef{Target: "t/prim/string"}, Availability: avail(),
			}}}
		}, "doc.Types[t/x/U].Variants[0].Availability.Added[0]"},
		{"group", func(d *ir.Document) { d.Services[0].Groups[0].Availability = avail() },
			"doc.Services[0].Groups[0].Availability.Added[0]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := scenarioBase()
			doc.Versions = []string{"v1", "v2", "v3"}
			tc.place(doc)
			assert.Equal(t, []string{tc.path}, scenarioCodes(doc, labelUndeclared))
		})
	}
}
