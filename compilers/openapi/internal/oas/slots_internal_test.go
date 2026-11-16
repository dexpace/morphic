// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas

import (
	"go/constant"
	"go/types"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
)

// everySchemaRow returns the Schema Object's rows at each minor in each
// dialect the table has rows for, the minor's own included, by "minor dialect".
func everySchemaRow(t *testing.T) map[string]SchemaRules {
	t.Helper()
	rows := map[string]SchemaRules{}
	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		for _, id := range append([]string{""}, dialectIDs()...) {
			r, unread := Table().SchemaRules(version, id)
			require.False(t, unread, "%s %s", version, id)
			require.NotNil(t, r.rows, "%s %s", version, id)
			rows[version+" "+id] = r
		}
	}
	return rows
}

// dialectIDs returns the id of each dialect the table has rows for.
func dialectIDs() []string {
	dialects := schemaDialects()
	ids := make([]string, 0, len(dialects))
	for _, d := range dialects {
		ids = append(ids, d.id)
	}
	return ids
}

// TestSlots_EveryKeywordOfEveryDialectHasOne pins that the slot table places
// every keyword a row defines, or defines later, in some slot, so no keyword
// of any dialect falls to SlotUnknown by being missed.
func TestSlots_EveryKeywordOfEveryDialectHasOne(t *testing.T) {
	t.Parallel()
	names := map[string]bool{}
	rows := everySchemaRow(t)
	for row, r := range rows {
		for name := range r.rows.fields {
			names[name] = true
			assert.NotEqual(t, SlotUnknown, slotOf(name), "%s defines %s", row, name)
		}
		for name := range r.rows.later {
			names[name] = true
			assert.NotEqual(t, SlotUnknown, slotOf(name), "%s defines %s later", row, name)
		}
	}
	require.NotEmpty(t, rows, "read no row")
	require.NotEmpty(t, names, "read no keyword")
}

// TestSlots_ADraftSpellingSharesItsTwins pins that each spelling only an
// earlier draft defines speaks of what its 2020-12 twin speaks of
// (docs/compilers/openapi.md#dialect-keywords), so a key's slot never turns on
// which of the two an author wrote.
func TestSlots_ADraftSpellingSharesItsTwins(t *testing.T) {
	t.Parallel()
	for _, pair := range [][2]string{
		{"id", "$id"},
		{"definitions", "$defs"},
		{"$recursiveRef", "$dynamicRef"},
		{"$recursiveAnchor", "$dynamicAnchor"},
		{"additionalItems", "items"},
		{"dependencies", "dependentSchemas"},
	} {
		assert.NotEqual(t, SlotUnknown, slotOf(pair[1]), pair[1])
		assert.Equal(t, slotOf(pair[1]), slotOf(pair[0]), "%s speaks of what %s does", pair[0], pair[1])
	}
}

// TestSlots_EverySlotHoldsAKeyword pins the other direction: each slot the
// package declares, read off its constants rather than a list here, holds a
// keyword some row defines, so no slot is one no key can land in.
func TestSlots_EverySlotHoldsAKeyword(t *testing.T) {
	t.Parallel()
	pkg, err := decls.Load("github.com/dexpace/morphic/compilers/openapi/internal/oas")
	require.NoError(t, err)
	named, err := pkg.Named("Slot")
	require.NoError(t, err)

	held := map[Slot]bool{}
	for _, r := range everySchemaRow(t) {
		for _, name := range slices.Concat(slices.Collect(maps.Keys(r.rows.fields)), slices.Collect(maps.Keys(r.rows.later))) {
			held[slotOf(name)] = true
		}
	}
	declared := 0
	for c := range pkg.Objects[*types.Const]() {
		if !types.Identical(c.Type(), named) {
			continue
		}
		declared++
		v, exact := constant.Uint64Val(c.Val())
		require.True(t, exact, c.Name())
		if Slot(v) != SlotUnknown {
			assert.True(t, held[Slot(v)], "no keyword of any row speaks of %s", c.Name())
		}
	}
	require.Greater(t, declared, 1, "read no slot but SlotUnknown")
}

// TestSchemaRules_SlotIsTheRowsInForce pins each answer of Slot: a keyword the
// rows define, one they define later, one they do not, and any key of the
// zero rows. A name no dialect defines has no slot of its own either, so a
// row a later change adds without a slot reads as unknown until the switch
// names it.
func TestSchemaRules_SlotIsTheRowsInForce(t *testing.T) {
	t.Parallel()
	at30, _ := Table().SchemaRules("3.0.3", "")
	at31, _ := Table().SchemaRules("3.1.0", "")
	for _, tc := range []struct {
		name  string
		rules SchemaRules
		key   string
		slot  Slot
		later bool
	}{
		{"defined", at30, "nullable", SlotKinds, false},
		{"defined later", at30, "prefixItems", SlotArray, true},
		{"undefined", at31, "nullable", SlotUnknown, false},
		{"an extension", at31, "x-a", SlotUnknown, false},
		{"the zero rows", SchemaRules{}, "type", SlotUnknown, false},
	} {
		slot, later := tc.rules.Slot(tc.key)
		assert.Equal(t, tc.slot, slot, tc.name)
		assert.Equal(t, tc.later, later, tc.name)
	}
	assert.Equal(t, SlotUnknown, slotOf("x-a"), "a name no dialect defines")
}
