// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestThroughAliases_ReadsAtMostTheBound pins the alias scalars a tag's type
// is read through: a chain of maxTagAliasHops reaches what it stands over, a
// longer one or a loop stops on an alias, which bounds no tag.
func TestThroughAliases_ReadsAtMostTheBound(t *testing.T) {
	t.Parallel()
	chain := func(hops int) ir.TypeRegistry {
		reg := ir.TypeRegistry{"type/lit": lit("type/lit", "a")}
		next := ir.TypeID("type/lit")
		for i := range hops {
			id := ir.TypeID(fmt.Sprintf("type/alias%d", i))
			reg[id] = &ir.Scalar{ID: id, Base: &ir.TypeRef{Target: next}}
			next = id
		}
		reg["type/x"] = reg[next]
		return reg
	}
	for name, tc := range map[string]struct {
		reg   ir.TypeRegistry
		bound bool
	}{
		"within the bound": {chain(maxTagAliasHops), true},
		"past the bound":   {chain(maxTagAliasHops + 1), false},
		"a loop": {ir.TypeRegistry{
			"type/x": &ir.Scalar{ID: "type/x", Base: &ir.TypeRef{Target: "type/y"}},
			"type/y": &ir.Scalar{ID: "type/y", Base: &ir.TypeRef{Target: "type/x"}},
		}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.bound, valueOf(newReadings(t), tc.reg, to("type/x")).pins())
		})
	}
}
