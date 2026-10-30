// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package planted

import "github.com/dexpace/morphic/ir"

// shade is an enum of this package, which exhaustive does not hold.
type shade int

const (
	light shade = iota
	dark
)

func plantKinds(k ir.ValueKind, s shade) int {
	switch k { // want "missing cases in switch of type ir.ValueKind: ir.ValueCtor (exhaustive)"
	case ir.ValueNull, ir.ValueBool, ir.ValueString, ir.ValueNumber, ir.ValueBytes, ir.ValueSymbol,
		ir.ValueList, ir.ValueObject, ir.ValueRefKind:
		return 1
	default:
		return 0
	}
}

func plantLocalKinds(s shade) int {
	switch s {
	case light:
		return 1
	default:
		return 0
	}
}
