// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// TestAllOfEntries_DepthCapKeepsWrapperVerbatim pins the bound on flattening: a
// chain of wrappers as deep as the cap is flattened whole, and one level more
// leaves the wrapper at the cap verbatim under a key spelling its path, with the
// ref inside it unclassified and everything above it flattened. The diagnostic
// blames the depth, not allOf.
func TestAllOfEntries_DepthCapKeepsWrapperVerbatim(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		wrappers int
		flat     bool
	}{
		{"at the cap", statement.MaxFlattenDepth, true},
		{"one past the cap", statement.MaxFlattenDepth + 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var b strings.Builder
			b.WriteString("    A: {type: object, properties: {a: {type: string}}}\n    M:\n      allOf:\n")
			for i := range tc.wrappers {
				b.WriteString(strings.Repeat("  ", i) + "        - allOf:\n")
			}
			b.WriteString(strings.Repeat("  ", tc.wrappers) + "        - {$ref: '#/components/schemas/A'}\n")

			doc, diags := LowerSpec(t, openapitest.ComponentSpec(b.String()))
			openapitest.RequireNoErrorDiags(t, diags)
			m, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/M")].(*ir.Model)
			require.True(t, ok, "M should be a model")
			if tc.flat {
				require.NotNil(t, m.Base, "the ref at the bottom is classified")
				assert.Equal(t, ir.TypeID("t/openapi/components/schemas/A"), m.Base.Target)
				assert.Empty(t, m.Unmodeled)
				return
			}
			assert.Nil(t, m.Base, "the ref sits inside the wrapper kept verbatim")
			path := strings.TrimSuffix(strings.Repeat("allOf/0/", statement.MaxFlattenDepth+1), "/")
			entry, ok := m.Unmodeled["openapi:"+path]
			require.True(t, ok, "the wrapper at the cap is kept under its path; got %v", m.Unmodeled)
			assert.Equal(t, jsontext.Pointer("/components/schemas/M/"+path), entry.Provenance.Pointer)
			assert.Len(t, m.Unmodeled, 1)
			msg := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct,
				ir.SeverityInfo, "/components/schemas/M/"+path)
			assert.Equal(t, "inline allOf entry is nested past the flattening depth of "+strconv.Itoa(statement.MaxFlattenDepth)+
				", so its own allOf was not flattened, and the entry is kept verbatim under Unmodeled", msg)
		})
	}
}

// TestAllOfEntries_KeysSpellEachEntrysPath pins, for the entries of a nested
// composition, the key each is kept under, which spells its path from the
// composed schema, and that a wrapper follows its own entries.
func TestAllOfEntries_KeysSpellEachEntrysPath(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec(`    M:
      allOf:
        - {type: object}
        - allOf:
            - {type: object}
            - allOf:
                - {type: object}
                - {type: object}
`))
	decl, ok := l.Doc.Components.Schemas.Get("M")
	require.True(t, ok)
	comp := allOfEntries(l.Ctx, l.Ctx.Statements().Of(decl).Schema, "/components/schemas/M")
	entries := comp.entries
	require.Len(t, entries, 6, "four inline entries and two wrappers")
	wantKeys := []string{
		"openapi:allOf/0",
		"openapi:allOf/1/allOf/0",
		"openapi:allOf/1/allOf/1/allOf/0",
		"openapi:allOf/1/allOf/1/allOf/1",
		"openapi:allOf/1/allOf/1",
		"openapi:allOf/1",
	}
	var inline int
	for i, e := range entries {
		assert.Equal(t, wantKeys[i], comp.entryKey(e.pointer))
		if !e.wrapper {
			inline++
		}
	}
	assert.Equal(t, 4, inline)
	assert.True(t, entries[len(entries)-1].wrapper, "a wrapper follows its own entries")
}

// TestAllOfRefEntries_StopsWhenTheConsumerDoes pins that the view of a
// flattened allOf's $ref entries yields them in order, a nested wrapper's
// included, and ends when the loop reading it breaks.
func TestAllOfRefEntries_StopsWhenTheConsumerDoes(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec(`    A: {type: object}
    B: {type: object}
    M:
      allOf:
        - allOf:
            - {$ref: '#/components/schemas/A'}
        - {type: object}
        - {$ref: '#/components/schemas/B'}
`))
	s := StatedIn(l, componentSchema(t, l, "M"))
	var all []*statement.Statement
	for b := range allOfRefEntries(l.Ctx, s) {
		all = append(all, b)
	}
	require.Len(t, all, 2, "the nested $ref and the one beside it")
	assert.Equal(t, StatedIn(l, componentSchema(t, l, "A")), refBranchTarget(l.Ctx, all[0]))
	assert.Equal(t, StatedIn(l, componentSchema(t, l, "B")), refBranchTarget(l.Ctx, all[1]))

	var seen int
	for range allOfRefEntries(l.Ctx, s) {
		seen++
		break
	}
	assert.Equal(t, 1, seen, "the view yields nothing after the loop breaks")
}

// TestAllOfEntries_DepthCapNamesWhatElseTheWrapperWrites pins that a wrapper
// kept at the cap lists the keywords it writes besides allOf, so the message
// says both why it was kept and what rides along. A wrapper whose type is not an
// object reads as a warning, which names the depth as well.
func TestAllOfEntries_DepthCapNamesWhatElseTheWrapperWrites(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		sibling  string
		severity ir.Severity
		want     string
	}{
		{"a keyword nothing reads", "minItems: 1", ir.SeverityInfo, "the entry (minItems) is kept verbatim"},
		{"non-object type", "type: string", ir.SeverityWarning, "the entry (type) is kept verbatim"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var b strings.Builder
			b.WriteString("    A: {type: object, properties: {a: {type: string}}}\n    M:\n      allOf:\n")
			for i := range statement.MaxFlattenDepth {
				b.WriteString(strings.Repeat("  ", i) + "        - allOf:\n")
			}
			pad := strings.Repeat("  ", statement.MaxFlattenDepth)
			b.WriteString(pad + "        - " + tc.sibling + "\n" + pad + "          allOf:\n" +
				pad + "            - {$ref: '#/components/schemas/A'}\n")

			_, diags := LowerSpec(t, openapitest.ComponentSpec(b.String()))
			openapitest.RequireNoErrorDiags(t, diags)
			path := strings.TrimSuffix(strings.Repeat("allOf/0/", statement.MaxFlattenDepth+1), "/")
			msg := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct,
				tc.severity, "/components/schemas/M/"+path)
			assert.Contains(t, msg, "nested past the flattening depth of "+strconv.Itoa(statement.MaxFlattenDepth))
			assert.Contains(t, msg, "was not flattened")
			assert.Contains(t, msg, tc.want)
			assert.NotContains(t, msg, "allOf)", "allOf is not listed as a kept keyword")
			assert.NotContains(t, msg, ", allOf")
		})
	}
}
