package pass_test

import (
	"encoding/json/v2"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// TestValidator_RunHandsBackItsInputWithValidatesFindings pins the adapter: it
// is named validate, returns the document it was given untouched, and reports
// exactly what Validate does. The document carries a dangling ref, so a Run
// that dropped the findings could not pass.
func TestValidator_RunHandsBackItsInputWithValidatesFindings(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["t/m"] = &ir.Model{ID: "t/m", Properties: []ir.Property{
		{ID: "p/m/a", WireName: "a", Type: ir.TypeRef{Target: "t/missing"}},
	}}
	want := pass.Validate(doc)
	require.Contains(t, codes(want), "ir/dangling-type-ref")
	before, err := json.Marshal(doc)
	require.NoError(t, err)

	out, got := pass.Validator{}.Run(doc)

	assert.Equal(t, "validate", pass.Validator{}.Name(), "the name callers type")
	assert.Same(t, doc, out, "an analysis returns its input")
	assert.Empty(t, cmp.Diff(want, got))
	after, err := json.Marshal(doc)
	require.NoError(t, err)
	assert.Empty(t, cmp.Diff(string(before), string(after)), "a pass never mutates its input")
}
