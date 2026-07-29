package irverify_test

import (
	"math"
	"testing"

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
	doc.Types["t/x/Model"].Common().Provenance = ir.Provenance{Source: 0, Pointer: "/components/schemas/Model"}
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
