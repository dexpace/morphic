// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// wholeCase is a schema whose position keeps its inline allOf principal whole,
// at entry, the principal writing minimum 1, and beside, the position's own
// minimum, "" for none.
type wholeCase struct{ name, schema, entry, beside string }

// wholeCases keep the principal whole for a disagreeing extension or a
// closure, with and without the position writing minimum too, alone and with a
// second entry.
var wholeCases = []wholeCase{
	{"an extension, the position writing minimum", "W1: {allOf: [{type: string, minimum: 1, x-k: 1}], minimum: 2, x-k: 2}", "W1/allOf/0", "2"},
	{"a closure, the position writing minimum", "W2: {allOf: [{type: string, minimum: 1, unevaluatedProperties: false}], minimum: 2}", "W2/allOf/0", "2"},
	{"an extension", "N1: {allOf: [{type: string, minimum: 1, x-k: 1}], x-k: 2}", "N1/allOf/0", ""},
	{"a closure", "N2: {allOf: [{type: string, minimum: 1, unevaluatedProperties: false}]}", "N2/allOf/0", ""},
	{"an extension, a second entry after", "W3: {allOf: [{type: string, minimum: 1, x-k: 1}, {description: d}], minimum: 2, x-k: 2}",
		"W3/allOf/0", "2"},
	{"an extension, a second entry before", "N3: {allOf: [{description: d}, {type: string, minimum: 1, x-k: 1}], x-k: 2}",
		"N3/allOf/1", ""},
	{"a closure, a second entry before", "N4: {allOf: [{description: d}, {type: string, minimum: 1, unevaluatedProperties: false}]}",
		"N4/allOf/1", ""},
}

// TestPrincipalWhole_AKeptValueIsKeptOnceAndReportedOnce pins an inline
// principal its position keeps whole, in both declaration orders: its
// minimum, which no field holds, is kept once, inside the whole entry, and
// reported once, by the report of that entry, which names it; the position's
// own minimum holds the plain key with its own report
// (docs/compilers/openapi.md#allof-inline-residue).
func TestPrincipalWhole_AKeptValueIsKeptOnceAndReportedOnce(t *testing.T) {
	t.Parallel()
	lines := make([]string, 0, len(wholeCases))
	for _, tc := range wholeCases {
		lines = append(lines, "    "+tc.schema)
	}
	reversed := slices.Clone(lines)
	slices.Reverse(reversed)
	for _, order := range [][]string{lines, reversed} {
		doc, diags := parseFull(t, openapitest.ComponentSpec(strings.Join(order, "\n")+"\n"))
		openapitest.RequireNoErrorDiags(t, diags)
		sites := unmodeledSites(doc)
		for _, tc := range wholeCases {
			t.Run(tc.name, func(t *testing.T) {
				name, _, _ := strings.Cut(tc.entry, "/")
				var principal, beside int
				for _, site := range sites {
					if !strings.Contains(site.path, "/"+name+"\"") && !strings.Contains(site.path, "/"+name+"]") {
						continue
					}
					var v map[string]any
					if json.Unmarshal(site.entry.Value, &v) == nil && v["minimum"] == 1.0 {
						principal++
					}
					switch {
					case site.key == "openapi:minimum" && string(site.entry.Value) == "1":
						principal++
					case site.key == "openapi:minimum" && string(site.entry.Value) == tc.beside:
						beside++
					}
				}
				assert.Equal(t, 1, principal, "the principal's minimum is kept once, inside its whole entry")
				assert.Equal(t, map[bool]int{true: 1}[tc.beside != ""], beside, "the position's minimum holds the plain key")
				var said []string
				for _, d := range diags {
					at := strings.TrimPrefix(string(d.Provenance.Pointer), "/components/schemas/")
					if strings.HasPrefix(at, tc.entry) && strings.Contains(d.Message, "minimum") {
						said = append(said, d.Message)
					}
				}
				require.Len(t, said, 1, "the principal's minimum is reported once: %v", said)
				assert.NotContains(t, said[0], "declared beside it", "by the whole entry's report, not the census's")
				_, listed, ok := strings.Cut(said[0], "(")
				assert.True(t, ok && strings.Contains(strings.SplitN(listed, ")", 2)[0], "minimum"),
					"the whole entry's report names it among what it holds: %s", said[0])
			})
		}
	}
}

