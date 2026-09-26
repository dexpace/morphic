package irverify_test

import (
	"math"
	"testing"
	"unicode/utf8"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// oneSource is the shape every compiled document has today: exactly one loaded
// file, so index 0 is the only addressable source.
func oneSource() []ir.SourceInfo {
	return []ir.SourceInfo{{Format: "openapi", Path: "api.yaml", Hash: "h"}}
}

// TestVerify_InRangeProvenanceSourceIsClean pins the negative half: a document
// that declares its source and indexes it must report nothing.
func TestVerify_InRangeProvenanceSourceIsClean(t *testing.T) {
	doc := validDoc()
	doc.Sources = oneSource()
	// The pointer matches the ID's own path, as a derived ID's always does: an ID
	// disagreeing with the coordinate it records is its own violation.
	doc.Types["t/x/Model"].Common().Provenance = ir.Provenance{Source: 0, Pointer: "/Model"}
	assert.Empty(t, irverify.Verify(doc))
}

// TestVerify_OutOfRangeProvenanceSourceIsAViolation mutates a source index past
// the declared table — the stale-index bug that makes a report point at a file
// the document never loaded.
func TestVerify_OutOfRangeProvenanceSourceIsAViolation(t *testing.T) {
	doc := validDoc()
	doc.Sources = oneSource()
	doc.Types["t/x/Model"].Common().Provenance = ir.Provenance{Source: 3}

	got := irverify.Verify(doc)
	require.Len(t, got, 1)
	assert.Equal(t, "ir/provenance-source-out-of-range", got[0].Code)
	assert.Equal(t, "doc.Types[t/x/Model].Provenance", got[0].Path)
}

// TestVerify_NoSourceSentinelIsClean pins the declared out-of-table value: a
// node that came from no input file says so with ir.NoSource, and holding it to
// the source table would fail every document carrying a pass diagnostic.
func TestVerify_NoSourceSentinelIsClean(t *testing.T) {
	doc := validDoc()
	doc.Sources = oneSource()
	doc.Types["t/x/Model"].Common().Provenance = ir.Provenance{Source: ir.NoSource}
	assert.Empty(t, irverify.Verify(doc))
}

// TestVerify_BelowSentinelProvenanceSourceIsAViolation covers the end of the
// range a bare `>= len` test would let through, and pins that admitting
// ir.NoSource admits nothing else negative: a second, undeclared sentinel is the
// drift this check exists to catch.
func TestVerify_BelowSentinelProvenanceSourceIsAViolation(t *testing.T) {
	for _, src := range []int{ir.NoSource - 1, ir.NoSource - 2, math.MinInt} {
		doc := validDoc()
		doc.Sources = oneSource()
		doc.Types["t/x/Model"].Common().Provenance = ir.Provenance{Source: src}
		assert.Contains(t, codesOf(irverify.Verify(doc)), "ir/provenance-source-out-of-range",
			"source %d addresses no declared source and is not the declared sentinel", src)
	}
}

// TestVerify_SourcelessDocAdmitsZeroAndTheSentinel pins the one tolerance: a
// document declaring no sources makes no claim about source indexing, so a
// zero-value Provenance is clean there — but any other index except the
// no-source sentinel still names a table entry that cannot exist.
func TestVerify_SourcelessDocAdmitsZeroAndTheSentinel(t *testing.T) {
	clean := validDoc()
	require.Empty(t, clean.Sources)
	assert.Empty(t, irverify.Verify(clean))

	sentinel := validDoc()
	sentinel.Types["t/x/Model"].Common().Provenance = ir.Provenance{Source: ir.NoSource}
	assert.Empty(t, irverify.Verify(sentinel))

	for _, src := range []int{1, ir.NoSource - 1} {
		broken := validDoc()
		broken.Types["t/x/Model"].Common().Provenance = ir.Provenance{Source: src}
		assert.Contains(t, codesOf(irverify.Verify(broken)), "ir/provenance-source-out-of-range",
			"source %d addresses no entry of an empty source table", src)
	}
}

// TestVerify_ProvenanceIsCheckedEverywhere confirms the check is document-wide
// rather than scoped to one carrier: a diagnostic and an Unmodeled entry are
// neither of them types, and both must be reached.
func TestVerify_ProvenanceIsCheckedEverywhere(t *testing.T) {
	doc := validDoc()
	doc.Sources = oneSource()
	doc.Diagnostics = []ir.Diagnostic{
		ir.NewDiagnostic(ir.SeverityWarning, "openapi/x", "m", ir.Provenance{Source: 9}),
	}
	doc.Types["t/x/Model"].Common().Unmodeled = ir.Unmodeled{
		"openapi:x-ext": {
			Reason:     ir.ReasonVendorExtension,
			Value:      ir.RawValue(`1`),
			Provenance: ir.Provenance{Source: 4},
		},
	}

	var paths []string
	for _, v := range irverify.Verify(doc) {
		if v.Code == "ir/provenance-source-out-of-range" {
			paths = append(paths, v.Path)
		}
	}
	assert.Len(t, paths, 2, "both the diagnostic and the preserved entry must be reported: %v", paths)
}

// TestVerify_ProvenanceLocatorRules is the table for appendLocatorViolations:
// one row per rule (a malformed pointer, a malformed position, a locator on
// NoSource) and the clean neighbour beside it, so a rule tightened to catch
// more than it should reddens here rather than only in the rule's own
// package. The provenance sits on a diagnostic — the shape #400's ill-formed
// message bytes were found on — and each row asserts the exact set of codes
// Verify reports, not merely that some code fired.
func TestVerify_ProvenanceLocatorRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		prov ir.Provenance
		want []string
	}{
		{
			name: "pointer with no leading slash is malformed",
			prov: ir.Provenance{Pointer: "components/x"},
			want: []string{"ir/provenance-pointer-malformed"},
		},
		{
			name: "pointer with a tilde escaping nothing is malformed",
			prov: ir.Provenance{Pointer: "/a~2b"},
			want: []string{"ir/provenance-pointer-malformed"},
		},
		{
			name: "pointer with invalid UTF-8 is malformed",
			prov: ir.Provenance{Pointer: "/a\xffb"},
			want: []string{"ir/provenance-pointer-malformed"},
		},
		{
			name: "the empty pointer names the whole document and is clean",
			prov: ir.Provenance{Pointer: ""},
			want: nil,
		},
		{
			name: "an escaped tilde and slash are clean",
			prov: ir.Provenance{Pointer: "/a~0b~1c"},
			want: nil,
		},
		{
			name: "line 0 with a column is malformed: lines are 1-based",
			prov: ir.Provenance{Position: ir.Position{Line: 0, Column: 3}},
			want: []string{"ir/provenance-position-malformed"},
		},
		{
			name: "a negative column is malformed",
			prov: ir.Provenance{Position: ir.Position{Line: 3, Column: -1}},
			want: []string{"ir/provenance-position-malformed"},
		},
		{
			name: "a positive line with column 0 is clean: the column is merely unknown",
			prov: ir.Provenance{Position: ir.Position{Line: 3, Column: 0}},
			want: nil,
		},
		{
			name: "a pointer on NoSource has nothing to be inside",
			prov: ir.Provenance{Source: ir.NoSource, Pointer: "/x"},
			want: []string{"ir/provenance-locator-without-source"},
		},
		{
			name: "a position on NoSource has nothing to be inside",
			prov: ir.Provenance{Source: ir.NoSource, Position: ir.Position{Line: 5, Column: 1}},
			want: []string{"ir/provenance-locator-without-source"},
		},
		{
			name: "a node on NoSource is exactly what a pass finding looks like",
			prov: ir.Provenance{Source: ir.NoSource, Node: "op/x"},
			want: nil,
		},
		{
			name: "a source with a pointer, a position and a node all together is clean",
			prov: ir.Provenance{
				Source:   0,
				Pointer:  "/x",
				Position: ir.Position{Line: 5, Column: 1},
				Node:     "op/x",
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := &ir.Document{
				IRVersion: ir.IRVersion,
				Diagnostics: []ir.Diagnostic{
					{Severity: ir.SeverityWarning, Code: "test/x", Message: "m", Provenance: tt.prov},
				},
			}

			got := irverify.Verify(doc)
			var codes []string
			for _, v := range got {
				codes = append(codes, v.Code)
				assert.True(t, utf8.ValidString(v.Message),
					"a violation's message is valid UTF-8 even when it quotes an ill-formed pointer (#400)")
			}
			if diff := cmp.Diff(tt.want, codes); diff != "" {
				t.Errorf("codes mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
