package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// TestVerify_InvalidUTF8DiagnosticIsAViolation asserts the diagnostics check
// flags a message carrying an ill-formed byte run — the state that breaks the
// Document's byte-for-byte JSON round-trip (invariant #7). Building the
// diagnostic as a raw struct literal bypasses ir.NewDiagnostic, standing in for
// any producer that forgets to route through the sanitizing constructor.
func TestVerify_InvalidUTF8DiagnosticIsAViolation(t *testing.T) {
	doc := validDoc()
	doc.Diagnostics = []ir.Diagnostic{
		{Severity: ir.SeverityError, Code: "openapi/validation", Message: "bad \xe0\xa5 byte"},
	}
	got := irverify.Verify(doc)
	require.NotEmpty(t, got)
	assert.Contains(t, codesOf(got), "ir/diagnostic-invalid-utf8")
}

// TestVerify_ValidUTF8DiagnosticIsClean confirms a well-formed message — the
// only kind ir.NewDiagnostic can emit — raises no violation, even when its
// source text was ill-formed before coercion.
func TestVerify_ValidUTF8DiagnosticIsClean(t *testing.T) {
	doc := validDoc()
	doc.Diagnostics = []ir.Diagnostic{
		ir.NewDiagnostic(ir.SeverityError, "openapi/validation", "bad \xe0\xa5 byte", ir.Provenance{}),
	}
	got := irverify.Verify(doc)
	assert.NotContains(t, codesOf(got), "ir/diagnostic-invalid-utf8")
}

// TestVerify_InvalidUTF8DiagnosticPathUsesIndex pins the violation Path to the
// offending diagnostic's slice index so a reproducer points at the exact entry.
func TestVerify_InvalidUTF8DiagnosticPathUsesIndex(t *testing.T) {
	doc := validDoc()
	doc.Diagnostics = []ir.Diagnostic{
		ir.NewDiagnostic(ir.SeverityWarning, "openapi/validation", "clean", ir.Provenance{}),
		{Severity: ir.SeverityError, Code: "openapi/validation", Message: "bad \xff here"},
	}
	got := irverify.Verify(doc)

	var found *irverify.Violation
	for i := range got {
		if got[i].Code == "ir/diagnostic-invalid-utf8" {
			found = &got[i]
			break
		}
	}
	require.NotNil(t, found, "expected an ir/diagnostic-invalid-utf8 violation")
	assert.Equal(t, "diagnostics[1]", found.Path)
}
