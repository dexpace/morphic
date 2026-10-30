// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irtest

import "github.com/dexpace/morphic/ir"

// NestedOverloadDoc returns a document whose first group holds an operation
// overloading one declared inside depth levels of nested groups, plus the ID
// they agree on. At a depth past ir.MaxWalkDepth a walk reaches the reference
// and not the declaration; below it, one walk reaches both.
func NestedOverloadDoc(depth int) (*ir.Document, ir.OpID) {
	target := ir.OpID("op/x/Nested")
	nested := ir.OperationGroup{Operations: []ir.Operation{{ID: target}}}
	for range depth {
		nested = ir.OperationGroup{Groups: []ir.OperationGroup{nested}}
	}
	return &ir.Document{Services: []ir.Service{{
		ID: "s/x",
		Groups: []ir.OperationGroup{
			{Operations: []ir.Operation{{ID: "op/x/Overload", OverloadOf: &target}}},
			nested,
		},
	}}}, target
}

// NestedListValue returns a Value that is depth levels of single-element lists
// wrapping a number, the shape a deeply nested array default lowers to.
func NestedListValue(depth int) ir.Value {
	v := ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("0")}
	for range depth {
		v = ir.Value{Kind: ir.ValueList, List: []ir.Value{v}}
	}
	return v
}
