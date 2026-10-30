// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// TestVerify_IdempotencyTokenIsPairedWithItsKind drives checkIdempotency over
// every pairing: a token named exactly when the kind is the token kind is clean,
// and either half without the other is a violation at the Idempotency itself.
func TestVerify_IdempotencyTokenIsPairedWithItsKind(t *testing.T) {
	t.Parallel()
	const token = ir.ParamID("param/x/S/op/key")
	const at = "doc.Services[0].Groups[0].Operations[0].Idempotency"
	for _, tc := range []struct {
		name string
		idem ir.Idempotency
		want []string
	}{
		{"the token kind with its token", ir.Idempotency{Kind: ir.IdempotencyToken, TokenParam: token}, nil},
		{"another kind without one", ir.Idempotency{Kind: ir.IdempotencySafe}, nil},
		{"no kind and no token", ir.Idempotency{}, nil},
		{"the token kind without its token", ir.Idempotency{Kind: ir.IdempotencyToken}, []string{at}},
		{"a token under another kind", ir.Idempotency{Kind: ir.IdempotencyIdempotent, TokenParam: token}, []string{at}},
		{"a token under no kind", ir.Idempotency{TokenParam: token}, []string{at}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			op := ir.Operation{ID: "op/x/S/op", Params: []ir.Parameter{{ID: token}}, Idempotency: tc.idem}
			var got []string
			for _, v := range irverify.Verify(opDoc(op)) {
				if v.Code == "ir/idempotency-token" {
					got = append(got, v.Path)
				}
			}
			assert.Equal(t, tc.want, got)
		})
	}
}
