// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas_test

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/dialect"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/compilers/openapi/internal/service/servicetest"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// sample returns a value of f's shape, one its format admits.
func sample(f oas.Field) string {
	switch {
	case f.Name == "type":
		return "string"
	case f.Format == oas.AbsoluteURI:
		return "'https://example.com/s'"
	case f.Format == oas.URI || f.Format == oas.ResourceURI:
		return "'#/components/schemas/Target'"
	}
	switch f.Shape {
	case oas.Boolean:
		return "true"
	case oas.Number, oas.PositiveNumber, oas.Natural:
		return "1"
	case oas.Nested:
		return "{}"
	case oas.Map:
		return "{a: {}}"
	case oas.List:
		return "[{}]"
	case oas.StringMap:
		return "{a: b}"
	case oas.StringList:
		return "[a]"
	case oas.RawMap:
		return "{a: [b]}"
	case oas.RawList:
		return "[a]"
	default:
		return "a"
	}
}

// slots returns which slot of st holds something: its reference, its allOf
// entries, or the struct of a slot.
func slots(st *statement.Statement) map[oas.Slot]bool {
	return map[oas.Slot]bool{
		oas.SlotReference: st.Ref != nil, oas.SlotConjunction: st.AllOf != nil, oas.SlotKinds: st.Kinds != nil,
		oas.SlotValues: st.Values != nil, oas.SlotFormat: st.Format != nil, oas.SlotNumber: st.Number != nil,
		oas.SlotText: st.Text != nil, oas.SlotArray: st.Array != nil, oas.SlotObject: st.Object != nil,
		oas.SlotChoice: st.Choice != nil, oas.SlotLogic: st.Logic != nil, oas.SlotNotes: st.Notes != nil,
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
		for _, uri := range append([]string{""}, oas.SchemaDialectIDs()...) {
			rules, _ := oas.Table().SchemaRules(version, uri)
			defined, later := oas.KeywordsOf(rules)
			fields := slices.Concat(defined, later)
			lines := make([]string, 0, 1+len(fields))
			lines = append(lines, "    Target: {}")
			for i, f := range fields {
				lines = append(lines, fmt.Sprintf("    K%d: {%s: %s}", i, f.Name, sample(f)))
			}
			head := "openapi: " + version + "\ninfo: {title: t, version: \"1\"}\npaths: {}\n"
			if uri != "" && version != "3.0.3" {
				head += "jsonSchemaDialect: '" + uri + "'\n"
			}
			c := servicetest.Context(t, head+"components:\n  schemas:\n"+strings.Join(lines, "\n")+"\n", service.Policy{})
			table := statement.NewTable(statement.Source{Bindings: c.Bindings(), Rules: dialect.NewBuilder(rules).Table()})
			for i, f := range fields {
				row := version + " " + uri + " " + f.Name
				st := table.At(c.Position(jsontext.Pointer(fmt.Sprintf("/components/schemas/K%d", i))))
				require.Equal(t, statement.Stated, st.Truth, row)
				require.Len(t, st.Keys, 1, row)
				k := st.Keys[0]
				slot, isLater := rules.Slot(f.Name)
				assert.Equal(t, f.Name, k.Name, row)
				assert.NotEqual(t, oas.SlotUnknown, k.Slot, row)
				assert.Equal(t, slot, k.Slot, row)
				assert.Equal(t, isLater, k.Later, row)
				for s, holds := range slots(st) {
					switch {
					case s != slot:
						assert.False(t, holds, "%s fills slot %d", row, s)
					case s != oas.SlotReference || f.Name == "$ref":
						assert.True(t, holds, "%s fills its slot", row)
					}
				}
				read++
			}
		}
	}
	require.Positive(t, read, "read no keyword of any row")
}
