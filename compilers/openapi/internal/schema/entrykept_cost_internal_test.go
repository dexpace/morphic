// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// keptShape writes the schema X of a document: `top` beside an allOf of n
// entries, entry(i) writing the i-th, so one shape varies how many keywords the
// entries keep and whether they write them alike.
func keptShape(n int, top string, entry func(i int) string) string {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n    X:\n")
	b.WriteString("      type: object\n" + top + "      allOf:\n")
	for i := range n {
		b.WriteString("        - " + entry(i) + "\n")
	}
	return b.String()
}

// keptOfShape returns what writtenApartKeys reads for the schema X of src: the
// composition's entries and what each keeps.
func keptOfShape(t *testing.T, src string) (lowering.Ctx, *statement.Schema, allOfComposition, [][]string) {
	t.Helper()
	lowerer, _ := LoweredFor(t, src)
	for name, st := range lowerer.Schemas() {
		if name != "X" {
			continue
		}
		comp := allOfEntries(lowerer.Ctx, st.Schema, "/components/schemas/X")
		kepts := make([][]string, len(comp.entries))
		for i, entry := range comp.entries {
			kepts[i] = keptOf(lowerer.Ctx, keptEntry{entry: entry, residue: compositionResidue(lowerer.Ctx, comp, i)})
		}
		return lowerer.Ctx, st.Schema, comp, kepts
	}
	require.Fail(t, "the document declares no schema X")
	return lowering.Ctx{}, nil, allOfComposition{}, nil
}

// keyShapes are the spellings an allOf of n entries keeps keywords in: each
// written by one entry (as many keys as entries), by all of them alike, by all
// of them otherwise, and beside a $ref naming nothing, which asks the entries
// keeping a key apart from the rest of its writers.
var keyShapes = []struct {
	name string
	top  string
	of   func(i int) string
}{
	{"each key written once", "", func(i int) string {
		return fmt.Sprintf("{k%da: 1, k%db: 2, k%dc: 3, k%dd: 4, k%de: 5}", i, i, i, i, i)
	}},
	{"each key written alike", "", func(int) string { return "{ka: 1, kb: 2, kc: 3, kd: 4, ke: 5}" }},
	{"each key written otherwise", "", func(i int) string {
		return fmt.Sprintf("{ka: %d, kb: %d, kc: %d, kd: %d, ke: %d}", i, i, i, i, i)
	}},
	{"keys beside a $ref naming nothing", "      k0a: 1\n", func(i int) string {
		return fmt.Sprintf("{$ref: '#/components/schemas/None', k%da: 1, k%db: 2, k0a: 1}", i, i)
	}},
}

// TestEntryKeeps_ReadsEachWrittenKeyOnce pins that keeping an allOf's keywords
// apart costs what the entries write once, not once for each kept keyword
// across every entry: from 2n entries to 4n the reads grow at most 2.5 times
// what they grow from n to 2n, whichever way the entries spell the keywords.
func TestEntryKeeps_ReadsEachWrittenKeyOnce(t *testing.T) {
	t.Parallel()
	for _, shape := range keyShapes {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()
			var spent [3]int
			for i, n := range []int{100, 200, 400} {
				loweringContext, schema, comp, kepts := keptOfShape(t, keptShape(n, shape.top, shape.of))
				var reads budget.Budget
				writtenApartKeys(loweringContext, schema, comp, kepts, &reads)
				spent[i] = reads.Spent()
			}
			assert.Positive(t, spent[0], "nothing was read: the shape reaches no key")
			assert.LessOrEqual(t, float64(spent[2]-spent[1]), 2.5*float64(spent[1]-spent[0]),
				"reads at 100, 200 and 400 entries: %v", spent)
		})
	}
}

// writtenApartOf is the definition writtenApartKeys answers for one key, read
// the plain way: the writers of key in order, one walk of the composition each.
func writtenApartOf(loweringContext lowering.Ctx, schema *statement.Schema, comp allOfComposition, key string, kepts [][]string) bool {
	var raws []ir.RawValue
	if hasKey(schema, key) {
		raw, err := annotation.RawFromNode(keyNode(schema, key))
		if err != nil {
			return true
		}
		raws = append(raws, raw)
	}
	for i, entry := range comp.entries {
		if (entry.schema.Ref != nil && !failedRef(loweringContext, entry)) || !stated(entry.schema.Schema) || !hasKey(entry.schema.Schema, key) ||
			(kepts != nil && !slices.Contains(kepts[i], key)) {
			continue
		}
		raw, err := annotation.RawFromNode(keyNode(entry.schema.Schema, key))
		if err != nil {
			return true
		}
		raws = append(raws, raw)
	}
	return slices.ContainsFunc(raws, func(raw ir.RawValue) bool { return !bytes.Equal(raws[0], raw) })
}

