// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestEntryValueSet_AValueSetTheTargetAlreadySaysStandsNowhere holds the
// control the cases that move a node are read against: an entry whose value
// set is no narrower than the one its $ref target holds, or that names the
// target's own type, is consumed by the target, so the position is a use of
// the target, nothing stands at the entry, nothing is kept and nothing is
// reported, whichever order the entries and the components are written in.
func TestEntryValueSet_AValueSetTheTargetAlreadySaysStandsNowhere(t *testing.T) {
	t.Parallel()
	const ref = "{$ref: '#/components/schemas/Level'}"
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for name, entry := range map[string]string{
			"wider":  "{enum: [low, mid, high]}",
			"equal":  "{enum: [low, mid]}",
			"typed":  "{type: string}",
			"within": "{type: string, enum: [low, mid, high]}",
		} {
			for spelling, schema := range map[string]string{
				"ref first": "{allOf: [" + ref + ", " + entry + "]}",
				"ref last":  "{allOf: [" + entry + ", " + ref + "]}",
			} {
				t.Run(fmt.Sprintf("%s/%s/%s", version, name, spelling), func(t *testing.T) {
					t.Parallel()
					inBothMappingOrders(t, conjunctionSpec(version, schema,
						"Level: {type: string, enum: [low, mid]}"), func(t *testing.T, spec string) {
						doc, diags := compileAnnotationSpec(t, "consumed", spec)
						requireErrorsAndWarnings(t, diags, nil)
						requireNoEntryIndexedType(t, doc)
						for _, at := range conjunctionPositions {
							assert.Equal(t, componentID("Level"), usedAt(t, doc, at), "the use at %s", at)
							assert.Empty(t, entryKeptUnder(doc, at), "nothing is kept at %s", at)
							assert.Empty(t, degradedUnder(diags, at), "nothing is reported at %s", at)
							assert.Equal(t, entryKept(), keptOn(kindNode(t, doc, componentID("Level"))), "the target keeps nothing")
						}
					})
				})
			}
		}
	}
}

// TestEntryValueSet_AUseRepeatedLeavesTheNodesOfTheValueSetAlone holds that a
// value set written on an entry beside a $ref lowers to the same types
// whether one property uses the schema or several positions do, and whether
// the entry comes before the $ref or after it.
func TestEntryValueSet_AUseRepeatedLeavesTheNodesOfTheValueSetAlone(t *testing.T) {
	t.Parallel()
	const ref = "{$ref: '#/components/schemas/T'}"
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for name, schema := range map[string]string{
			"enum after the ref":  "{allOf: [" + ref + ", {enum: [a]}]}",
			"enum before the ref": "{allOf: [{enum: [a]}, " + ref + "]}",
			"enum beside":         "{allOf: [" + ref + "], enum: [a]}",
		} {
			t.Run(version+"/"+name, func(t *testing.T) {
				t.Parallel()
				requireRepeatedUsesLeaveNodesAlone(t, version, schema, "T: {type: string}")
			})
		}
	}
}
