package irverify_test

import (
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
	assert.Equal(t, "doc.Types[t/x/Model].TypeCommon.Provenance", got[0].Path)
}

// TestVerify_NegativeProvenanceSourceIsAViolation covers the other end of the
// range, which a bare `>= len` test would let through.
func TestVerify_NegativeProvenanceSourceIsAViolation(t *testing.T) {
	doc := validDoc()
	doc.Sources = oneSource()
	doc.Types["t/x/Model"].Common().Provenance = ir.Provenance{Source: -1}
	assert.Contains(t, codesOf(irverify.Verify(doc)), "ir/provenance-source-out-of-range")
}

// TestVerify_SourcelessDocAdmitsOnlyZero pins the one tolerance: a document
// declaring no sources makes no claim about source indexing, so a zero-value
// Provenance is clean there — but a non-zero index still names a table entry
// that cannot exist.
func TestVerify_SourcelessDocAdmitsOnlyZero(t *testing.T) {
	clean := validDoc()
	require.Empty(t, clean.Sources)
	assert.Empty(t, irverify.Verify(clean))

	broken := validDoc()
	broken.Types["t/x/Model"].Common().Provenance = ir.Provenance{Source: 1}
	assert.Contains(t, codesOf(irverify.Verify(broken)), "ir/provenance-source-out-of-range")
}

// TestVerify_ProvenanceIsCheckedEverywhere confirms the check is document-wide
// rather than scoped to one carrier: a diagnostic and a Preserved entry are
// neither of them types, and both must be reached.
func TestVerify_ProvenanceIsCheckedEverywhere(t *testing.T) {
	doc := validDoc()
	doc.Sources = oneSource()
	doc.Diagnostics = []ir.Diagnostic{
		ir.NewDiagnostic(ir.SeverityWarning, "openapi/x", "m", ir.Provenance{Source: 9}),
	}
	doc.Types["t/x/Model"].Common().Preserved = ir.Preserved{
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