// TestWrittenApartKeys_AnswersWhatTheirWritersWrite pins writtenApartKeys to
// the plain reading of its definition, writtenApartOf, for every kept key of
// shapes that vary who writes it, with what, and who keeps it: alike and
// otherwise, by the allOf's own schema, by an entry that writes it null, by one
// whose value cannot be rendered, and beside a $ref naming nothing.
func TestWrittenApartKeys_AnswersWhatTheirWritersWrite(t *testing.T) {
	t.Parallel()
	for name, entries := range map[string][]string{
		"alike":                     {"{ka: 1}", "{ka: 1}", "{kb: 2}"},
		"otherwise":                 {"{ka: 1}", "{ka: 2}", "{kb: 2}"},
		"one writer":                {"{ka: 1}", "{kb: 2}"},
		"null and a value":          {"{ka: null}", "{ka: 1}"},
		"an unrendered key":         {"{ka: !!int abc}", "{kb: 1}"},
		"two unrendered":            {"{ka: !!int abc}", "{ka: !!int abc}"},
		"a $ref's keys":             {"{$ref: '#/components/schemas/None', ka: 1}", "{ka: 1}", "{$ref: '#/components/schemas/None', ka: 2}"},
		"a $ref's lone key":         {"{$ref: '#/components/schemas/None', ka: 1}", "{ka: 2}"},
		"a keyword merged in place": {"{$ref: '#/components/schemas/None', properties: {a: {}}}", "{properties: {b: {}}}"},
		"a $ref's own value":        {"{$ref: '#/components/schemas/None', ka: 1}", "{$ref: '#/components/schemas/None', ka: 1}"},
	} {
		for _, top := range []string{"", "      ka: 1\n", "      ka: 3\n"} {
			t.Run(fmt.Sprintf("%s beside %q", name, top), func(t *testing.T) {
				t.Parallel()
				loweringContext, schema, comp, kepts := keptOfShape(t, keptShape(len(entries), top, func(i int) string { return entries[i] }))
				var reads budget.Budget
				writers, keepers := writtenApartKeys(loweringContext, schema, comp, kepts, &reads)
				wantKeepers := map[string]bool{}
				wantWriters := map[string]bool{}
				for i, kept := range kepts {
					for _, key := range kept {
						wantWriters[key] = writtenApartOf(loweringContext, schema, comp, key, nil)
						if failedRef(loweringContext, comp.entries[i]) {
							wantKeepers[key] = writtenApartOf(loweringContext, schema, comp, key, kepts)
						}
					}
				}
				assert.Equal(t, wantWriters, writers, "the writers of each kept key")
				assert.Equal(t, wantKeepers, keepers, "the writers among the entries keeping it")
			})
		}
	}
}

// TestEntryKeeps_KeepsTheSameKeysInEitherOrder pins that what an allOf's
// entries keep, and the order it is reported in, do not depend on the order
// the entries are written in: the keys kept apart are the same, and so are the
// entries kept whole, with the keywords they hold.
func TestEntryKeeps_KeepsTheSameKeysInEitherOrder(t *testing.T) {
	t.Parallel()
	entries := []string{
		"{ka: 1, kb: 1}", "{ka: 2, kc: 3}", "{kd: 4, ke: 5}", "{kb: 1, kf: 6}",
		"{$ref: '#/components/schemas/None', kg: 7, ka: 1}",
	}
	kept := func(order []string) map[string][]string {
		t.Helper()
		lowerer, _ := LoweredFor(t, keptShape(len(order), "      kz: 9\n", func(i int) string { return order[i] }))
		for name, st := range lowerer.Schemas() {
			if name != "X" {
				continue
			}
			comp := readAllOf(lowerer.Ctx, st.Schema, "/components/schemas/X")
			out := map[string][]string{}
			for i, keptEntry := range comp.kept {
				entry := strings.TrimSpace(order[i])
				out[entry] = append(slices.Clone(keptEntry.own), fmt.Sprint(keptEntry.whole, keptEntry.reason))
			}
			return out
		}
		require.Fail(t, "the document declares no schema X")
		return nil
	}
	forward := slices.Clone(entries)
	backward := slices.Clone(entries)
	slices.Reverse(backward)
	assert.Equal(t, kept(forward), kept(backward))
	require.Len(t, kept(forward), len(entries), "each entry is told apart")
}
