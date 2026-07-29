package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// allReasons is the closed reason set from ir/preserved.go, listed by name so
// deleting or renaming a constant breaks the build here. ReasonOutOfScope is
// included even though no compiler writes it yet: it is declared, so the
// verifier must accept it — an unwritten reason is a gap in the compilers, not
// in the enum.
var allReasons = []ir.PreserveReason{
	ir.ReasonVendorExtension, ir.ReasonValidationOnly, ir.ReasonDegradedLowering,
	ir.ReasonNoIRHome, ir.ReasonOutOfScope,
}

// docPreserving hangs p off validDoc's model, a Preserved site the walk reaches
// through the Types registry.
func docPreserving(p ir.Preserved) *ir.Document {
	doc := validDoc()
	doc.Types["t/x/Model"].Common().Preserved = p
	return doc
}

// TestVerify_EveryDeclaredReasonIsClean pins the negative half of the reason
// check: none of the declared values may be reported, or the check would fail
// documents it exists to pass.
func TestVerify_EveryDeclaredReasonIsClean(t *testing.T) {
	p := ir.Preserved{}
	for _, r := range allReasons {
		p["openapi:"+string(r)] = ir.PreservedEntry{Reason: r, Value: ir.RawValue(`1`)}
	}
	assert.Empty(t, irverify.Verify(docPreserving(p)))
}

// TestVerify_EmptyPreserveReasonIsAViolation is the mutation the audit found
// the verifier blind to: an entry that round-trips clean and passes
// pass.Validate while carrying a reason no consumer's switch can route.
func TestVerify_EmptyPreserveReasonIsAViolation(t *testing.T) {
	doc := docPreserving(ir.Preserved{
		"openapi:x-rate-limit": {Value: ir.RawValue(`100`)},
	})
	got := irverify.Verify(doc)
	require.NotEmpty(t, got)
	assert.Equal(t, "ir/empty-preserve-reason", got[0].Code)
	assert.Equal(t, "doc.Types[t/x/Model].Preserved[openapi:x-rate-limit]", got[0].Path)
}

// TestVerify_UnknownPreserveReasonIsAViolation covers the other way a bare
// string enum goes wrong: a value that deserializes happily but names no
// declared reason.
func TestVerify_UnknownPreserveReasonIsAViolation(t *testing.T) {
	doc := docPreserving(ir.Preserved{
		"openapi:x-rate-limit": {Reason: "totally_invented", Value: ir.RawValue(`100`)},
	})
	got := irverify.Verify(doc)
	require.NotEmpty(t, got)
	assert.Equal(t, "ir/unknown-preserve-reason", got[0].Code)
	assert.Contains(t, got[0].Message, "totally_invented")
}

// TestVerify_EmptyPreservedKeyIsAViolation checks the key half alone: a valid
// reason must not mask an unlookupable key.
func TestVerify_EmptyPreservedKeyIsAViolation(t *testing.T) {
	doc := docPreserving(ir.Preserved{
		"": {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`1`)},
	})
	got := irverify.Verify(doc)
	require.Len(t, got, 1)
	assert.Equal(t, "ir/empty-preserved-key", got[0].Code)
	assert.Equal(t, `doc.Types[t/x/Model].Preserved[""]`, got[0].Path)
}

// TestVerify_EmptyKeyAndReasonReportBoth pins that the two defects are checked
// independently, so a doubly-broken entry names both rather than stopping at
// the first.
func TestVerify_EmptyKeyAndReasonReportBoth(t *testing.T) {
	doc := docPreserving(ir.Preserved{"": {Value: ir.RawValue(`1`)}})
	codes := codesOf(irverify.Verify(doc))
	assert.Contains(t, codes, "ir/empty-preserved-key")
	assert.Contains(t, codes, "ir/empty-preserve-reason")
}

// TestVerify_PreservedIsCheckedBelowTheTopLevel confirms the check rides the
// generic walk rather than a hand-listed set of carriers: the same defect must
// be found on a nested Preserved map no registry walk would reach.
func TestVerify_PreservedIsCheckedBelowTheTopLevel(t *testing.T) {
	doc := validDoc()
	doc.Services = []ir.Service{{
		ID: "s/x/S",
		Groups: []ir.OperationGroup{{
			Operations: []ir.Operation{{
				ID:        "o/x/S/op",
				Preserved: ir.Preserved{"openapi:x-internal": {Value: ir.RawValue(`true`)}},
			}},
		}},
	}}
	assert.Contains(t, codesOf(irverify.Verify(doc)), "ir/empty-preserve-reason")
}