// reasonCase is a schema whose position keeps entries whole, with the reason
// and report code each entry's key must carry.
type reasonCase struct {
	name, schema string
	want         map[string]ir.UnmodeledReason
}

// TestPrincipalWhole_TheWholeEntrysReasonReadsEveryKeywordItHolds pins the
// reason a principal kept whole is kept under: the one its kept keywords share
// where they share one, the bound its own reading left it among them, else
// degraded_lowering, with the report code that reason takes; an entry kept
// whole beside it keeps its own (docs/compilers/openapi.md#allof-inline-residue).
func TestPrincipalWhole_TheWholeEntrysReasonReadsEveryKeywordItHolds(t *testing.T) {
	t.Parallel()
	for _, tc := range []reasonCase{
		{"an extension and a bound", "{allOf: [{type: string, minimum: 1, x-k: 1}], x-k: 2}",
			map[string]ir.UnmodeledReason{"openapi:allOf/0": ir.ReasonDegradedLowering}},
		{"a closure and a bound", "{allOf: [{type: string, minimum: 1, unevaluatedProperties: false}]}",
			map[string]ir.UnmodeledReason{"openapi:allOf/0": ir.ReasonDegradedLowering}},
		{"an extension alone", "{allOf: [{type: string, x-k: 1}], x-k: 2}",
			map[string]ir.UnmodeledReason{"openapi:allOf/0": ir.ReasonVendorExtension}},
		{"a closure alone", "{allOf: [{type: string, unevaluatedProperties: false}]}",
			map[string]ir.UnmodeledReason{"openapi:allOf/0": ir.ReasonValidationOnly}},
		{"an extension and a bound, an entry beside", "{allOf: [{type: string, minimum: 1, x-k: 1}, {x-k: 5}], x-k: 2}",
			map[string]ir.UnmodeledReason{"openapi:allOf/0": ir.ReasonDegradedLowering, "openapi:allOf/1": ir.ReasonVendorExtension}},
		{"a closure and a bound, an entry beside", "{allOf: [{type: string, minimum: 1, unevaluatedProperties: false}, {unevaluatedItems: false}]}",
			map[string]ir.UnmodeledReason{"openapi:allOf/0": ir.ReasonDegradedLowering, "openapi:allOf/1": ir.ReasonValidationOnly}},
		{"an extension alone, an entry beside", "{allOf: [{type: string, x-k: 1}, {minimum: 3, x-k: 5}], x-k: 2}",
			map[string]ir.UnmodeledReason{"openapi:allOf/0": ir.ReasonVendorExtension, "openapi:allOf/1": ir.ReasonDegradedLowering}},
		{"a closure alone, an entry beside", "{allOf: [{type: string, unevaluatedProperties: false}, {minimum: 3, unevaluatedItems: false}]}",
			map[string]ir.UnmodeledReason{"openapi:allOf/0": ir.ReasonValidationOnly, "openapi:allOf/1": ir.ReasonDegradedLowering}},
		{"no principal held", "{allOf: [{type: string}, {minimum: 1, x-k: 1}], x-k: 2}",
			map[string]ir.UnmodeledReason{"openapi:allOf/1": ir.ReasonDegradedLowering}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec("    X: "+tc.schema+"\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			held := doc.Types[componentID("X")].Common().Unmodeled
			for key, want := range tc.want {
				assert.Equal(t, want, unmodeledEntry(t, held, key).Reason, key)
				at := "/components/schemas/X/" + strings.TrimPrefix(key, "openapi:")
				code := diag.DegradedConstruct
				if want == ir.ReasonValidationOnly {
					code = diag.ValidationOnlyKeyword
				}
				assert.Equal(t, 1, countCode(diags, code, at), "%s is reported under the code its reason takes", key)
			}
		})
	}
}
