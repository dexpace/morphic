package schema_test

import (
	"context"
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// TestLowerComponentSchemas_EnumBudgetBindsOnlyPastTheLimit drives the budget
// where it is enforced. Both branches lower the same fixture under the same
// walk: what differs is one number, which is what makes the refusal a budget
// rather than a judgement about the enum.
func TestLowerComponentSchemas_EnumBudgetBindsOnlyPastTheLimit(t *testing.T) {
	t.Parallel()
	const spec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    E: {type: string, enum: [a, b, c]}
`
	tests := []struct {
		name    string
		limit   int
		refused bool
	}{
		{"at the budget", 3, false},
		{"one member past the budget", 2, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l, _ := schema.LoweredFor(t, spec)
			l.Ctx.Limits = lowering.Limits{MaxEnumMembers: tc.limit}

			diags := schema.LowerComponentSchemas(t.Context(), l.Ctx, l.Types)

			node := typeByName(l.Document(t), "E")
			require.NotNil(t, node)
			if !tc.refused {
				openapitest.RequireNoErrorDiags(t, diags)
				enum, ok := node.(*ir.Enum)
				require.True(t, ok)
				assert.Len(t, enum.Members, 3)
				return
			}
			_, ok := node.(*ir.Any)
			assert.True(t, ok, "an enum past the budget lowers as the top type")
			require.Len(t, diags, 1)
			assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
			assert.Equal(t, jsontext.Pointer("/components/schemas/E"), diags[0].Provenance.Pointer,
				"the refusal points at the enum, not at the document")
			// The budget bounds what the IR holds, so the members must not come
			// back as bytes. The unhomed-keyword census preserves verbatim
			// whatever the node it built has no field for, and ir.Any has a field
			// for nothing — so without keywordHome counting Any a home for `enum`,
			// refusing the members here put every one of them straight back under
			// Unmodeled, leaving only the per-member amplification bounded.
			assert.Empty(t, node.Common().Unmodeled,
				"a refused enum is not preserved verbatim: that is the cost the budget exists to refuse")
		})
	}
}

// TestLowerComponentSchemas_CanceledContextStopsBeforeTheFirstComponent holds
// the component loop to honouring ctx itself, rather than leaving it to the
// phase boundary above. The boundary would report the same cancellation with no
// check here at all, so the assertion is about what was interned: a document
// declaring components that reaches an already-cancelled walk must leave the
// registry untouched, holding nothing built or promised once it is finished.
func TestLowerComponentSchemas_CanceledContextStopsBeforeTheFirstComponent(t *testing.T) {
	t.Parallel()
	const spec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    A: {type: object, properties: {n: {type: string}}}
    B: {type: object, properties: {n: {type: string}}}
`
	l, _ := schema.LoweredFor(t, spec)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	diags := schema.LowerComponentSchemas(ctx, l.Ctx, l.Types)

	assert.Empty(t, diags)
	reg, finished := l.Types.Finish(t.Context())
	assert.Empty(t, finished)
	assert.Empty(t, reg, "a cancelled walk lowers no component")
}
