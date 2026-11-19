// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestEnumValid_TiesToConstBlock holds each bare string enum's Valid to the
// values its const block declares, read from the ir declarations rather than a
// list, as TestAuthKind_TiesToConstBlock does for AuthKind: every declared
// value is Valid, and an invented spelling and the empty string are answered as
// the method's doc comment says. The empty string is Valid only for the three
// enums whose comment names an empty default, and an enum whose empty form is
// not a declared value is not Valid empty.
func TestEnumValid_TiesToConstBlock(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		valid      func(string) bool
		emptyValid bool
	}{
		{"AdditionalMode", func(s string) bool { return ir.AdditionalMode(s).Valid() }, true},
		{"HTTPLocation", func(s string) bool { return ir.HTTPLocation(s).Valid() }, false},
		{"IdempotencyKind", func(s string) bool { return ir.IdempotencyKind(s).Valid() }, true},
		{"MessageDirection", func(s string) bool { return ir.MessageDirection(s).Valid() }, false},
		{"PageStrategy", func(s string) bool { return ir.PageStrategy(s).Valid() }, false},
		{"PresenceKind", func(s string) bool { return ir.PresenceKind(s).Valid() }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			declared := declaredConstsOfType(t, tc.name)
			require.NotEmpty(t, declared, "the ir sources must declare %s constants", tc.name)
			for _, c := range declared {
				assert.True(t, tc.valid(c.Value), "declared value %q must be Valid", c.Value)
			}

			assert.Equal(t, tc.emptyValid, tc.valid(""), "the empty string")
			assert.False(t, tc.valid("no-such-value"), "an invented spelling")
		})
	}
}
