// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package specrules_test

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/dialect"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/compilers/openapi/internal/service/servicetest"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// sample returns a value of field's shape, one its format admits.
func sample(field specrules.Field) string {
	switch {
	case field.Name == "type":
		return "string"
	case field.Format == specrules.AbsoluteURI:
		return "'https://example.com/s'"
	case field.Format == specrules.URI || field.Format == specrules.ResourceURI:
		return "'#/components/schemas/Target'"
	}
	switch field.Shape {
	case specrules.Boolean:
		return "true"
	case specrules.Number, specrules.PositiveNumber, specrules.Natural:
		return "1"
	case specrules.Nested:
		return "{}"
	case specrules.Map:
		return "{a: {}}"
	case specrules.List:
		return "[{}]"
	case specrules.StringMap:
		return "{a: b}"
	case specrules.StringList:
		return "[a]"
	case specrules.RawMap:
		return "{a: [b]}"
	case specrules.RawList:
		return "[a]"
	default:
		return "a"
	}
}

// slots returns which slot of st holds something: its reference, its allOf
// entries, or the struct of a slot.
func slots(st *statement.Statement) map[specrules.Slot]bool {
	return map[specrules.Slot]bool{
		specrules.SlotReference: st.Ref != nil, specrules.SlotConjunction: st.AllOf != nil, specrules.SlotKinds: st.Kinds != nil,
		specrules.SlotValues: st.Values != nil, specrules.SlotFormat: st.Format != nil, specrules.SlotNumber: st.Number != nil,
		specrules.SlotText: st.Text != nil, specrules.SlotArray: st.Array != nil, specrules.SlotObject: st.Object != nil,
		specrules.SlotChoice: st.Choice != nil, specrules.SlotLogic: st.Logic != nil, specrules.SlotNotes: st.Notes != nil,
	}
}

// TestSlots_EveryKeywordReadsIntoItsSlotAndNoOther reads, for every row at
// every minor in every dialect, a component writing each keyword the row
// defines or defines later, through the statement reader handed that row,
// and holds each to one key in the row's slot and to that slot's value alone.
// A reference holds the bindings' reading, which a $ref always has, and the
// resource no value but its keys.
func TestSlots_EveryKeywordReadsIntoItsSlotAndNoOther(t *testing.T) {
	t.Parallel()
	read := 0
	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		for _, uri := range append([]string{""}, specrules.SchemaDialectIDs()...) {
			rules, _ := specrules.Table().SchemaRules(version, uri)
			defined, later := specrules.KeywordsOf(rules)
			fields := slices.Concat(defined, later)
			lines := make([]string, 0, 1+len(fields))
			lines = append(lines, "    Target: {}")
			for i, field := range fields {
				lines = append(lines, fmt.Sprintf("    K%d: {%s: %s}", i, field.Name, sample(field)))
			}
			head := "openapi: " + version + "\ninfo: {title: t, version: \"1\"}\npaths: {}\n"
			if uri != "" && version != "3.0.3" {
				head += "jsonSchemaDialect: '" + uri + "'\n"
			}
			serviceContext := servicetest.Context(t, head+"components:\n  schemas:\n"+strings.Join(lines, "\n")+"\n", service.Policy{})
			table := statement.NewTable(statement.Source{Bindings: serviceContext.Bindings(), Rules: dialect.NewBuilder(rules).Table()})
			for i, field := range fields {
				row := version + " " + uri + " " + field.Name
				st := table.At(serviceContext.Position(jsontext.Pointer(fmt.Sprintf("/components/schemas/K%d", i))))
				require.Equal(t, statement.Stated, st.Truth, row)
				require.Len(t, st.Keys, 1, row)
				key := st.Keys[0]
				slot, isLater := rules.Slot(field.Name)
				assert.Equal(t, field.Name, key.Name, row)
				assert.NotEqual(t, specrules.SlotUnknown, key.Slot, row)
				assert.Equal(t, slot, key.Slot, row)
				assert.Equal(t, isLater, key.DefinedOnlyInLaterMinor, row)
				for slotValue, holds := range slots(st) {
					switch {
					case slotValue != slot:
						assert.False(t, holds, "%s fills slot %d", row, slotValue)
					case slotValue != specrules.SlotReference || field.Name == "$ref":
						assert.True(t, holds, "%s fills its slot", row)
					}
				}
				read++
			}
		}
	}
	require.Positive(t, read, "read no keyword of any row")
}
