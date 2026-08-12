package irverify_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// TestVerify_AbsentIRVersionIsAViolation asserts a document carrying no schema
// stamp is reported. Absence is the producer's defect — a compiler that never
// set the field — and it is the one an unstamped document hides best, because
// the JSON key is omitempty and so a document without it is byte-identical to
// one that never had it.
func TestVerify_AbsentIRVersionIsAViolation(t *testing.T) {
	doc := validDoc()
	doc.IRVersion = ""

	got := irverify.Verify(doc)
	require.Len(t, got, 1)
	assert.Equal(t, "ir/ir-version-absent", got[0].Code)
	assert.Equal(t, "doc.IRVersion", got[0].Path)
}

// TestVerify_IncompatibleIRVersionIsAViolation asserts a stamp this build does
// not read is reported, whether it names a real other schema generation or is
// not a version at all. The policy is exact equality (ir-design §2.1), so both
// reach the same code: neither can be interpreted, and a consumer has the same
// one move in either case.
func TestVerify_IncompatibleIRVersionIsAViolation(t *testing.T) {
	tests := []struct {
		name    string
		version string
	}{
		{"older generation", "0.1.0"},
		{"newer generation", "99.0.0"},
		{"not a version", "99.99.99-bogus"},
		{"whitespace around the current version", " 0.3.0 "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc := validDoc()
			doc.IRVersion = tc.version

			got := irverify.Verify(doc)
			require.Len(t, got, 1)
			assert.Equal(t, "ir/ir-version-incompatible", got[0].Code)
			assert.Equal(t, "doc.IRVersion", got[0].Path)
			assert.Contains(t, got[0].Message, tc.version)
		})
	}
}

// TestVerify_CurrentIRVersionIsClean holds the rule to firing on the documents
// it is meant to pass. Without it the check could reject every document and
// every other test here would still read as if it worked.
func TestVerify_CurrentIRVersionIsClean(t *testing.T) {
	doc := validDoc()
	require.Equal(t, ir.IRVersion, doc.IRVersion)

	assert.Empty(t, irverify.Verify(doc))
}

// TestVerify_StampedDocumentRoundTripsClean asserts the check leaves invariant 7
// intact: a valid document still survives the JSON round trip byte-for-byte, and
// the decoded document verifies as clean. The stamp is the one field a round trip
// could drop without any other check noticing, since omitempty erases an empty
// one on the way out.
func TestVerify_StampedDocumentRoundTripsClean(t *testing.T) {
	doc := validDoc()

	encoded, err := json.Marshal(doc)
	require.NoError(t, err)

	var decoded ir.Document
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	assert.Equal(t, ir.IRVersion, decoded.IRVersion)
	assert.Empty(t, irverify.Verify(&decoded))

	reencoded, err := json.Marshal(&decoded)
	require.NoError(t, err)
	assert.JSONEq(t, string(encoded), string(reencoded))
}
