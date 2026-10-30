// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

import "testing"

// TestTypeDef_MarkerMethods exercises the unexported sealed-interface marker on
// every concrete TypeDef kind. The markers carry no behavior; calling them keeps
// the sealed-sum contract honest and observable to coverage.
func TestTypeDef_MarkerMethods(t *testing.T) {
	t.Parallel()
	defs := []TypeDef{
		&Primitive{},
		&Scalar{},
		&Model{},
		&Union{},
		&Enum{},
		&List{},
		&MapT{},
		&Tuple{},
		&Literal{},
		&External{},
		&Any{},
	}
	for _, td := range defs {
		t.Run(string(td.Kind()), func(t *testing.T) {
			t.Parallel()
			// typeDef is the unexported sealing marker on the TypeDef interface;
			// invoking it keeps the sealed-sum contract observable. The bodies are
			// empty (zero statements), so this documents rather than covers them.
			td.typeDef()
		})
	}
}
