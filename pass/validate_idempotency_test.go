package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestValidate_IdempotencyTokenIsPairedWithItsKind pins checkIdempotency over
// every pairing: a token named exactly when the kind is the token kind is clean,
// and either half without the other is an error at the operation's idempotency,
// saying which half is missing.
func TestValidate_IdempotencyTokenIsPairedWithItsKind(t *testing.T) {
	t.Parallel()
	const token = ir.ParamID("param/x/put/key")
	const noToken, unasked = "names no token parameter", `names token parameter "param/x/put/key"`
	for _, tc := range []struct {
		name string
		idem ir.Idempotency
		want string
	}{
		{"the token kind with its token", ir.Idempotency{Kind: ir.IdempotencyToken, TokenParam: token}, ""},
		{"another kind without one", ir.Idempotency{Kind: ir.IdempotencySafe}, ""},
		{"no kind and no token", ir.Idempotency{}, ""},
		{"the token kind without its token", ir.Idempotency{Kind: ir.IdempotencyToken}, noToken},
		{"a token under another kind", ir.Idempotency{Kind: ir.IdempotencyIdempotent, TokenParam: token}, unasked},
		{"a token under no kind", ir.Idempotency{TokenParam: token}, unasked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			op := ir.Operation{ID: "op/x/put", Params: []ir.Parameter{{ID: token, Name: ir.Naming{Source: "key"}}}, Idempotency: tc.idem}
			var got []ir.Diagnostic
			for _, d := range validated(t, docWithOperations(op)) {
				if d.Code == "pass/idempotency-token-mismatch" {
					got = append(got, d)
				}
			}
			if tc.want == "" {
				assert.Empty(t, got)
				return
			}
			require.Len(t, got, 1)
			assert.Equal(t, ir.SeverityError, got[0].Severity)
			assert.Equal(t, "op/x/put/idempotency", got[0].Provenance.Node)
			assert.Contains(t, got[0].Message, tc.want)
		})
	}
}
